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

// dbResult is what the datastore reports for the species today.
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

// pendingDue puts item in the pending map with a deadline already passed, which
// is the set warmFirstDailyAcceptance considers.
func pendingDue(item *PendingDetection) map[string]PendingDetection {
	item.FlushDeadline = consensusAt.Add(-time.Second)
	return map[string]PendingDetection{pendingKeyForDetection(consensusSource, &item.Detection): *item}
}

func TestShouldDiscardFirstDailyDetection(t *testing.T) {
	t.Parallel()

	const perch = classifier.RegistryIDPerchV2

	// memo is the per-day answer warmFirstDailyAcceptance would have left behind:
	// nil means it could not resolve one, which the gate must fail open on.
	accepted, absent := true, false

	tests := []struct {
		name        string
		confidences map[string]float64
		modelID     string
		rawLabel    string
		shared      bool
		memo        *bool
		wantDiscard bool
	}{
		{
			name:        "first daily detection confirmed by two models is accepted",
			confidences: map[string]float64{birdNETModel: 0.8, perch: 0.7},
			modelID:     birdNETModel,
			shared:      true,
			memo:        &absent,
		},
		{
			name:        "first daily detection with only one model is discarded",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			shared:      true,
			memo:        &absent,
			wantDiscard: true,
		},
		{
			name:        "second model below its normal threshold does not confirm",
			confidences: map[string]float64{birdNETModel: 0.8, perch: 0.3},
			modelID:     birdNETModel,
			shared:      true,
			memo:        &absent,
			wantDiscard: true,
		},
		{
			name:        "later detection the same day is accepted",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			shared:      true,
			memo:        &accepted,
		},
		{
			name:        "an unresolved memo fails open",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			shared:      true,
		},
		{
			name:        "species not shared by every active bird model is untouched",
			confidences: map[string]float64{birdNETModel: 0.8},
			modelID:     birdNETModel,
			memo:        &absent,
		},
		{
			name:        "bat detections are untouched",
			confidences: map[string]float64{classifier.RegistryIDBat: 0.8},
			modelID:     classifier.RegistryIDBat,
			shared:      true,
			memo:        &absent,
		},
		{
			name:        "non-species sound classes are untouched",
			confidences: map[string]float64{perch: 0.8},
			modelID:     perch,
			rawLabel:    "power_tool",
			shared:      true,
			memo:        &absent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// No datastore: the gate must decide entirely from memory.
			p := &Processor{}
			markSpeciesShared(p, tt.shared)
			if tt.memo != nil {
				p.firstDaily.markChecked(consensusDay, consensusSpecies, *tt.memo)
			}

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
		wantDiscard bool
	}{
		{name: "active adjustment lowers the bar", timerOffset: time.Hour},
		{name: "expired adjustment does not", timerOffset: -time.Hour, wantDiscard: true},
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
			markSpeciesShared(p, true)
			p.firstDaily.markChecked(consensusDay, consensusSpecies, false)

			discard, _ := p.shouldDiscardFirstDailyDetection(
				newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}), settings)

			assert.Equal(t, tt.wantDiscard, discard)
		})
	}
}

// TestNoteAcceptedDetection verifies that an approved detection is remembered
// immediately, so the next detection of that species skips the rule without
// waiting for asynchronous persistence.
func TestNoteAcceptedDetection(t *testing.T) {
	t.Parallel()

	p := &Processor{}
	markSpeciesShared(p, true)
	item := newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})
	p.firstDaily.markChecked(consensusDay, consensusSpecies, false)

	p.noteAcceptedDetection(item)

	discard, _ := p.shouldDiscardFirstDailyDetection(item, newConsensusSettings())
	assert.False(t, discard, "the species was already accepted today")
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

// TestFirstDailySupportTTLExpires pins the bound on stale model-support verdicts:
// a model reload or variant swap can replace a model's label set while keeping
// its registry ID, which the cache key cannot see, so an entry must not be
// trusted forever.
func TestFirstDailySupportTTLExpires(t *testing.T) {
	t.Parallel()

	key := firstDailySupportKey([]string{birdNETModel}, consensusSpecies)
	c := firstDailyConsensus{
		support: map[string]supportEntry{key: {shared: true, expiresAt: time.Now().Add(-time.Second)}},
	}

	_, cached := c.lookupSupport(key)
	assert.False(t, cached, "an expired entry must not be served")
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

// TestShouldDiscardFirstDailyDetection_NilOrchestrator is a permanent regression
// test for a nil p.Bn (a zero-value Processor, or any path reached before the
// orchestrator is wired). classifier.Orchestrator.SpeciesSharedByBirdModels
// guards a nil receiver before touching any field, so this must fail open rather
// than panic.
func TestShouldDiscardFirstDailyDetection_NilOrchestrator(t *testing.T) {
	t.Parallel()

	// No seeded support memo either, so speciesSharedByActiveBirdModels has to ask
	// the nil orchestrator.
	p := &Processor{} // p.Bn is nil
	p.firstDaily.markChecked(consensusDay, consensusSpecies, false)
	item := newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})

	discard, _ := p.shouldDiscardFirstDailyDetection(item, newConsensusSettings())
	assert.False(t, discard, "an unevaluable orchestrator must fail open")
}

