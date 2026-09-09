// first_daily_consensus.go gates the first accepted detection of each bird
// species per calendar day behind confirmation from a second model.
//
// A single model crossing its threshold once is the weakest evidence the
// pipeline produces, and it is exactly the evidence that creates a "new species
// today" entry. Requiring two independent models to agree on that first
// detection removes most of those false starts while leaving the rest of the
// day untouched: once a species has one accepted detection, normal single-model
// behaviour resumes.
//
// The rule reuses the aggregation that already exists. A PendingDetection
// collects every model that fired for one species on one source inside the
// existing flush window, so "two models at the same time" needs no new timing or
// buffering — it is a count over item.ModelContributions. Nothing here changes
// inference, persistence, settings or the API.
//
// Every uncertainty fails open (the detection is accepted as it is today):
// unknown taxonomy, unreadable model metadata, a datastore error, or a species
// the active models do not all share.
//
// The checks run cheapest-first: in-memory counts, then the per-day memo, then
// the cached taxonomy and model-support lookups, and only then the datastore.
// The datastore lookup is additionally warmed from warmFirstDailyAcceptance
// under a brief read lock before flushPendingDetections takes its exclusive
// lock, so the common case never runs it there.
package processor

import (
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/labels/nonbird"
	"github.com/tphakala/birdnet-go/internal/logger"
)

const (
	// firstDailyMinModels is how many models must independently confirm the first
	// detection of a species on a given day.
	firstDailyMinModels = 2

	// reasonFirstDailyConsensus is the discard reason surfaced in the flush log
	// and the /system/events/detections aggregation.
	reasonFirstDailyConsensus = "first daily detection confirmed by only one model"

	// firstDailySupportCacheCap bounds the model-support memo. The key space is
	// (active model set x species), so the cap only bites on a pathological
	// config; clearing wholesale beats tracking per-entry ages.
	firstDailySupportCacheCap = 4096

	// firstDailySupportTTL bounds how long a model-support verdict is trusted.
	// The key already changes when the active model SET changes, but a model
	// reload or variant swap can replace a model's label set while keeping the
	// same registry ID, which the key cannot see. Model reloads are a rare,
	// deliberate action, so the TTL only needs to be short enough that such a
	// change is picked up within one operator session, not on every lookup.
	firstDailySupportTTL = 10 * time.Minute
)

// firstDailyConsensus is the small amount of state the rule keeps. The zero
// value is ready to use, so a Processor built directly in a test needs no
// constructor call.
type firstDailyConsensus struct {
	mu sync.Mutex

	// day is the calendar day checked describes; it is dropped on roll-over.
	day string

	// checked records whether a species already has an accepted detection today.
	// Presence means the question has been settled for the day, the value is the
	// answer.
	//
	// The datastore is the source of truth, but both answers are memoized. The
	// positive is required for correctness: persistence is asynchronous, so a
	// detection approved in this flush cycle is only enqueued and its row is
	// usually invisible to the next cycle — without the memo the second detection
	// of a species would be gated again, which is precisely what the rule must not
	// do. The negative is required for cost: a species held back by this rule
	// would otherwise re-query on every flush, forever. A negative can only turn
	// positive when this process accepts a detection, and that path calls
	// markAccepted.
	checked map[string]bool

	// support memoizes whether a species is shared by every relevant active bird
	// model, each entry bounded by firstDailySupportTTL. The key carries the
	// active model set, which is what usually invalidates the verdict, so this
	// has no calendar semantics and survives the day roll — re-deriving it every
	// midnight would reload every label set during the dawn chorus.
	support map[string]supportEntry
}

type supportEntry struct {
	shared    bool
	expiresAt time.Time
}

// markAccepted records that a species has an accepted detection on day.
func (c *firstDailyConsensus) markAccepted(day, species string) {
	c.markChecked(day, species, true)
}

func (c *firstDailyConsensus) markChecked(day, species string, accepted bool) {
	if day == "" || species == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollLocked(day)
	if c.checked == nil {
		c.checked = make(map[string]bool)
	}
	c.checked[species] = accepted
}

// acceptedToday reports the memoized answer; settled is false when the species
// has not been resolved for this day yet.
func (c *firstDailyConsensus) acceptedToday(day, species string) (accepted, settled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollLocked(day)
	accepted, settled = c.checked[species]
	return accepted, settled
}

func (c *firstDailyConsensus) lookupSupport(key string) (shared, cached bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.support[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return false, false
	}
	return entry.shared, true
}

