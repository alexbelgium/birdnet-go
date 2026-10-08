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
// Every exception and uncertainty fails open (the detection is accepted as it
// is today): a whitelisted species, unknown taxonomy, unreadable model metadata,
// a datastore error, or a species the active models do not all share.
//
// The checks run cheapest-first because the gate is evaluated while
// p.pendingMutex is held: whitelist, in-memory counts, then the per-day memo,
// then the cached taxonomy and model-support lookups. The datastore is not
// consulted there at all — warmFirstDailyAcceptance resolves it beforehand,
// off the lock.
package processor

import (
	"fmt"
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

	// firstDailySupportCacheCap bounds the memo's memory. Expired entries are
	// never swept, so this is what stops the map growing without limit; the key
	// space is (active model set x species), so it only bites on a pathological
	// config, where clearing wholesale is cheaper than evicting individually.
	firstDailySupportCacheCap = 4096

	// firstDailyRetainedDays is how many calendar days of answers are kept: the
	// current one and the previous one, which is the most that can be pending
	// simultaneously across a midnight boundary.
	firstDailyRetainedDays = 2

	// firstDailySupportTTL bounds how long a model-support verdict is trusted.
	// The key already changes when the active model SET changes, but a model
	// reload or variant swap can replace a model's label set while keeping the
	// same registry ID, which the key cannot see. Model reloads are a rare,
	// deliberate action, so the TTL only needs to be short enough that such a
	// change is picked up within one operator session, not on every lookup.
	firstDailySupportTTL = 10 * time.Minute

	// firstDailyMaxConcurrentLookups bounds datastore work started by one or
	// more warm passes. Completed results remain in-flight until the next pass
	// applies them, so this also bounds the result channel.
	firstDailyMaxConcurrentLookups = 4
)

// firstDailyConsensus is the small amount of state the rule keeps. The zero
// value is ready to use, so a Processor built directly in a test needs no
// constructor call.
type firstDailyConsensus struct {
	mu sync.Mutex

	// checked maps a calendar day to that day's per-species answers. Presence of
	// a species means the question has been settled for that day; the value is
	// the answer.
	//
	// The datastore is the source of truth, but both answers are memoized. The
	// positive is required for correctness: persistence is asynchronous, so a
	// detection approved in this flush cycle is only enqueued and its row is
	// usually invisible to the next cycle — without the memo the second detection
	// of a species would be gated again, which is precisely what the rule must not
	// do. The negative is required for cost: a species held back by this rule
	// would otherwise re-query on every flush, forever.
	//
	// It is keyed by day rather than reset on a day change because detections
	// either side of midnight are pending at the same time, and the flush loop
	// walks them in map order. A single day's worth of state would be evicted by
	// whichever late previous-day entry happened to be visited next, taking the
	// fresh day's acceptances with it.
	checked map[string]map[string]bool

	// support memoizes whether a species is shared by every relevant active bird
	// model, each entry bounded by firstDailySupportTTL. The key carries the
	// active model set, which is what usually invalidates the verdict, so this
	// has no calendar semantics and survives the day roll — re-deriving it every
	// midnight would reload every label set during the dawn chorus.
	support map[string]supportEntry

	// approvals made during one flush cycle are committed at the next warm pass,
	// so every entry in the cycle observes the same accepted set.
	approved map[firstDailyCandidate]struct{}

	// lookups carries datastore answers back to the flusher. inFlight prevents
	// duplicate queries for the same day and species.
	lookups  chan firstDailyLookupResult
	inFlight map[firstDailyCandidate]struct{}

	// generation advances while the rule is disabled. Results from an earlier
	// generation are ignored after re-enabling.
	generation uint64
}

type supportEntry struct {
	shared    bool
	expiresAt time.Time
}

// markAccepted records that a species has an accepted detection on day.
func (c *firstDailyConsensus) markAccepted(day, species string) {
	if day == "" || species == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dayLocked(day)[species] = true
}

// markAbsent records that the datastore reported no detection of species on day.
//
// It never overwrites an acceptance already recorded for that day. Approval is
// asynchronous, so a query issued moments after one can legitimately still see
// zero rows; caching that as "absent" would gate the species' next detection —
// the outcome the positive memo exists to prevent.
func (c *firstDailyConsensus) markAbsent(day, species string) {
	if day == "" || species == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	answers := c.dayLocked(day)
	if _, settled := answers[species]; settled {
		return
	}
	answers[species] = false
}

