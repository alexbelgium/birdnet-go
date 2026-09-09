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

	// firstDailySupportCacheCap bounds the model-support memo. The natural key
	// space is (active model set x species seen today), so the cap only matters
	// on a pathological config; clearing wholesale is cheaper than tracking ages.
	firstDailySupportCacheCap = 4096

	// firstDailyDayFormat matches the date column the datastore stores and
	// queries detections by.
	firstDailyDayFormat = "2006-01-02"
)

// firstDailyConsensus is the small amount of state the rule keeps. The zero
// value is ready to use, so a Processor built directly in a test needs no
// constructor call.
type firstDailyConsensus struct {
	mu sync.Mutex

	// day is the calendar day accepted and support describe. Both are dropped
	// when it rolls over.
	day string

	// accepted memoizes species already confirmed for the day. The datastore is
	// the source of truth, but persistence is asynchronous: a detection approved
	// in this flush cycle is only enqueued, so the row is usually not visible to
	// the very next cycle. Without this memo the second detection of a species
	// would be gated again, which is precisely what the rule must not do.
	accepted map[string]struct{}

	// support memoizes whether a species is shared by every relevant active bird
	// model, keyed by the active model set so a topology change re-derives it.
	support map[string]bool
}

// markAccepted records that a species has an accepted detection on day.
func (c *firstDailyConsensus) markAccepted(day, species string) {
	if day == "" || species == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollLocked(day)
	if c.accepted == nil {
		c.accepted = make(map[string]struct{})
	}
	c.accepted[species] = struct{}{}
}

// isAccepted reports whether markAccepted has seen this species today.
func (c *firstDailyConsensus) isAccepted(day, species string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollLocked(day)
	_, ok := c.accepted[species]
	return ok
}

// lookupSupport returns a memoized model-support verdict.
func (c *firstDailyConsensus) lookupSupport(day, key string) (shared, cached bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollLocked(day)
	shared, cached = c.support[key]
	return shared, cached
}

// storeSupport memoizes a model-support verdict.
func (c *firstDailyConsensus) storeSupport(day, key string, shared bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rollLocked(day)
	if c.support == nil {
		c.support = make(map[string]bool)
	}
	if len(c.support) >= firstDailySupportCacheCap {
		c.support = make(map[string]bool)
	}
	c.support[key] = shared
}

// rollLocked drops yesterday's state. Caller holds c.mu.
func (c *firstDailyConsensus) rollLocked(day string) {
	if c.day == day {
		return
	}
	c.day = day
	c.accepted = nil
	c.support = nil
}

// firstDailyDay returns the calendar day a pending detection belongs to, in the
// same local-date form the datastore stores. A detection that starts before
// midnight and flushes after it is classified by when it was heard, not by when
// the flusher happened to reach it.
func firstDailyDay(item *PendingDetection) string {
	when := item.FirstDetected
	if when.IsZero() {
		when = item.CreatedAt
	}
	if when.IsZero() {
		return ""
	}
	return when.Format(firstDailyDayFormat)
}

// shouldDiscardFirstDailyDetection reports whether item is the first detection of
// a bird species today with only one model behind it, and so should be held back
// until a second model agrees.
//
// Discarding here is deliberately terminal for this pending entry only: the
// species is not marked accepted, so the next window in which two models do agree
// is accepted normally, and single-model behaviour resumes from then on.
func (p *Processor) shouldDiscardFirstDailyDetection(item *PendingDetection, settings *conf.Settings) (discard bool, reason string) {
	if item == nil || settings == nil {
		return false, ""
	}

	result := &item.Detection.Result
	scientificName := result.Species.ScientificName
	commonName := result.Species.CommonName
	if scientificName == "" {
		return false, ""
	}

	// Bats and the non-species sound classes keep today's behaviour untouched.
	if item.BestModelID == classifier.RegistryIDBat || nonbird.IsNonSpeciesLabel(result.RawLabel) {
		return false, ""
	}

	// Already agreed on by enough models: nothing for this rule to decide. Checked
	// first because it is a pure in-memory count and it is the common outcome for
	// a genuine new species.
	if p.countConfirmingModels(settings, item, commonName, scientificName) >= firstDailyMinModels {
		return false, ""
	}

	// A dynamic threshold that actually lowered the bar is the operator asking for
	// a more permissive gate on this species; do not override that with a stricter
	// one.
	if p.dynamicThresholdLowered(settings, item.BestModelID, commonName, scientificName) {
		return false, ""
	}

	// Birds only. An unknown taxon is not evidence of a non-bird, so it fails open.
	if isBird, known := classifier.IsBirdSpecies(scientificName); !known || !isBird {
		return false, ""
	}

	day := firstDailyDay(item)
	if day == "" {
		return false, ""
	}

	// Only meaningful when every relevant active bird model could have confirmed
	// it. A species one model cannot predict could never reach two confirmations.
	if !p.speciesSharedByActiveBirdModels(day, item.Source, scientificName) {
		return false, ""
	}

	// Only the first accepted detection of the day is gated.
	if p.speciesAcceptedToday(day, scientificName) {
		return false, ""
	}

	GetLogger().Debug("first daily detection lacks a second model",
		logger.String("species", commonName),
		logger.String("scientific_name", scientificName),
		logger.String("source", p.getDisplayNameForSource(item.Source)),
		logger.String("best_model_id", item.BestModelID),
		logger.Int("model_count", len(item.ModelContributions)),
		logger.String("day", day),
		logger.String("operation", "first_daily_consensus"))

	return true, reasonFirstDailyConsensus
}

