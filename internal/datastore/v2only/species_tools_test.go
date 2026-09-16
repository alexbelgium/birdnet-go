package v2only

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