// acceptedToday reports the memoized answer; settled is false when the species
// has not been resolved for that day yet. It is a pure read.
func (c *firstDailyConsensus) acceptedToday(day, species string) (accepted, settled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	accepted, settled = c.checked[day][species]
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
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.support == nil || len(c.support) >= firstDailySupportCacheCap {
		c.support = make(map[string]supportEntry)
	}
	c.support[key] = supportEntry{shared: shared, expiresAt: time.Now().Add(firstDailySupportTTL)}
}

// dayLocked returns day's answer map, creating it and evicting any day older
// than the two most recent. Two are retained because that is the most that can
// be pending at once — a detection heard before midnight and one heard after.
// Caller holds c.mu.
func (c *firstDailyConsensus) dayLocked(day string) map[string]bool {
	if c.checked == nil {
		c.checked = make(map[string]map[string]bool, firstDailyRetainedDays)
	}
	if c.checked[day] == nil {
		c.checked[day] = make(map[string]bool)
	}
	for len(c.checked) > firstDailyRetainedDays {
		oldest := ""
		for known := range c.checked {
			// Days are YYYY-MM-DD, so lexicographic order is chronological.
			if oldest == "" || known < oldest {
				oldest = known
			}
		}
		delete(c.checked, oldest)
	}
	return c.checked[day]
}

// firstDailyCandidate is item narrowed to the two strings the datastore lookup
// needs. warmFirstDailyAcceptance collects a slice of these under the read lock
// and queries after releasing it, so it must not reference the PendingDetection
// (whose ModelContributions map is shared state).
type firstDailyCandidate struct {
	day            string
	scientificName string
}

