package v2only

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
)

// seedWorkspaceDetection stores a detection for label under the default model,
// or under modelID when it is non-zero.
func seedWorkspaceDetection(t *testing.T, ds *Datastore, label string, modelID uint, at time.Time, confidence float64, clip string) uint {
	t.Helper()
	if modelID == 0 {
		modelID = ds.defaultModelID
	}
	l, err := ds.label.GetOrCreate(t.Context(), label, modelID, ds.speciesLabelTypeID, ds.avesClassID)
	require.NoError(t, err)
	det := &entities.Detection{ModelID: modelID, LabelID: l.ID, DetectedAt: at.Unix(), Confidence: confidence}
	if clip != "" {
		det.ClipName = &clip
	}
	require.NoError(t, ds.detection.Save(t.Context(), det))
	return det.ID
}

func reviewWorkspaceDetection(t *testing.T, ds *Datastore, id uint, verdict entities.VerificationStatus) {
	t.Helper()
	require.NoError(t, ds.manager.DB().Create(&entities.DetectionReview{DetectionID: id, Verified: verdict}).Error)
}

func lockWorkspaceDetection(t *testing.T, ds *Datastore, id uint) {
	t.Helper()
	require.NoError(t, ds.detection.Lock(t.Context(), id))
}

func TestSpeciesWorkspace_V2(t *testing.T) {
	t.Parallel()
	ds, cleanup := setupTestDatastore(t)
	defer cleanup()
	ds.timezone = time.UTC
	ctx := t.Context()

	var perch entities.AIModel
	require.NoError(t, ds.manager.DB().Create(&entities.AIModel{Name: "Perch", Version: "2", Variant: "default", ModelType: entities.ModelTypeMulti}).Error)
	require.NoError(t, ds.manager.DB().Where("name = ?", "Perch").First(&perch).Error)

	day := func(d int) time.Time { return time.Date(2025, 5, d, 6, 0, 0, 0, time.UTC) }
	// Turdus merula: two models plus a legacy concatenated label.
	a := seedWorkspaceDetection(t, ds, "Turdus merula", 0, day(3), 0.70, "a.wav")
	b := seedWorkspaceDetection(t, ds, "Turdus merula", perch.ID, day(9), 0.95, "b.wav")
	c := seedWorkspaceDetection(t, ds, "Turdus merula_Blackbird", 0, day(1), 0.80, "c.wav")
	d := seedWorkspaceDetection(t, ds, "Turdus merula", 0, day(5), 0.99, "") // no clip
	reviewWorkspaceDetection(t, ds, b, entities.VerificationFalsePositive)
	reviewWorkspaceDetection(t, ds, a, entities.VerificationCorrect)
	lockWorkspaceDetection(t, ds, c)
	// Prefix-sharing species must never be merged with Motacilla alba.
	seedWorkspaceDetection(t, ds, "Motacilla alba", 0, day(2), 0.6, "m1.wav")
	seedWorkspaceDetection(t, ds, "Motacilla alba alba", 0, day(2), 0.9, "m2.wav")
	// Species whose only detection is a false positive stays in the inventory.
	fp := seedWorkspaceDetection(t, ds, "Strix aluco", 0, day(4), 0.5, "s.wav")
	reviewWorkspaceDetection(t, ds, fp, entities.VerificationFalsePositive)

	t.Run("inventory merges labels and counts false positives", func(t *testing.T) {
		rows, err := ds.SpeciesWorkspaceInventory(ctx, "")
		require.NoError(t, err)
		byName := map[string]datastore.SpeciesWorkspaceRow{}
		for _, r := range rows {
			byName[r.ScientificName] = r
		}
		require.Len(t, byName, 4)
		tm := byName["Turdus merula"]
		assert.Equal(t, int64(4), tm.Total)
		assert.Equal(t, int64(1), tm.Locked)
		assert.Equal(t, day(1), tm.FirstSeen)
		assert.Equal(t, day(9), tm.LastSeen)
		assert.Equal(t, int64(1), byName["Motacilla alba"].Total)
		assert.Equal(t, int64(1), byName["Strix aluco"].Total)

		one, err := ds.SpeciesWorkspaceInventory(ctx, "Turdus merula")
		require.NoError(t, err)
		require.Len(t, one, 1)
		assert.Equal(t, int64(4), one[0].Total)
	})

	t.Run("stats ignore false positives and clip-less detections for max confidence", func(t *testing.T) {
		stats, err := ds.SpeciesWorkspaceStats(ctx)
		require.NoError(t, err)
		byName := map[string]datastore.SpeciesWorkspaceStats{}
		for _, s := range stats {
			byName[s.ScientificName] = s
		}
		tm := byName["Turdus merula"]
		assert.Equal(t, int64(1), tm.Correct)
		assert.Equal(t, int64(1), tm.FalsePositive)
		require.NotNil(t, tm.MaxConfidence)
		assert.InDelta(t, 0.80, *tm.MaxConfidence, 1e-9) // 0.95 is FP, 0.99 has no clip
		assert.Nil(t, byName["Strix aluco"].MaxConfidence)
	})

	t.Run("candidates put locked first and skip false positives", func(t *testing.T) {
		got, err := ds.SpeciesWorkspaceCandidates(ctx, "Turdus merula", 5)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, c, got[0].ID)
		assert.True(t, got[0].Locked)
		assert.Equal(t, a, got[1].ID)
	})

	t.Run("recordings page across labels with sort and locked filter", func(t *testing.T) {
		recs, total, err := ds.SpeciesWorkspaceRecordings(ctx, datastore.SpeciesRecordingQuery{
			ScientificName: "Turdus merula", SortBy: datastore.SpeciesSortConfidenceDesc, Limit: 2,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(4), total)
		require.Len(t, recs, 2)
		assert.Equal(t, d, recs[0].Note.ID)
		assert.Equal(t, b, recs[1].Note.ID)
		assert.Equal(t, "Perch", recs[1].ModelName)
		assert.Equal(t, day(9), recs[1].DetectedAt)

		recs, total, err = ds.SpeciesWorkspaceRecordings(ctx, datastore.SpeciesRecordingQuery{
			ScientificName: "Turdus merula", SortBy: datastore.SpeciesSortDateAsc, LockedOnly: true, Limit: 10,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		require.Len(t, recs, 1)
		assert.Equal(t, c, recs[0].Note.ID)
	})

	t.Run("delete keeps locked and other species", func(t *testing.T) {
		ids, remaining, err := ds.SpeciesWorkspaceDeletable(ctx, "Turdus merula", 10)
		require.NoError(t, err)
		assert.Equal(t, int64(3), remaining)
		assert.ElementsMatch(t, []uint{a, b, d}, ids)

		outcome, _, err := ds.SpeciesWorkspaceDeleteDetection(ctx, "Turdus merula", c)
		require.NoError(t, err)
		assert.Equal(t, datastore.SpeciesDeleteLocked, outcome)

		outcome, _, err = ds.SpeciesWorkspaceDeleteDetection(ctx, "Motacilla alba", fp)
		require.NoError(t, err)
		assert.Equal(t, datastore.SpeciesDeleteReassigned, outcome)

		outcome, clip, err := ds.SpeciesWorkspaceDeleteDetection(ctx, "Turdus merula", a)
		require.NoError(t, err)
		assert.Equal(t, datastore.SpeciesDeleteDeleted, outcome)
		assert.Equal(t, "a.wav", clip)

		outcome, _, err = ds.SpeciesWorkspaceDeleteDetection(ctx, "Turdus merula", a)
		require.NoError(t, err)
		assert.Equal(t, datastore.SpeciesDeleteMissing, outcome)

		_, remaining, err = ds.SpeciesWorkspaceDeletable(ctx, "Turdus merula", 1)
		require.NoError(t, err)
		assert.Equal(t, int64(2), remaining)
	})
}
