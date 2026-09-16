package v2only

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
)

func TestSpeciesToolsLegacyLabelsAndProjection(t *testing.T) {
	ds, cleanup := setupTestDatastore(t)
	defer cleanup()
	now := time.Now().Truncate(time.Second)
	clean := seedDetection(t, ds, "Turdus merula", now)
	legacy := seedDetection(t, ds, "Turdus merula_Common Blackbird", now.Add(time.Hour))
	rejected := seedDetection(t, ds, "Tyto alba", now)
	require.NoError(t, ds.manager.DB().Create(&entities.DetectionReview{DetectionID: rejected.ID, Verified: "false_positive"}).Error)
	rows, err := ds.GetSpeciesTools(t.Context(), []string{"count", "max_confidence", "last_heard"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.EqualValues(t, 2, *rows[0].Count)
	assert.Equal(t, now.Add(time.Hour).Unix(), *rows[0].LastTimestamp)
	assert.NotNil(t, rows[0].LastHeard)
	assert.Nil(t, rows[1].MaxConfidence)
	require.NoError(t, ds.manager.DB().Model(&entities.Detection{}).Where("id = ?", clean.ID).Updates(map[string]any{"clip_name": "clean.wav", "confidence": 0.99}).Error)
	require.NoError(t, ds.manager.DB().Model(&entities.Detection{}).Where("id = ?", legacy.ID).Updates(map[string]any{"clip_name": "legacy.wav", "confidence": 0.8}).Error)
	require.NoError(t, ds.manager.DB().Create(&entities.DetectionLock{DetectionID: legacy.ID}).Error)
	candidates, err := ds.GetSpeciesToolRecordings(t.Context(), []string{"Turdus merula"}, 0)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	assert.Equal(t, legacy.ID, candidates[0].ID)
	assert.Equal(t, "Turdus merula", candidates[0].ScientificName)
	assert.True(t, candidates[0].Locked)
	assert.Equal(t, clean.ID, candidates[1].ID)
	require.NoError(t, ds.manager.DB().Migrator().DropTable(&entities.DetectionReview{}))
	rows, err = ds.GetSpeciesTools(t.Context(), []string{"count", "last_heard"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.EqualValues(t, 2, *rows[0].Count)
	assert.Nil(t, rows[0].MaxConfidence)
	rows, err = ds.GetSpeciesTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Nil(t, rows[0].Count)
	assert.Nil(t, rows[0].MaxConfidence)
	assert.Nil(t, rows[0].LastHeard)
}

// TestV2OnlyDatastore_GetSpeciesReviewStats verifies per-species total/verified/
// rejected counts including false positives.
func TestV2OnlyDatastore_GetSpeciesReviewStats(t *testing.T) {
	ds, cleanup := setupTestDatastore(t)
	defer cleanup()

	saveTestNote(t, ds, "2024-01-15", "08:00:00", "Passer domesticus", 0.85) // ID 1
	saveTestNote(t, ds, "2024-01-15", "09:00:00", "Passer domesticus", 0.90) // ID 2
	saveTestNote(t, ds, "2024-01-16", "10:00:00", "Turdus merula", 0.80)     // ID 3

	require.NoError(t, ds.SaveNoteReview(&datastore.NoteReview{NoteID: 1, Verified: "correct"}))
	require.NoError(t, ds.SaveNoteReview(&datastore.NoteReview{NoteID: 3, Verified: "false_positive"}))

	stats, err := ds.GetSpeciesReviewStats(t.Context())
	require.NoError(t, err)

	byName := make(map[string]datastore.SpeciesReviewStat, len(stats))
	for _, s := range stats {
		byName[s.ScientificName] = s
	}

	assert.Equal(t, 2, byName["Passer domesticus"].Total)
	assert.Equal(t, 1, byName["Passer domesticus"].Verified)
	assert.Equal(t, 0, byName["Passer domesticus"].Rejected)
	// CommonName is resolved via resolveCommonName; with no name maps loaded in the
	// test it falls back to the scientific name. The fully-rejected-species UI relies
	// on this field being populated so it can render a row for the deleted species.
	assert.Equal(t, "Passer domesticus", byName["Passer domesticus"].CommonName)

	assert.Equal(t, 1, byName["Turdus merula"].Total)
	assert.Equal(t, 0, byName["Turdus merula"].Verified)
	assert.Equal(t, 1, byName["Turdus merula"].Rejected) // false positive counted
	assert.Equal(t, "Turdus merula", byName["Turdus merula"].CommonName)
}

// TestV2OnlyDatastore_GetSpeciesNoteIDs verifies note-ID lookup by scientific name.
func TestV2OnlyDatastore_GetSpeciesNoteIDs(t *testing.T) {
	ds, cleanup := setupTestDatastore(t)
	defer cleanup()

	saveTestNote(t, ds, "2024-01-15", "08:00:00", "Passer domesticus", 0.85) // ID 1
	saveTestNote(t, ds, "2024-01-15", "09:00:00", "Passer domesticus", 0.90) // ID 2
	saveTestNote(t, ds, "2024-01-16", "10:00:00", "Turdus merula", 0.80)     // ID 3

	ids, err := ds.GetSpeciesNoteIDs(t.Context(), "Passer domesticus")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"1", "2"}, ids)

	none, err := ds.GetSpeciesNoteIDs(t.Context(), "Nonexistent species")
	require.NoError(t, err)
	assert.Empty(t, none)
}

// TestV2OnlyDatastore_GetSpeciesNoteIDs_LegacyLabelFormat is a regression test for
// labels stored in the legacy "ScientificName_CommonName" form: a lookup by the
// clean scientific name must still resolve the detection, and review stats must
// report the extracted scientific name.
func TestV2OnlyDatastore_GetSpeciesNoteIDs_LegacyLabelFormat(t *testing.T) {
	ds, cleanup := setupTestDatastore(t)
	defer cleanup()

	ctx := t.Context()
	// Create a legacy concatenated label and a detection referencing it directly,
	// bypassing Save's ExtractScientificName normalization so the underscore form
	// is actually persisted.
	label, err := ds.label.GetOrCreate(ctx, "Tyto alba_Barn Owl", ds.defaultModelID, ds.speciesLabelTypeID, ds.avesClassID)
	require.NoError(t, err)
	det := &entities.Detection{ModelID: ds.defaultModelID, LabelID: label.ID, DetectedAt: time.Now().Unix(), Confidence: 0.9}
	require.NoError(t, ds.detection.Save(ctx, det))

	ids, err := ds.GetSpeciesNoteIDs(ctx, "Tyto alba")
	require.NoError(t, err)
	assert.Equal(t, []string{fmt.Sprintf("%d", det.ID)}, ids)

	stats, err := ds.GetSpeciesReviewStats(ctx)
	require.NoError(t, err)
	found := false
	for _, s := range stats {
		if s.ScientificName == "Tyto alba" {
			found = true
			assert.Equal(t, 1, s.Total)
		}
	}
	assert.True(t, found, "review stats should report the extracted scientific name")
}

// TestV2OnlyDatastore_GetSpeciesReviewStats_MergesDuplicateLabels is a regression
// test for review-stats aggregation across label variants: a species present under
// both a clean scientific-name label and a legacy "ScientificName_CommonName" label
// (distinct label rows) must collapse into a SINGLE merged stat row. The query
// groups by the raw label scientific_name, so without merging
// the Manage view — which keys stats by scientific name — silently drops (undercounts)
// one label's counts.
func TestV2OnlyDatastore_GetSpeciesReviewStats_MergesDuplicateLabels(t *testing.T) {
	ds, cleanup := setupTestDatastore(t)
	defer cleanup()

	ctx := t.Context()

	// Clean "Tyto alba" label with one detection, reviewed correct.
	cleanLabel, err := ds.label.GetOrCreate(ctx, "Tyto alba", ds.defaultModelID, ds.speciesLabelTypeID, ds.avesClassID)
	require.NoError(t, err)
	cleanDet := &entities.Detection{ModelID: ds.defaultModelID, LabelID: cleanLabel.ID, DetectedAt: time.Now().Unix(), Confidence: 0.9}
	require.NoError(t, ds.detection.Save(ctx, cleanDet))

	// Legacy concatenated label for the SAME species with one detection, reviewed
	// false positive. GetOrCreate persists the underscore form as a distinct label
	// (bypassing Save's ExtractScientificName normalization).
	legacyLabel, err := ds.label.GetOrCreate(ctx, "Tyto alba_Barn Owl", ds.defaultModelID, ds.speciesLabelTypeID, ds.avesClassID)
	require.NoError(t, err)
	legacyDet := &entities.Detection{ModelID: ds.defaultModelID, LabelID: legacyLabel.ID, DetectedAt: time.Now().Unix(), Confidence: 0.9}
	require.NoError(t, ds.detection.Save(ctx, legacyDet))

	require.NoError(t, ds.SaveNoteReview(&datastore.NoteReview{NoteID: cleanDet.ID, Verified: "correct"}))
	require.NoError(t, ds.SaveNoteReview(&datastore.NoteReview{NoteID: legacyDet.ID, Verified: "false_positive"}))

	stats, err := ds.GetSpeciesReviewStats(ctx)
	require.NoError(t, err)

	var tytoAlba []datastore.SpeciesReviewStat
	for _, s := range stats {
		if s.ScientificName == "Tyto alba" {
			tytoAlba = append(tytoAlba, s)
		}
	}
	require.Len(t, tytoAlba, 1, "clean and legacy labels for one species must merge into a single stat row")
	assert.Equal(t, 2, tytoAlba[0].Total, "both labels' detections must be summed")
	assert.Equal(t, 1, tytoAlba[0].Verified, "verified count must sum across labels")
	assert.Equal(t, 1, tytoAlba[0].Rejected, "rejected count must sum across labels")
}

// TestV2OnlyDatastore_GetSpeciesNoteIDs_PrefixSharingSpecies guards the LIKE
// escaping: the legacy "_" separator is a wildcard, so "Motacilla alba" must not
// also match the distinct species "Motacilla alba alba".
func TestV2OnlyDatastore_GetSpeciesNoteIDs_PrefixSharingSpecies(t *testing.T) {
	ds, cleanup := setupTestDatastore(t)
	defer cleanup()

	alba := seedDetection(t, ds, "Motacilla alba", time.Now())
	_ = seedDetection(t, ds, "Motacilla alba alba", time.Now())

	ids, err := ds.GetSpeciesNoteIDs(t.Context(), "Motacilla alba")
	require.NoError(t, err)
	assert.Equal(t, []string{fmt.Sprintf("%d", alba.ID)}, ids)
}