type firstDailyLookupResult struct {
	firstDailyCandidate
	count      int64
	err        error
	generation uint64
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

	// Opt-in. Read from the per-cycle settings snapshot, so toggling it takes
	// effect on the next flush without a restart.
	if !settings.Realtime.FirstDailyConsensus.Enabled {
		return firstDailyNormal, firstDailyCandidate{}
	}

	result := &item.Detection.Result
	scientificName := result.Species.ScientificName
	if scientificName == "" || result.Timestamp.IsZero() {
		return firstDailyNormal, firstDailyCandidate{}
	}

	// Whitelisted species always retain normal single-model behaviour. Keep this
	// before taxonomy, model-support and datastore work so an exemption is free
	// apart from the same canonical name matching used by the species exclude list.
	if isSpeciesExcluded(result.Species.CommonName, scientificName, settings.Realtime.FirstDailyConsensus.Whitelist) {
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

	// Once a species has an accepted detection today the rule is done with it for
	// the day, whatever else is true of it. That is the steady state for most of
	// the day, so this half of the memo is read before the taxonomy and
	// model-support lookups below.
	day := result.Date()
	candidate := firstDailyCandidate{day: day, scientificName: scientificName}
	accepted, settled := p.firstDaily.acceptedToday(day, scientificName)
	if settled && accepted {
		return firstDailyNormal, firstDailyCandidate{}
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

	// The "not accepted yet" half of the memo is only actionable once the checks
	// above have confirmed the species is still eligible. Reading it earlier would
	// let a stale answer outlive the eligibility that produced it: a model
	// reconfigured mid-day can stop sharing a species, and the rule must exempt it
	// from that point rather than keep discarding it on the strength of a memo
	// written while it was still shared.
	if settled {
		return firstDailyKnownAbsent, candidate
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

	// Only a memo that already says "not accepted yet" discards. This function
	// never touches the datastore: warmFirstDailyAcceptance has already resolved
	// every detection due this cycle, off the lock. An unresolved verdict here
	// therefore means the query failed, and re-running it under the exclusive
	// lock is exactly what the warm pass exists to avoid — so it fails open,
	// like every other uncertainty in this rule.
	if verdict != firstDailyKnownAbsent {
		return false, ""
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

// warmFirstDailyAcceptance applies completed datastore answers and approvals
// from the previous flush cycle, then starts bounded background lookups for
// unsettled pending species. Looking at entries before their deadline normally
// gives the query the detection window in which to finish. Anything still
// unresolved when due fails open in shouldDiscardFirstDailyDetection.
//
// It is the datastore I/O specifically that this keeps off the lock. The
// model-support lookup the gate also performs still runs under the read lock
// here, and can walk every active model's label set when its memo misses; that
// is bounded by firstDailySupportTTL and is the same work the flush path would
// otherwise do under the exclusive lock.
func (p *Processor) warmFirstDailyAcceptance(_ time.Time, settings *conf.Settings) {
	// The feature boundary sits here, not in the per-item predicate: shipped off
	// by default, this must cost one branch per tick, not a lock plus a sweep.
	if !settings.Realtime.FirstDailyConsensus.Enabled {
		p.firstDaily.disable()
		return
	}
	if p.Ds == nil {
		return
	}
	p.firstDaily.applyCompletedLookups()
	p.firstDaily.commitApprovals()

	var candidates []firstDailyCandidate
	p.pendingMutex.RLock()
	for mapKey := range p.pendingDetections {
		item := p.pendingDetections[mapKey]
		if verdict, candidate := p.firstDailyGateApplies(&item, settings); verdict == firstDailyNeedsCheck {
			candidates = append(candidates, candidate)
		}
	}
	p.pendingMutex.RUnlock()

	for _, candidate := range candidates {
		p.startFirstDailyLookup(candidate)
	}
}

// noteAcceptedDetection records an approval for the next warm pass. Deferring the
// commit makes every pending entry in this flush cycle observe the same memo.
func (p *Processor) noteAcceptedDetection(item *PendingDetection) {
	if item == nil {
		return
	}
	result := &item.Detection.Result
	if result.Timestamp.IsZero() {
		return
	}
	p.firstDaily.queueApproval(firstDailyCandidate{day: result.Date(), scientificName: result.Species.ScientificName})
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
		if normal <= 0 {
			normal = modelGlobalConfidenceThreshold(settings, modelID)
		}
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

func (c *firstDailyConsensus) disable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-enabling must query again because detections accepted while the rule was
	// off were never queued as approvals here.
	c.checked = nil
	c.approved = nil
	c.generation++
}

func (c *firstDailyConsensus) queueApproval(candidate firstDailyCandidate) {
	if candidate.day == "" || candidate.scientificName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.approved == nil {
		c.approved = make(map[firstDailyCandidate]struct{})
	}
	c.approved[candidate] = struct{}{}
}

func (c *firstDailyConsensus) commitApprovals() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for candidate := range c.approved {
		c.dayLocked(candidate.day)[candidate.scientificName] = true
	}
	c.approved = nil
}

func (p *Processor) startFirstDailyLookup(candidate firstDailyCandidate) {
	c := &p.firstDaily
	c.mu.Lock()
	if c.lookups == nil {
		c.lookups = make(chan firstDailyLookupResult, firstDailyMaxConcurrentLookups)
	}
	if c.inFlight == nil {
		c.inFlight = make(map[firstDailyCandidate]struct{})
	}
	if _, settled := c.checked[candidate.day][candidate.scientificName]; settled || len(c.inFlight) >= firstDailyMaxConcurrentLookups {
		c.mu.Unlock()
		return
	}
	if _, running := c.inFlight[candidate]; running {
		c.mu.Unlock()
		return
	}
	c.inFlight[candidate] = struct{}{}
	generation, results, ds := c.generation, c.lookups, p.Ds
	c.mu.Unlock()

	go func() {
		result := firstDailyLookupResult{firstDailyCandidate: candidate, generation: generation}
		defer func() {
			if recovered := recover(); recovered != nil {
				result.err = fmt.Errorf("first daily consensus lookup panicked: %v", recovered)
			}
			results <- result
		}()
		result.count, result.err = ds.CountSpeciesDetections(candidate.scientificName, candidate.day, "", 0)
	}()
}

func (c *firstDailyConsensus) applyCompletedLookups() {
	for {
		select {
		case result := <-c.lookups:
			c.mu.Lock()
			delete(c.inFlight, result.firstDailyCandidate)
			current := result.generation == c.generation
			c.mu.Unlock()
			if !current {
				continue
			}
			if result.err != nil {
				GetLogger().Debug("first daily consensus: species lookup failed, accepting detection",
					logger.String("scientific_name", result.scientificName), logger.String("day", result.day),
					logger.Error(result.err), logger.String("operation", "first_daily_consensus"))
				continue
			}
			if result.count > 0 {
				c.markAccepted(result.day, result.scientificName)
			} else {
				c.markAbsent(result.day, result.scientificName)
			}
		default:
			return
		}
	}
}
