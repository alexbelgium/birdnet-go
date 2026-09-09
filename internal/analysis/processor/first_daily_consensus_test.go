package processor

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore/mocks"
	"github.com/tphakala/birdnet-go/internal/detection"
)

const (
	consensusSpecies = "Parus major"
	consensusCommon  = "Great Tit"
	consensusSource  = "src1"
	birdNETModel     = "BirdNET_V2.4"
)

var (
	consensusAt  = time.Date(2026, 6, 11, 8, 0, 0, 0, time.UTC)
	consensusDay = consensusAt.Format(time.DateOnly)
)

// dbResult is what the datastore reports for the species today. A nil *dbResult
// in a test case means the datastore must not be consulted at all.
type dbResult struct {
	count int64
	err   error
}

// newConsensusSettings returns settings with the rule turned on. It ships off by
// default (see TestFirstDailyConsensusDisabledByDefault), so every case that
// exercises the rule must opt in explicitly.
func newConsensusSettings() *conf.Settings {
	settings := &conf.Settings{}
	settings.BirdNET.Threshold = 0.5
	settings.Realtime.FirstDailyConsensus.Enabled = true
	return settings
}

// newConsensusDetection builds a pending detection from per-model best scores.
func newConsensusDetection(modelID, rawLabel string, confidences map[string]float64) *PendingDetection {
	contributions := make(map[string]ModelContribution, len(confidences))
	for id, confidence := range confidences {
		contributions[id] = ModelContribution{HitCount: 1, MaxConfidence: confidence}
	}
	return &PendingDetection{
		Detection: Detections{
			Result: detection.Result{
				Timestamp: consensusAt,
				Species: detection.Species{
					CommonName:     consensusCommon,
					ScientificName: consensusSpecies,
				},
				RawLabel: rawLabel,
			},
		},
		Source:             consensusSource,
		BestModelID:        modelID,
		ModelContributions: contributions,
	}
}

// markSpeciesShared seeds the model-support memo so the gate behaves as if every
// active bird model knows the species. The support lookup itself is covered by
// TestSpeciesSharedByBirdModels in the classifier package; seeding it here keeps
// these cases focused on the gate's own decisions.
func markSpeciesShared(p *Processor, shared bool) {
	p.firstDaily.storeSupport(firstDailySupportKey(nil, consensusSpecies), shared)
}

// expectSpeciesCount wires a datastore that answers the "already accepted today"
// query exactly once.
func expectSpeciesCount(t *testing.T, db *dbResult) *mocks.MockInterface {
	t.Helper()
	ds := mocks.NewMockInterface(t)
	ds.EXPECT().
		CountSpeciesDetections(consensusSpecies, consensusDay, "", 0).
		Return(db.count, db.err).
		Once()
	return ds
}

func TestShouldDiscardFirstDailyDetection(t *testing.T) {
	t.Parallel()

	const perch = classifier.RegistryIDPerchV2

	tests := []struct {
		name        string
		confidences map[string]float64
		modelID     string
		rawLabel    string
		shared      bool
		db          *dbResult
		wantDiscard bool
	}{
		{
			name:        "first daily detection confirmed by two models is accepted",
			confidences: map[string]float64{birdNETModel: 0.8, perch: 0.7},
			modelID:     birdNETModel,
			shared:      true,
		},
		{
			name:        "first daily detection with only one model is discarded",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			shared:      true,
			db:          &dbResult{count: 0},
			wantDiscard: true,
		},
		{
			name:        "second model below its normal threshold does not confirm",
			confidences: map[string]float64{birdNETModel: 0.8, perch: 0.3},
			modelID:     birdNETModel,
			shared:      true,
			db:          &dbResult{count: 0},
			wantDiscard: true,
		},
		{
			name:        "later detection the same day is accepted",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			shared:      true,
			db:          &dbResult{count: 3},
		},
		{
			name:        "datastore error fails open",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			shared:      true,
			db:          &dbResult{err: errors.New("database is locked")},
		},
		{
			name:        "species not shared by every active bird model is untouched",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			shared:      false,
		},
		{
			name:        "bat detections are untouched",
			confidences: map[string]float64{classifier.RegistryIDBat: 0.8},
			modelID:     classifier.RegistryIDBat,
			shared:      true,
		},
		{
			name:        "non-species sound classes are untouched",
			confidences: map[string]float64{perch: 0.8},
			modelID:     perch,
			rawLabel:    "power_tool",
			shared:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Processor{}
			if tt.db != nil {
				p.Ds = expectSpeciesCount(t, tt.db)
			}
			markSpeciesShared(p, tt.shared)

			discard, reason := p.shouldDiscardFirstDailyDetection(
				newConsensusDetection(tt.modelID, tt.rawLabel, tt.confidences), newConsensusSettings())

			assert.Equal(t, tt.wantDiscard, discard)
			if tt.wantDiscard {
				assert.Equal(t, reasonFirstDailyConsensus, reason)
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}

// TestShouldDiscardFirstDailyDetection_DynamicThreshold covers the exception for
// a species whose bar dynamic thresholding has actually lowered: the operator
// asked for a more permissive gate, so one model is enough. An expired
// adjustment no longer counts as lowering it.
func TestShouldDiscardFirstDailyDetection_DynamicThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		timerOffset time.Duration
		db          *dbResult
		wantDiscard bool
	}{
		{name: "active adjustment lowers the bar", timerOffset: time.Hour},
		{name: "expired adjustment does not", timerOffset: -time.Hour, db: &dbResult{count: 0}, wantDiscard: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := newConsensusSettings()
			settings.Realtime.DynamicThreshold.Enabled = true
			settings.Realtime.DynamicThreshold.Min = 0.1

			p := &Processor{
				DynamicThresholds: map[string]*DynamicThreshold{
					"great tit": {Level: 2, Timer: time.Now().Add(tt.timerOffset)},
				},
			}
			if tt.db != nil {
				p.Ds = expectSpeciesCount(t, tt.db)
			}
			markSpeciesShared(p, true)

			discard, _ := p.shouldDiscardFirstDailyDetection(
				newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}), settings)

			assert.Equal(t, tt.wantDiscard, discard)
		})
	}
}