// TestFirstDailyConsensusRechecksEligibilityAgainstStaleMemo pins the ordering
// between the memo and the eligibility checks. A species that was shared by
// every active bird model when the warm pass recorded "not accepted yet" can
// stop being shared if models are reconfigured mid-day. Reading that memo before
// re-checking eligibility would keep discarding the species for the rest of the
// day on the strength of an answer whose premise no longer holds.
func TestFirstDailyConsensusRechecksEligibilityAgainstStaleMemo(t *testing.T) {
	t.Parallel()

	p := &Processor{}
	p.firstDaily.markChecked(consensusDay, consensusSpecies, false)
	markSpeciesShared(p, false) // the reconfiguration

	discard, _ := p.shouldDiscardFirstDailyDetection(
		newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}), newConsensusSettings())

	assert.False(t, discard, "a species no longer shared must be exempt despite the memo")
}

// TestWarmFirstDailyAcceptance is the two-phase flow: the warm pass resolves the
// memo from the datastore off the lock, and the gate then decides from memory
// alone. The mock's Once() proves the later gate call issues no second query.
func TestWarmFirstDailyAcceptance(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	p := &Processor{
		Ds:                expectSpeciesCount(t, &dbResult{count: 0}),
		pendingDetections: pendingDue(newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})),
	}
	markSpeciesShared(p, true)

	p.warmFirstDailyAcceptance(consensusAt, settings)

	discard, reason := p.shouldDiscardFirstDailyDetection(
		newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.7}), settings)

	assert.True(t, discard)
	assert.Equal(t, reasonFirstDailyConsensus, reason)
}

// TestWarmFirstDailyAcceptance_AcceptedSpecies covers the other datastore answer:
// a species already logged today settles the memo as accepted, so the gate lets
// the detection through.
func TestWarmFirstDailyAcceptance_AcceptedSpecies(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	p := &Processor{
		Ds:                expectSpeciesCount(t, &dbResult{count: 3}),
		pendingDetections: pendingDue(newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})),
	}
	markSpeciesShared(p, true)

	p.warmFirstDailyAcceptance(consensusAt, settings)

	discard, _ := p.shouldDiscardFirstDailyDetection(
		newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}), settings)

	assert.False(t, discard)
}

// TestWarmFirstDailyAcceptance_DatastoreErrorFailsOpen pins the failure mode: an
// error leaves the memo unresolved, and the gate accepts rather than retrying the
// query under flushPendingDetections's exclusive lock.
func TestWarmFirstDailyAcceptance_DatastoreErrorFailsOpen(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	p := &Processor{
		Ds:                expectSpeciesCount(t, &dbResult{err: errors.New("database is locked")}),
		pendingDetections: pendingDue(newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})),
	}
	markSpeciesShared(p, true)

	p.warmFirstDailyAcceptance(consensusAt, settings)

	discard, _ := p.shouldDiscardFirstDailyDetection(
		newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}), settings)

	assert.False(t, discard, "an unanswerable query must not discard")
}

// TestWarmFirstDailyAcceptance_SkipsWhenDisabled pins the opt-in boundary: on the
// default config the pass must return before locking or querying anything. The
// mock has no expectation set, so any query fails the test.
func TestWarmFirstDailyAcceptance_SkipsWhenDisabled(t *testing.T) {
	t.Parallel()

	settings := &conf.Settings{}
	settings.BirdNET.Threshold = 0.5

	p := &Processor{
		Ds:                mocks.NewMockInterface(t),
		pendingDetections: pendingDue(newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})),
	}
	markSpeciesShared(p, true)

	p.warmFirstDailyAcceptance(consensusAt, settings)

	discard, _ := p.shouldDiscardFirstDailyDetection(
		newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8}), settings)

	assert.False(t, discard, "the rule must be inert until enabled")
}

// TestWarmFirstDailyAcceptance_SkipsNotYetDue pins the deadline filter: warming a
// detection that will not flush for another ~12 ticks would re-derive the same
// verdict every second and query for detections the min-count filter may still
// discard.
func TestWarmFirstDailyAcceptance_SkipsNotYetDue(t *testing.T) {
	t.Parallel()

	item := newConsensusDetection(birdNETModel, "", map[string]float64{birdNETModel: 0.8})
	item.FlushDeadline = consensusAt.Add(time.Minute)

	p := &Processor{
		Ds: mocks.NewMockInterface(t), // no expectation: a query fails the test
		pendingDetections: map[string]PendingDetection{
			pendingKeyForDetection(consensusSource, &item.Detection): *item,
		},
	}
	markSpeciesShared(p, true)

	p.warmFirstDailyAcceptance(consensusAt, newConsensusSettings())
}

// TestWarmFirstDailyAcceptance_SkipsIneligibleDetections verifies the pass shares
// the gate's own eligibility checks, so it never queries for a species this rule
// would never gate anyway. The mock has no expectation set.
func TestWarmFirstDailyAcceptance_SkipsIneligibleDetections(t *testing.T) {
	t.Parallel()

	p := &Processor{
		Ds:                mocks.NewMockInterface(t),
		pendingDetections: pendingDue(newConsensusDetection(classifier.RegistryIDBat, "", map[string]float64{classifier.RegistryIDBat: 0.8})),
	}

	p.warmFirstDailyAcceptance(consensusAt, newConsensusSettings())
}
