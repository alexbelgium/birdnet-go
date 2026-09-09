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
	testSpecies     = "Parus major"
	testCommonName  = "Great Tit"
	consensusSource = "src1"
	testThreshold   = 0.5
)

var testDetectedAt = time.Date(2026, 6, 11, 8, 0, 0, 0, time.UTC)

// newConsensusSettings returns settings where every model shares one 0.5
// threshold and dynamic thresholding is off.
func newConsensusSettings() *conf.Settings {
	settings := &conf.Settings{}
	settings.BirdNET.Threshold = testThreshold
	return settings
}

// newConsensusDetection builds a pending detection for the given per-model best
// confidences.
func newConsensusDetection(modelID, rawLabel string, confidences map[string]float64) *PendingDetection {
	contributions := make(map[string]ModelContribution, len(confidences))
	for id, confidence := range confidences {
		contributions[id] = ModelContribution{HitCount: 1, MaxConfidence: confidence}
	}
	return &PendingDetection{
		Detection: Detections{
			Result: detection.Result{
				Species: detection.Species{
					CommonName:     testCommonName,
					ScientificName: testSpecies,
				},
				RawLabel: rawLabel,
			},
		},
		Source:             consensusSource,
		BestModelID:        modelID,
		FirstDetected:      testDetectedAt,
		Count:              5,
		ModelContributions: contributions,
	}
}

// markSpeciesShared seeds the model-support memo so the gate behaves as if every
// active bird model knows the species. The support lookup itself is covered by
// TestBirdModelSpeciesSupport in the classifier package; seeding it here keeps
// these cases focused on the gate's own decisions.
func markSpeciesShared(p *Processor, shared bool) {
	day := testDetectedAt.Format(firstDailyDayFormat)
	p.firstDaily.storeSupport(day, firstDailySupportKey(nil, testSpecies), shared)
}

func TestShouldDiscardFirstDailyDetection(t *testing.T) {
	t.Parallel()

	const (
		birdNET = "BirdNET_V2.4"
		perch   = classifier.RegistryIDPerchV2
	)

	tests := []struct {
		name string
		// confidences is the best score each model contributed.
		confidences map[string]float64
		modelID     string
		rawLabel    string
		shared      bool
		// dbCount is what the datastore reports for the species today; a nil
		// datastore is used when expectDB is false.
		dbCount     int64
		dbErr       error
		expectDB    bool
		wantDiscard bool
	}{
		{
			name:        "first daily detection confirmed by two models is accepted",
			confidences: map[string]float64{birdNET: 0.8, perch: 0.7},
			modelID:     birdNET,
			shared:      true,
			wantDiscard: false,
		},
		{
			name:        "first daily detection with only one model is discarded",
			confidences: map[string]float64{birdNET: 0.8},
			modelID:     birdNET,
			shared:      true,
			expectDB:    true,
			dbCount:     0,
			wantDiscard: true,
		},
		{
			name:        "second model below its normal threshold does not confirm",
			confidences: map[string]float64{birdNET: 0.8, perch: 0.3},
			modelID:     birdNET,
			shared:      true,
			expectDB:    true,
			dbCount:     0,
			wantDiscard: true,
		},
		{
			name:        "later detection the same day is accepted",
			confidences: map[string]float64{birdNET: 0.8},
			modelID:     birdNET,
			shared:      true,
			expectDB:    true,
			dbCount:     3,
			wantDiscard: false,
		},
		{
			name:        "datastore error fails open",
			confidences: map[string]float64{birdNET: 0.8},
			modelID:     birdNET,
			shared:      true,
			expectDB:    true,
			dbErr:       errors.New("database is locked"),
			wantDiscard: false,
		},
		{
			name:        "species not shared by every active bird model is untouched",
			confidences: map[string]float64{birdNET: 0.8},
			modelID:     birdNET,
			shared:      false,
			wantDiscard: false,
		},
		{
			name:        "bat detections are untouched",
			confidences: map[string]float64{classifier.RegistryIDBat: 0.8},
			modelID:     classifier.RegistryIDBat,
			shared:      true,
			wantDiscard: false,
		},
		{
			name:        "non-species sound classes are untouched",
			confidences: map[string]float64{perch: 0.8},
			modelID:     perch,
			rawLabel:    "power_tool",
			shared:      true,
			wantDiscard: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := newConsensusSettings()
			p := &Processor{}

			if tt.expectDB {
				ds := mocks.NewMockInterface(t)
				day := testDetectedAt.Format(firstDailyDayFormat)
				ds.EXPECT().
					CountSpeciesDetections(testSpecies, day, "", 0).
					Return(tt.dbCount, tt.dbErr).
					Once()
				p.Ds = ds
			}

			markSpeciesShared(p, tt.shared)
			item := newConsensusDetection(tt.modelID, tt.rawLabel, tt.confidences)

			discard, reason := p.shouldDiscardFirstDailyDetection(item, settings)

			assert.Equal(t, tt.wantDiscard, discard)
			if tt.wantDiscard {
				assert.Equal(t, reasonFirstDailyConsensus, reason)
			} else {
				assert.Empty(t, reason)
			}
		})
	}
}

