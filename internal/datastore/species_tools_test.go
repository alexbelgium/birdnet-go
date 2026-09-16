package datastore

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpeciesToolsProjectionAndRejectedInventory(t *testing.T) {
	ds := setupTestDB(t)
	require.NoError(t, ds.DB.Create(&[]Note{
		{ID: 1, ScientificName: "Turdus merula", Confidence: 0.8, Date: "2026-01-01", Time: "10:00:00"},
		{ID: 2, ScientificName: "Turdus merula", Confidence: 0.99, Date: "2026-01-02", Time: "11:00:00"},
		{ID: 3, ScientificName: "Tyto alba", Confidence: 0.95, Date: "2026-01-03", Time: "12:00:00"},
	}).Error)
	require.NoError(t, ds.DB.Create(&[]NoteReview{{NoteID: 2, Verified: "false_positive"}, {NoteID: 3, Verified: "false_positive"}}).Error)
	rows, err := ds.GetSpeciesTools(t.Context(), []string{"count", "max_confidence", "last_heard"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.EqualValues(t, 2, *rows[0].Count)
	assert.InDelta(t, 0.8, *rows[0].MaxConfidence, 0.0001)
	assert.Equal(t, "2026-01-02 11:00:00", *rows[0].LastHeard)
	assert.Nil(t, rows[1].MaxConfidence)
	// Identity and count must still work without the optional review relation.
	require.NoError(t, ds.DB.Migrator().DropTable(&NoteReview{}))
	rows, err = ds.GetSpeciesTools(t.Context(), []string{"count", "last_heard"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.EqualValues(t, 2, *rows[0].Count)
	assert.Nil(t, rows[0].MaxConfidence)
	rows, err = ds.GetSpeciesTools(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	data, err := json.Marshal(rows)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "max_confidence")
	assert.NotContains(t, string(data), "count")
	assert.NotContains(t, string(data), "last_heard")
	_, err = ds.GetSpeciesTools(t.Context(), []string{"count; DROP TABLE notes"})
	require.Error(t, err)
}

func TestSpeciesToolsRecordingOrder(t *testing.T) {
	ds := setupTestDB(t)
	require.NoError(t, ds.DB.AutoMigrate(&NoteLock{}))
	require.NoError(t, ds.DB.Create(&[]Note{
		{ID: 1, ScientificName: "Turdus merula", Confidence: 0.99, ClipName: "unlocked.wav"},
		{ID: 2, ScientificName: "Turdus merula", Confidence: 0.8, ClipName: "locked.wav"},
		{ID: 3, ScientificName: "Turdus merula", Confidence: 0.9, ClipName: "locked-higher.wav"},
		{ID: 4, ScientificName: "Turdus merula", Confidence: 1, ClipName: "rejected.wav"},
		{ID: 5, ScientificName: "Turdus merula", Confidence: 1},
		{ID: 6, ScientificName: "Tyto alba", Confidence: 1, ClipName: "other.wav"},
	}).Error)
	require.NoError(t, ds.DB.Create(&[]NoteLock{{NoteID: 2}, {NoteID: 3}, {NoteID: 4}}).Error)
	require.NoError(t, ds.DB.Create(&NoteReview{NoteID: 4, Verified: "false_positive"}).Error)
	rows, err := ds.GetSpeciesToolRecordings(t.Context(), []string{"Turdus merula"}, 0)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, uint(3), rows[0].ID)
	assert.Equal(t, uint(2), rows[1].ID)
	assert.Equal(t, uint(1), rows[2].ID)
	_, err = ds.GetSpeciesToolRecordings(t.Context(), []string{"Turdus merula"}, -1)
	require.Error(t, err)
}