// noteAcceptedDetection records an approved detection so later detections of the
// same species today bypass the rule without waiting for asynchronous
// persistence to land.
func (p *Processor) noteAcceptedDetection(item *PendingDetection) {
	if item == nil {
		return
	}
	p.firstDaily.markAccepted(firstDailyDay(item), item.Detection.Result.Species.ScientificName)
}

// countConfirmingModels counts the bird-capable models whose best score for this
// species reached that model's normal (non-dynamic) threshold. The dynamically
// adjusted threshold is deliberately not used: a model that only cleared a
// lowered bar is not independent confirmation.
func (p *Processor) countConfirmingModels(settings *conf.Settings, item *PendingDetection, commonName, scientificName string) int {
	count := 0
	for modelID, contrib := range item.ModelContributions {
		if modelID == classifier.RegistryIDBat {
			continue
		}
		normal := p.getBaseConfidenceThreshold(settings, commonName, scientificName, modelID)
		if float32(contrib.MaxConfidence) >= normal {
			count++
		}
	}
	return count
}

// dynamicThresholdLowered reports whether dynamic thresholding is currently
// holding this species below its normal threshold. It reads the same state
// getAdjustedConfidenceThreshold applies, but without that function's
// side effects (expiry reset and event recording), which must not be triggered
// from a filtering decision.
func (p *Processor) dynamicThresholdLowered(settings *conf.Settings, modelID, commonName, scientificName string) bool {
	if !settings.Realtime.DynamicThreshold.Enabled {
		return false
	}
	// A custom per-species threshold opts out of dynamic adjustment entirely,
	// matching shouldFilterDetection.
	if config, exists := lookupSpeciesConfig(settings.Realtime.Species.Config, commonName, scientificName); exists && config.Threshold > 0 {
		return false
	}

	key := strings.ToLower(commonName)
	if key == "" {
		key = strings.ToLower(scientificName)
	}

	p.thresholdsMutex.RLock()
	dt, exists := p.DynamicThresholds[key]
	var level int
	var timer time.Time
	if exists && dt != nil {
		level, timer = dt.Level, dt.Timer
	}
	p.thresholdsMutex.RUnlock()

	if !exists || level <= 0 || !time.Now().Before(timer) {
		return false
	}

	base := float64(p.getBaseConfidenceThreshold(settings, commonName, scientificName, modelID))
	return effectiveDynamicThreshold(base, level, settings.Realtime.DynamicThreshold.Min) < base
}

// speciesSharedByActiveBirdModels reports whether at least firstDailyMinModels
// relevant bird models are active and every one of them can predict the species.
func (p *Processor) speciesSharedByActiveBirdModels(day, sourceID, scientificName string) bool {
	modelIDs := p.sourceModelIDs(sourceID)
	cacheKey := firstDailySupportKey(modelIDs, scientificName)

	if shared, cached := p.firstDaily.lookupSupport(day, cacheKey); cached {
		return shared
	}

	relevant, supporting, ok := p.Bn.BirdModelSpeciesSupport(scientificName, modelIDs)
	shared := ok && relevant >= firstDailyMinModels && supporting == relevant

	p.firstDaily.storeSupport(day, cacheKey, shared)
	return shared
}

// sourceModelIDs returns the models actually analysing sourceID, which is the set
// that could realistically confirm a detection on it. A model loaded but not
// assigned to this source never sees its audio.
//
// Returns nil when the topology is unknown, which BirdModelSpeciesSupport reads
// as "consider every loaded model".
func (p *Processor) sourceModelIDs(sourceID string) map[string]struct{} {
	if p.BufferMgr == nil || sourceID == "" {
		return nil
	}
	buffers := p.BufferMgr.AnalysisBuffers(sourceID)
	if len(buffers) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(buffers))
	for modelID := range buffers {
		ids[modelID] = struct{}{}
	}
	return ids
}

// firstDailySupportKey builds the memo key. The model set is part of the key so
// loading, unloading or reassigning a model re-derives the verdict instead of
// serving a stale one.
func firstDailySupportKey(modelIDs map[string]struct{}, scientificName string) string {
	if len(modelIDs) == 0 {
		return "*|" + scientificName
	}
	ids := slices.Collect(maps.Keys(modelIDs))
	slices.Sort(ids)
	return strings.Join(ids, ",") + "|" + scientificName
}

// speciesAcceptedToday reports whether the species already has an accepted
// detection on day. The datastore is authoritative; the in-memory memo only
// covers detections approved in this process that have not been persisted yet,
// and spares the database a query per detection for the rest of the day.
//
// Any inability to answer returns true, so the rule is skipped and the detection
// is accepted exactly as it is today.
func (p *Processor) speciesAcceptedToday(day, scientificName string) bool {
	if p.firstDaily.isAccepted(day, scientificName) {
		return true
	}
	if p.Ds == nil {
		return true
	}

	count, err := p.Ds.CountSpeciesDetections(scientificName, day, "", 0)
	if err != nil {
		GetLogger().Debug("first daily consensus: species lookup failed, accepting detection",
			logger.String("scientific_name", scientificName),
			logger.String("day", day),
			logger.Error(err),
			logger.String("operation", "first_daily_consensus"))
		return true
	}
	if count > 0 {
		p.firstDaily.markAccepted(day, scientificName)
		return true
	}
	return false
}