func (c *firstDailyConsensus) storeSupport(key string, shared bool) {
	c.storeSupportUntil(key, shared, time.Now().Add(firstDailySupportTTL))
}

// storeSupportUntil is storeSupport with an explicit expiry, so tests can seed an
// already-stale entry without waiting out firstDailySupportTTL.
func (c *firstDailyConsensus) storeSupportUntil(key string, shared bool, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.support == nil || len(c.support) >= firstDailySupportCacheCap {
		c.support = make(map[string]supportEntry)
	}
	c.support[key] = supportEntry{shared: shared, expiresAt: expiresAt}
}

// rollLocked drops yesterday's per-day state. Caller holds c.mu.
func (c *firstDailyConsensus) rollLocked(day string) {
	if c.day == day {
		return
	}
	c.day = day
	c.checked = nil
}

// firstDailyCandidate is item narrowed to what the gate and its prewarm pass
// both need, so a candidate collected under a read lock outlives it safely.
type firstDailyCandidate struct {
	day            string
	scientificName string
}

// firstDailyVerdict is what firstDailyGateApplies decided using only in-memory
// state: whether the rule has nothing to say, already knows the answer from the
// per-day memo, or must still ask the datastore.
type firstDailyVerdict int

const (
	firstDailyNormal      firstDailyVerdict = iota // rule does not apply: accept as today
	firstDailyKnownAbsent                          // memo already says "not accepted yet": discard
	firstDailyNeedsCheck                           // memo has no answer yet: the datastore must be asked
)

// firstDailyGateApplies runs every check that does not touch the datastore and
// classifies item accordingly.
//
// This is the single predicate both shouldDiscardFirstDailyDetection and
// warmFirstDailyAcceptance use, so the two paths cannot silently diverge on
// which detections the datastore lookup applies to.
func (p *Processor) firstDailyGateApplies(item *PendingDetection, settings *conf.Settings) (firstDailyVerdict, firstDailyCandidate) {
	if item == nil || settings == nil {
		return firstDailyNormal, firstDailyCandidate{}
	}

	result := &item.Detection.Result
	scientificName := result.Species.ScientificName
	if scientificName == "" || result.Timestamp.IsZero() {
		return firstDailyNormal, firstDailyCandidate{}
	}

	// Bats and the non-species sound classes keep today's behaviour untouched.
	if !classifier.IsBirdCapableModel(item.BestModelID) || nonbird.IsNonSpeciesLabel(result.RawLabel) {
		return firstDailyNormal, firstDailyCandidate{}
	}

	// Already agreed on by enough models: nothing for this rule to decide. Checked
	// first because it is a pure in-memory count and it is the common outcome for
	// a genuine new species.
	if p.countConfirmingModels(settings, item) >= firstDailyMinModels {
		return firstDailyNormal, firstDailyCandidate{}
	}

	// A dynamic threshold that actually lowered the bar is the operator asking for
	// a more permissive gate on this species; do not override that with a stricter
	// one.
	if p.dynamicThresholdLowered(settings, item) {
		return firstDailyNormal, firstDailyCandidate{}
	}

	// Only the first accepted detection of the day is gated. The memo answers for
	// every species already settled today — the steady state for the rest of the
	// day — so it comes before the taxonomy and model-support lookups, and before
	// the datastore is asked again.
	day := result.Date()
	candidate := firstDailyCandidate{day: day, scientificName: scientificName}
	if accepted, settled := p.firstDaily.acceptedToday(day, scientificName); settled {
		if accepted {
			return firstDailyNormal, firstDailyCandidate{}
		}
		return firstDailyKnownAbsent, candidate
	}

	// Birds only. An unknown taxon is not evidence of a non-bird, so it fails open.
	if isBird, known := classifier.IsBirdSpecies(scientificName); !known || !isBird {
		return firstDailyNormal, firstDailyCandidate{}
	}

	// Only meaningful when every relevant active bird model could have confirmed
	// it. A species one model cannot predict could never reach two confirmations.
	if !p.speciesSharedByActiveBirdModels(item.Source, scientificName) {
		return firstDailyNormal, firstDailyCandidate{}
	}

	return firstDailyNeedsCheck, candidate
}

