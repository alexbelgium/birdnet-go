package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
)

// TestGetNewSpecies_ExcludesFalsePositives guards the "New Species Detected" chart against counting
// species whose detections were all reviewed as false positives, and against a species' lifetime
// first-seen being pinned to a false-positive detection.
func TestGetNewSpecies_ExcludesFalsePositives(t *testing.T) {
	db := setupInsightsTestDB(t)
	repo := NewDetectionRepository(db, nil, false, false)
	ctx := t.Context()

	withFP := seedLabel(t, db, "Parus major")
	allFP := seedLabel(t, db, "Turdus merula")

	// Parus major's earliest detection is a false positive: first-seen must skip it for the next real one.
	fpID := seedDetection(t, db, withFP, 1000, 0.6)
	seedFalsePositiveReview(t, db, fpID)
	seedDetection(t, db, withFP, 1800, 0.9)

	// Turdus merula has only a false-positive detection: it is not a newly detected species.
	allFPID := seedDetection(t, db, allFP, 1200, 0.5)
	seedFalsePositiveReview(t, db, allFPID)

	got, err := repo.GetNewSpecies(ctx, 500, 5000, 100, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Parus major", got[0].ScientificName)
	assert.Equal(t, int64(1800), got[0].FirstDetected)
}

// TestGetNewSpecies_FalsePositiveBeforeWindowDoesNotHideNewSpecies checks the exclusion is applied
// when computing the lifetime first-seen, not only within the window. A false positive predating the
// window must not count as prior history: the species is genuinely new and must still be reported.
// This is the inverse failure of the reported bug — the old query hid real new species this way.
func TestGetNewSpecies_FalsePositiveBeforeWindowDoesNotHideNewSpecies(t *testing.T) {
	db := setupInsightsTestDB(t)
	repo := NewDetectionRepository(db, nil, false, false)
	ctx := t.Context()

	label := seedLabel(t, db, "Parus major")
	fpID := seedDetection(t, db, label, 100, 0.6) // false positive before the window
	seedFalsePositiveReview(t, db, fpID)
	seedDetection(t, db, label, 1000, 0.8) // first real detection, inside the window

	got, err := repo.GetNewSpecies(ctx, 500, 5000, 100, 0)
	require.NoError(t, err)
	require.Len(t, got, 1, "a species whose only prior detection was a false positive is still new")
	assert.Equal(t, "Parus major", got[0].ScientificName)
	assert.Equal(t, int64(1000), got[0].FirstDetected)
}

// TestGetNewSpecies_RealDetectionBeforeWindowIsNotNew guards the window bound itself while the
// false-positive filter moves around it: genuine prior history still disqualifies a species.
func TestGetNewSpecies_RealDetectionBeforeWindowIsNotNew(t *testing.T) {
	db := setupInsightsTestDB(t)
	repo := NewDetectionRepository(db, nil, false, false)
	ctx := t.Context()

	label := seedLabel(t, db, "Parus major")
	seedDetection(t, db, label, 100, 0.9) // real detection before the window
	seedDetection(t, db, label, 1000, 0.8)

	got, err := repo.GetNewSpecies(ctx, 500, 5000, 100, 0)
	require.NoError(t, err)
	assert.Empty(t, got, "species first detected before the window is not new")
}

// TestGetNewSpecies_AcrossModelsAndLifetime pins that the candidate restriction keeps the
// species-level semantics: prior history under another model's label disqualifies a species,
// and a reported species keeps its lifetime last detection, which lies after the window.
func TestGetNewSpecies_AcrossModelsAndLifetime(t *testing.T) {
	db := setupInsightsTestDB(t)
	repo := NewDetectionRepository(db, nil, false, false)
	ctx := t.Context()
	require.NoError(t, db.Create(&entities.AIModel{ID: 2, Name: "Perch", Version: "2", ModelType: entities.ModelTypeBird}).Error)

	// Turdus merula was heard by model 2 before the window: not new, although model 1's label is.
	oldOther := seedLabelForModel(t, db, "Turdus merula", 2)
	seedDetection(t, db, oldOther, 100, 0.9)
	seedDetection(t, db, seedLabel(t, db, "Turdus merula"), 1000, 0.8)

	// Parus major is new; its later detections, under both models, set its last-seen.
	newLabel := seedLabel(t, db, "Parus major")
	seedDetection(t, db, newLabel, 1500, 0.7)
	seedDetection(t, db, seedLabelForModel(t, db, "Parus major", 2), 8000, 0.6)

	got, err := repo.GetNewSpecies(ctx, 500, 5000, 100, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Parus major", got[0].ScientificName)
	assert.Equal(t, newLabel, got[0].LabelID)
	assert.Equal(t, int64(1500), got[0].FirstDetected)
	assert.Equal(t, int64(8000), got[0].LastDetected)
	assert.Equal(t, 1, got[0].CountInPeriod)
}

// TestGetNewSpecies_CountInPeriod pins count_in_period to the number of detections of the
// species inside the queried window, excluding false positives and detections after the window.
// The field was declared and always reported as zero.
func TestGetNewSpecies_CountInPeriod(t *testing.T) {
	db := setupInsightsTestDB(t)
	repo := NewDetectionRepository(db, nil, false, false)
	ctx := t.Context()

	label := seedLabel(t, db, "Megascops asio")
	seedDetection(t, db, label, 600, 0.7)          // first ever, inside the window
	seedDetection(t, db, label, 900, 0.8)          // inside
	fpID := seedDetection(t, db, label, 1200, 0.6) // inside but reviewed away
	seedFalsePositiveReview(t, db, fpID)
	seedDetection(t, db, label, 5000, 0.9) // exactly at the end: the window is [start, end)
	seedDetection(t, db, label, 9000, 0.9) // after the window

	got, err := repo.GetNewSpecies(ctx, 500, 5000, 100, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Megascops asio", got[0].ScientificName)
	assert.Equal(t, int64(600), got[0].FirstDetected)
	assert.Equal(t, 2, got[0].CountInPeriod)
}