// TestNoteAcceptedDetection verifies that an approved detection is remembered
// immediately, so the next detection of that species skips the rule without
// waiting for asynchronous persistence. No datastore is wired, so a lookup would
// fail the test by nil-mock expectation.
func TestNoteAcceptedDetection(t *testing.T) {
	t.Parallel()

	p := &Processor{}
	markSpeciesShared(p, true)
	item := newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})

	p.noteAcceptedDetection(item)

	discard, _ := p.shouldDiscardFirstDailyDetection(item, newConsensusSettings())
	assert.False(t, discard, "the species was already accepted today")
}

// TestFirstDailyConsensusMemoizesAbsence pins the negative memo: a species the
// rule is holding back must not re-query the datastore on every flush. The mock
// allows the call exactly once across two gate evaluations.
func TestFirstDailyConsensusMemoizesAbsence(t *testing.T) {
	t.Parallel()

	p := &Processor{Ds: expectSpeciesCount(t, &dbResult{count: 0})}
	markSpeciesShared(p, true)
	settings := newConsensusSettings()

	for range 2 {
		item := newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})
		discard, _ := p.shouldDiscardFirstDailyDetection(item, settings)
		assert.True(t, discard)
	}
}

func TestFirstDailyConsensusRollsOverAtMidnight(t *testing.T) {
	t.Parallel()

	var c firstDailyConsensus
	c.markAccepted("2026-06-11", consensusSpecies)

	accepted, settled := c.acceptedToday("2026-06-11", consensusSpecies)
	require.True(t, settled)
	require.True(t, accepted)

	_, settled = c.acceptedToday("2026-06-12", consensusSpecies)
	assert.False(t, settled, "yesterday's state must not carry over")

	_, settled = c.acceptedToday("2026-06-11", consensusSpecies)
	assert.False(t, settled, "state is kept for one day only")
}

// TestFirstDailyConsensusSupportSurvivesMidnight pins the deliberate asymmetry:
// model support has no calendar semantics, so it must not be re-derived (which
// reloads every label set) at the start of the dawn chorus.
func TestFirstDailyConsensusSupportSurvivesMidnight(t *testing.T) {
	t.Parallel()

	var c firstDailyConsensus
	key := firstDailySupportKey([]string{birdNETModel}, consensusSpecies)
	c.storeSupport(key, true)
	c.markAccepted("2026-06-11", consensusSpecies)
	c.markAccepted("2026-06-12", consensusSpecies) // rolls the day

	shared, cached := c.lookupSupport(key)
	assert.True(t, cached)
	assert.True(t, shared)
}

func TestFirstDailySupportKeyIsModelSetSensitive(t *testing.T) {
	t.Parallel()

	one := firstDailySupportKey([]string{birdNETModel}, consensusSpecies)
	two := firstDailySupportKey([]string{birdNETModel, classifier.RegistryIDPerchV2}, consensusSpecies)

	assert.NotEqual(t, one, two, "changing the active model set must re-derive the verdict")
}