// shouldDiscardFirstDailyDetection reports whether item is the first detection of
// a bird species today with only one model behind it, and so should be held back
// until a second model agrees.
//
// Discarding here is deliberately terminal for this pending entry only: the
// species is not marked accepted, so the next window in which two models do agree
// is accepted normally, and single-model behaviour resumes from then on.
func (p *Processor) shouldDiscardFirstDailyDetection(item *PendingDetection, settings *conf.Settings) (discard bool, reason string) {
	verdict, candidate := p.firstDailyGateApplies(item, settings)

	switch verdict {
	case firstDailyNormal:
		return false, ""
	case firstDailyKnownAbsent:
		// Already resolved by the memo (typically by warmFirstDailyAcceptance,
		// below): no datastore round trip needed to discard.
	case firstDailyNeedsCheck:
		// warmFirstDailyAcceptance resolves this from flushPendingDetections
		// before its exclusive lock is taken, so the datastore is reached here
		// only for a candidate that appeared after that pass ran this tick — the
		// query stays off the ingestion-blocking path in the common case.
		accepted, ok := p.speciesAcceptedInDatastore(candidate.day, candidate.scientificName)
		if !ok || accepted {
			return false, ""
		}
	}

	GetLogger().Debug("first daily detection lacks a second model",
		logger.String("species", item.Detection.Result.Species.CommonName),
		logger.String("scientific_name", candidate.scientificName),
		logger.String("source", p.getDisplayNameForSource(item.Source)),
		logger.String("best_model_id", item.BestModelID),
		logger.Int("model_count", len(item.ModelContributions)),
		logger.String("day", candidate.day),
		logger.String("operation", "first_daily_consensus"))

	return true, reasonFirstDailyConsensus
}

// warmFirstDailyAcceptance resolves "already accepted today" for every pending
// detection the gate would otherwise need the datastore for, via queries run
// after releasing a brief read lock on p.pendingDetections — so
// flushPendingDetections's exclusive lock, held for the rest of the flush cycle,
// never blocks on database I/O for this rule. It answers the same question
// shouldDiscardFirstDailyDetection does at flush time, into the same memo, so a
// slow query here only delays that memo warming, never a discard decision.
//
// A detection that appears after this pass runs and is due before the next one
// still gets a correct answer, just resolved (rarely) under the exclusive lock
// instead — this pass is a fast path, not a correctness requirement.
func (p *Processor) warmFirstDailyAcceptance(settings *conf.Settings) {
	if p.Ds == nil {
		return
	}

	p.pendingMutex.RLock()
	candidates := make([]firstDailyCandidate, 0, len(p.pendingDetections))
	for mapKey := range p.pendingDetections {
		item := p.pendingDetections[mapKey]
		if verdict, candidate := p.firstDailyGateApplies(&item, settings); verdict == firstDailyNeedsCheck {
			candidates = append(candidates, candidate)
		}
	}
	p.pendingMutex.RUnlock()

	for _, candidate := range candidates {
		p.speciesAcceptedInDatastore(candidate.day, candidate.scientificName)
	}
}

// noteAcceptedDetection records an approved detection so later detections of the
// same species today bypass the rule without waiting for asynchronous
// persistence to land.
func (p *Processor) noteAcceptedDetection(item *PendingDetection) {
	if item == nil {
		return
	}
	result := &item.Detection.Result
	if result.Timestamp.IsZero() {
		return
	}
	p.firstDaily.markAccepted(result.Date(), result.Species.ScientificName)
}

// countConfirmingModels counts the bird-capable models whose best score for this
// species reached that model's normal (non-dynamic) threshold. The dynamically
// adjusted threshold is deliberately not used: a model that only cleared a
// lowered bar is not independent confirmation.
func (p *Processor) countConfirmingModels(settings *conf.Settings, item *PendingDetection) int {
	species := item.Detection.Result.Species
	count := 0
	for modelID, contrib := range item.ModelContributions {
		if !classifier.IsBirdCapableModel(modelID) {
			continue
		}
		normal := p.getBaseConfidenceThreshold(settings, species.CommonName, species.ScientificName, modelID)
		if float32(contrib.MaxConfidence) >= normal {
			count++
		}
	}
	return count
}