// TestShouldDiscardFirstDailyDetection_DynamicThresholdLowered covers the
// exception for a species whose bar dynamic thresholding has actually lowered:
// the operator asked for a more permissive gate, so one model is enough.
func TestShouldDiscardFirstDailyDetection_DynamicThresholdLowered(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	settings.Realtime.DynamicThreshold.Enabled = true
	settings.Realtime.DynamicThreshold.Min = 0.1

	p := &Processor{
		DynamicThresholds: map[string]*DynamicThreshold{
			"great tit": {Level: 2, Timer: time.Now().Add(time.Hour)},
		},
	}
	markSpeciesShared(p, true)

	item := newConsensusDetection("BirdNET_V2.4", "", map[string]float64{"BirdNET_V2.4": 0.8})

	discard, _ := p.shouldDiscardFirstDailyDetection(item, settings)
	assert.False(t, discard, "a lowered dynamic threshold must not be overridden by a stricter rule")
}

// TestShouldDiscardFirstDailyDetection_DynamicThresholdExpired verifies that an
// expired dynamic threshold no longer counts as lowering the bar.
func TestShouldDiscardFirstDailyDetection_DynamicThresholdExpired(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	settings.Realtime.DynamicThreshold.Enabled = true
	settings.Realtime.DynamicThreshold.Min = 0.1

	ds := mocks.NewMockInterface(t)
	ds.EXPECT().
		CountSpeciesDetections(testSpecies, testDetectedAt.Format(firstDailyDayFormat), "", 0).
		Return(int64(0), nil).
		Once()

	p := &Processor{
		Ds: ds,
		DynamicThresholds: map[string]*DynamicThreshold{
			"great tit": {Level: 2, Timer: time.Now().Add(-time.Hour)},
		},
	}
	markSpeciesShared(p, true)

	item := newConsensusDetection("BirdNET_V2.4", "", map[string]float64{"BirdNET_V2.4": 0.8})

	discard, _ := p.shouldDiscardFirstDailyDetection(item, settings)
	assert.True(t, discard)
}

// TestNoteAcceptedDetection verifies that an approved detection is remembered
// immediately, so the next detection of that species skips the rule without
// waiting for asynchronous persistence. A datastore that would report zero is
// wired in to prove the memo answers first.
func TestNoteAcceptedDetection(t *testing.T) {
	t.Parallel()

	settings := newConsensusSettings()
	p := &Processor{}
	markSpeciesShared(p, true)

	item := newConsensusDetection("BirdNET_V2.4", "", map[string]float64{"BirdNET_V2.4": 0.8})
	p.noteAcceptedDetection(item)

	discard, _ := p.shouldDiscardFirstDailyDetection(item, settings)
	assert.False(t, discard, "the species was already accepted today")
}

func TestFirstDailyConsensusRollsOverAtMidnight(t *testing.T) {
	t.Parallel()

	var c firstDailyConsensus
	c.markAccepted("2026-06-11", testSpecies)
	require.True(t, c.isAccepted("2026-06-11", testSpecies))

	assert.False(t, c.isAccepted("2026-06-12", testSpecies), "yesterday's acceptance must not carry over")
	assert.False(t, c.isAccepted("2026-06-11", testSpecies), "state is kept for one day only")
}

func TestFirstDailyDay(t *testing.T) {
	t.Parallel()

	t.Run("uses the time the detection was heard", func(t *testing.T) {
		t.Parallel()
		item := &PendingDetection{FirstDetected: testDetectedAt, CreatedAt: testDetectedAt.Add(48 * time.Hour)}
		assert.Equal(t, "2026-06-11", firstDailyDay(item))
	})

	t.Run("falls back to creation time", func(t *testing.T) {
		t.Parallel()
		item := &PendingDetection{CreatedAt: testDetectedAt}
		assert.Equal(t, "2026-06-11", firstDailyDay(item))
	})

	t.Run("reports nothing without a timestamp", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, firstDailyDay(&PendingDetection{}))
	})
}

func TestFirstDailySupportKeyIsModelSetSensitive(t *testing.T) {
	t.Parallel()

	one := firstDailySupportKey(map[string]struct{}{"BirdNET_V2.4": {}}, testSpecies)
	two := firstDailySupportKey(map[string]struct{}{"BirdNET_V2.4": {}, classifier.RegistryIDPerchV2: {}}, testSpecies)

	assert.NotEqual(t, one, two, "changing the active model set must re-derive the verdict")

	// Map iteration order must not leak into the key.
	assert.Equal(t,
		firstDailySupportKey(map[string]struct{}{"a": {}, "b": {}}, testSpecies),
		firstDailySupportKey(map[string]struct{}{"b": {}, "a": {}}, testSpecies))
}