// TestDynamicThresholdKeyMatchesParseAndValidateSpecies pins the shared key
// derivation: the map is written under this key, so a divergence here would make
// the dynamic-threshold exception silently never fire.
func TestDynamicThresholdKeyMatchesParseAndValidateSpecies(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "great tit", dynamicThresholdKey(consensusCommon, consensusSpecies))
	assert.Equal(t, "parus major", dynamicThresholdKey("", consensusSpecies), "falls back to the scientific name")
	assert.Empty(t, dynamicThresholdKey("", ""))
}

// TestFirstDailyConsensusDisabledByDefault pins the opt-in contract: on a
// zero-value config the rule must not touch a detection it would otherwise
// discard, and must not reach the datastore to decide that. The mock has no
// CountSpeciesDetections expectation, so any query fails the test.
func TestFirstDailyConsensusDisabledByDefault(t *testing.T) {
	t.Parallel()

	settings := &conf.Settings{}
	settings.BirdNET.Threshold = 0.5
	require.False(t, settings.Realtime.FirstDailyConsensus.Enabled)

	p := &Processor{Ds: mocks.NewMockInterface(t)}
	markSpeciesShared(p, true)

	// Exactly the case TestShouldDiscardFirstDailyDetection discards when enabled.
	discard, reason := p.shouldDiscardFirstDailyDetection(
		newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}), settings)

	assert.False(t, discard, "the rule must be inert until enabled")
	assert.Empty(t, reason)
}

// TestShouldDiscardFirstDailyDetection_NilOrchestrator is a permanent regression
// test for a nil p.Bn (a zero-value Processor, or any path reached before the
// orchestrator is wired). classifier.Orchestrator.SpeciesSharedByBirdModels
// guards a nil receiver before touching any field, so this must fail open rather
// than panic.
func TestShouldDiscardFirstDailyDetection_NilOrchestrator(t *testing.T) {
	t.Parallel()

	p := &Processor{} // p.Bn is nil
	item := newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})

	assert.NotPanics(t, func() {
		discard, _ := p.shouldDiscardFirstDailyDetection(item, newConsensusSettings())
		assert.False(t, discard, "an unevaluable orchestrator must fail open")
	})
}

// TestFirstDailySupportTTLExpires pins the bound on stale model-support verdicts:
// a model reload or variant swap can replace a model's label set while keeping
// its registry ID, which the cache key cannot see, so an entry must not be
// trusted forever. storeSupportUntil seeds an already-expired entry directly,
// standing in for firstDailySupportTTL's passage without a real wait.
func TestFirstDailySupportTTLExpires(t *testing.T) {
	t.Parallel()

	var c firstDailyConsensus
	key := firstDailySupportKey([]string{birdNETModel}, consensusSpecies)

	c.storeSupportUntil(key, true, time.Now().Add(-time.Second))
	_, cached := c.lookupSupport(key)
	assert.False(t, cached, "an expired entry must not be served")

	c.storeSupportUntil(key, true, time.Now().Add(time.Minute))
	shared, cached := c.lookupSupport(key)
	require.True(t, cached)
	assert.True(t, shared)
}

// TestWarmFirstDailyAcceptance verifies the datastore-warming pass: it resolves
// the "already accepted today" memo from p.pendingDetections under its own read
// lock, so shouldDiscardFirstDailyDetection reaches the datastore at most once
// even though it runs later, under flushPendingDetections's exclusive lock, for
// a different PendingDetection value of the same species.
func TestWarmFirstDailyAcceptance(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	p := &Processor{
		Ds: expectSpeciesCount(t, &dbResult{count: 0}),
		pendingDetections: map[string]PendingDetection{
			"src1:parus major": *newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}),
		},
	}
	markSpeciesShared(p, true)

	p.warmFirstDailyAcceptance(settings)

	// A later, distinct PendingDetection value for the same species must reuse the
	// memo the warm pass populated rather than querying again — the mock's Once()
	// on expectSpeciesCount enforces that.
	discard, reason := p.shouldDiscardFirstDailyDetection(
		newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.7}), settings)

	assert.True(t, discard)
	assert.Equal(t, reasonFirstDailyConsensus, reason)
}

// TestWarmFirstDailyAcceptance_SkipsIneligibleDetections verifies the prewarm
// pass shares shouldDiscardFirstDailyDetection's own eligibility checks, so it
// never queries the datastore for a species this rule would never gate anyway.
func TestWarmFirstDailyAcceptance_SkipsIneligibleDetections(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	p := &Processor{
		Ds: mocks.NewMockInterface(t), // no CountSpeciesDetections expectation set
		pendingDetections: map[string]PendingDetection{
			"src1:pipistrellus pipistrellus": *newConsensusDetection(classifier.RegistryIDBat, "", map[string]float64{classifier.RegistryIDBat: 0.8}),
		},
	}

	assert.NotPanics(t, func() {
		p.warmFirstDailyAcceptance(settings)
	})
}