// dynamicThresholdLowered reports whether dynamic thresholding is currently
// holding this species below its normal threshold. It reads the same state
// getAdjustedConfidenceThreshold applies, but without that function's side
// effects (expiry reset and event recording), which must not be triggered from a
// filtering decision.
func (p *Processor) dynamicThresholdLowered(settings *conf.Settings, item *PendingDetection) bool {
	if !settings.Realtime.DynamicThreshold.Enabled {
		return false
	}
	species := item.Detection.Result.Species
	// A custom per-species threshold opts out of dynamic adjustment entirely,
	// matching shouldFilterDetection.
	if config, exists := lookupSpeciesConfig(settings.Realtime.Species.Config, species.CommonName, species.ScientificName); exists && config.Threshold > 0 {
		return false
	}

	key := dynamicThresholdKey(species.CommonName, species.ScientificName)

	var level int
	var timer time.Time
	p.thresholdsMutex.RLock()
	if dt := p.DynamicThresholds[key]; dt != nil {
		level, timer = dt.Level, dt.Timer
	}
	p.thresholdsMutex.RUnlock()

	if level <= 0 || !time.Now().Before(timer) {
		return false
	}

	base := float64(p.getBaseConfidenceThreshold(settings, species.CommonName, species.ScientificName, item.BestModelID))
	return effectiveDynamicThreshold(base, level, settings.Realtime.DynamicThreshold.Min) < base
}

// dynamicThresholdKey derives the DynamicThresholds map key for a species. The
// map is written under this key by parseAndValidateSpecies via speciesLowercase,
// so both must agree or lookups here silently miss.
func dynamicThresholdKey(commonName, scientificName string) string {
	key := strings.ToLower(commonName)
	if key == "" {
		key = strings.ToLower(scientificName)
	}
	return key
}

// speciesSharedByActiveBirdModels reports whether enough relevant bird models are
// active and all of them can predict the species.
func (p *Processor) speciesSharedByActiveBirdModels(sourceID, scientificName string) bool {
	modelIDs := p.sourceModelIDs(sourceID)
	cacheKey := firstDailySupportKey(modelIDs, scientificName)

	if shared, cached := p.firstDaily.lookupSupport(cacheKey); cached {
		return shared
	}

	shared, ok := p.Bn.SpeciesSharedByBirdModels(scientificName, modelIDs, firstDailyMinModels)
	if !ok {
		// Transient: a model mid-reload. Do not memoize it, or one bad moment would
		// disable the rule for this species until the TTL expires.
		return false
	}

	p.firstDaily.storeSupport(cacheKey, shared)
	return shared
}

// sourceModelIDs returns the models actually analysing sourceID, sorted, which is
// the set that could realistically confirm a detection on it. A model loaded but
// not assigned to this source never sees its audio.
//
// Returns nil when the topology is unknown, which SpeciesSharedByBirdModels reads
// as "consider every loaded model".
func (p *Processor) sourceModelIDs(sourceID string) []string {
	if p.BufferMgr == nil || sourceID == "" {
		return nil
	}
	buffers := p.BufferMgr.AnalysisBuffers(sourceID)
	if len(buffers) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(buffers))
}

// firstDailySupportKey builds the memo key. The model set is part of the key so
// loading, unloading or reassigning a model re-derives the verdict instead of
// serving a stale one; the IDs arrive sorted so the key is stable. A model reload
// that keeps its registry ID but swaps its label set is not visible in this key
// and is instead bounded by firstDailySupportTTL.
func firstDailySupportKey(modelIDs []string, scientificName string) string {
	return strings.Join(modelIDs, ",") + "|" + scientificName
}

// speciesAcceptedInDatastore asks the datastore whether the species already has a
// detection today, memoizing both answers so the question costs at most one query
// per species per day. warmFirstDailyAcceptance is the intended caller in the
// common case; shouldDiscardFirstDailyDetection falls back to calling it directly
// only for a candidate the warm pass missed this tick.
//
// ok is false when the question cannot be answered, which the caller treats as
// "accept the detection as it is today".
func (p *Processor) speciesAcceptedInDatastore(day, scientificName string) (accepted, ok bool) {
	if p.Ds == nil {
		return false, false
	}
	if accepted, settled := p.firstDaily.acceptedToday(day, scientificName); settled {
		return accepted, true
	}

	count, err := p.Ds.CountSpeciesDetections(scientificName, day, "", 0)
	if err != nil {
		GetLogger().Debug("first daily consensus: species lookup failed, accepting detection",
			logger.String("scientific_name", scientificName),
			logger.String("day", day),
			logger.Error(err),
			logger.String("operation", "first_daily_consensus"))
		return false, false
	}

	accepted = count > 0
	p.firstDaily.markChecked(day, scientificName, accepted)
	return accepted, true
}
