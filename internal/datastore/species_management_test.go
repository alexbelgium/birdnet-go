// species_management_test.go: Tests for species-management datastore helpers
// (GetSpeciesReviewStats and GetSpeciesNoteIDs) used by the species deletion UI.
package datastore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/datastore/entities"
)

// TestGetSpeciesReviewStats verifies per-species totals and confirmed/rejected counts,
// including the critical case where every detection of a species is a false positive
// (such species must still be reported as deletion candidates).
func TestGetSpeciesReviewStats(t *testing.T) {
	t.Parallel()

	ds := setupTestDB(t)
	seedTestData(t, ds) // 2x American Robin, 2x Blue Jay, 1x Northern Cardinal

	// Robin: one confirmed, one rejected. Blue Jay: both rejected (pure mislabel).
	// Cardinal: no manual review at all.
	reviews := []NoteReview{
		{NoteID: 1, Verified: string(entities.VerificationCorrect)},
		{NoteID: 2, Verified: string(entities.VerificationFalsePositive)},
		{NoteID: 3, Verified: string(entities.VerificationFalsePositive)},
		{NoteID: 4, Verified: string(entities.VerificationFalsePositive)},
	}
	for i := range reviews {
		require.NoError(t, ds.DB.Create(&reviews[i]).Error)
	}

	stats, err := ds.GetSpeciesReviewStats(t.Context())
	require.NoError(t, err)
	require.Len(t, stats, 3)

	byName := make(map[string]SpeciesReviewStats, len(stats))
	for _, s := range stats {
		byName[s.ScientificName] = s
	}

	robin := byName["Turdus migratorius"]
	assert.Equal(t, "American Robin", robin.CommonName)
	assert.Equal(t, 2, robin.Total)
	assert.Equal(t, 1, robin.Verified)
	assert.Equal(t, 1, robin.Rejected)

	// Fully-rejected species must still be present in the results.
	jay := byName["Cyanocitta cristata"]
	require.Contains(t, byName, "Cyanocitta cristata")
	assert.Equal(t, 2, jay.Total)
	assert.Equal(t, 0, jay.Verified)
	assert.Equal(t, 2, jay.Rejected)

	// Unreviewed species: total counted, no confirmed/rejected.
	cardinal := byName["Cardinalis cardinalis"] //nolint:misspell // correct scientific name
	assert.Equal(t, 1, cardinal.Total)
	assert.Equal(t, 0, cardinal.Verified)
	assert.Equal(t, 0, cardinal.Rejected)
}

// TestGetSpeciesNoteIDs verifies exact-species ID lookup used to drive bulk deletion.
func TestGetSpeciesNoteIDs(t *testing.T) {
	t.Parallel()

	ds := setupTestDB(t)
	seedTestData(t, ds)

	ids, err := ds.GetSpeciesNoteIDs("Turdus migratorius")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"1", "2"}, ids)

	// A species with no detections returns an empty slice and no error.
	none, err := ds.GetSpeciesNoteIDs("Nonexistent species")
	require.NoError(t, err)
	assert.Empty(t, none)
}
