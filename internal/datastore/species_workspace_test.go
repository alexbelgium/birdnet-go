package datastore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tphakala/birdnet-go/internal/logger"
)

func setupWorkspaceLegacyDB(t *testing.T) *DataStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Note{}, &Results{}, &NoteReview{}, &NoteLock{}, &NoteComment{}))
	ensureSpeciesWorkspaceIndex(db, DialectSQLite, logger.NewConsoleLogger("test", logger.LogLevelWarn))
	return &DataStore{DB: db}
}

func TestSpeciesWorkspace_Legacy(t *testing.T) {
	t.Parallel()
	ds := setupWorkspaceLegacyDB(t)
	ctx := t.Context()

	add := func(id uint, sci, date, clock string, conf float64, clip string) {
		t.Helper()
		require.NoError(t, ds.DB.Create(&Note{ID: id, ScientificName: sci, CommonName: sci + " common", Date: date, Time: clock, Confidence: conf, ClipName: clip}).Error)
	}
	add(1, "Turdus merula", "2025-05-03", "06:00:00", 0.70, "a.wav")
	add(2, "Turdus merula", "2025-05-09", "07:00:00", 0.95, "b.wav")
	add(3, "Turdus merula", "2025-05-01", "05:00:00", 0.80, "c.wav")
	add(4, "Turdus merula", "2025-05-05", "06:00:00", 0.99, "")
	add(5, "Strix aluco", "2025-05-04", "23:00:00", 0.50, "s.wav")
	require.NoError(t, ds.DB.Create(&NoteReview{NoteID: 2, Verified: "false_positive"}).Error)
	require.NoError(t, ds.DB.Create(&NoteReview{NoteID: 1, Verified: "correct"}).Error)
	require.NoError(t, ds.DB.Create(&NoteLock{NoteID: 3, LockedAt: time.Now()}).Error)
	require.NoError(t, ds.DB.Create(&Results{NoteID: 1, Species: "x"}).Error)

	t.Run("inventory", func(t *testing.T) {
		rows, err := ds.SpeciesWorkspaceInventory(ctx, "Turdus merula")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(4), rows[0].Total)
		assert.Equal(t, int64(1), rows[0].Locked)
		assert.Equal(t, "2025-05-01 05:00:00", rows[0].FirstSeen.Format(time.DateTime))
		assert.Equal(t, "2025-05-09 07:00:00", rows[0].LastSeen.Format(time.DateTime))
	})

	t.Run("stats and candidates", func(t *testing.T) {
		stats, err := ds.SpeciesWorkspaceStats(ctx, "")
		require.NoError(t, err)
		require.Len(t, stats, 2)
		one, err := ds.SpeciesWorkspaceStats(ctx, "Turdus merula")
		require.NoError(t, err)
		require.Len(t, one, 1)
		assert.Equal(t, int64(1), one[0].FalsePositive)
		none, err := ds.SpeciesWorkspaceStats(ctx, "Nobody")
		require.NoError(t, err)
		assert.Empty(t, none)
		for _, s := range stats {
			if s.ScientificName == "Turdus merula" {
				assert.Equal(t, int64(1), s.Correct)
				assert.Equal(t, int64(1), s.FalsePositive)
				require.NotNil(t, s.MaxConfidence)
				assert.InDelta(t, 0.80, *s.MaxConfidence, 1e-9)
			}
		}
		byName, err := ds.SpeciesWorkspaceCandidates(ctx, []string{"Turdus merula"}, 5)
		require.NoError(t, err)
		got := byName["Turdus merula"]
		require.Len(t, got, 2)
		assert.Equal(t, uint(3), got[0].ID)
		assert.True(t, got[0].Locked)
		assert.Equal(t, uint(1), got[1].ID)
	})

	t.Run("recordings", func(t *testing.T) {
		recs, total, err := ds.SpeciesWorkspaceRecordings(ctx, SpeciesRecordingQuery{ScientificName: "Turdus merula", SortBy: SpeciesSortDateDesc, Limit: 2, Offset: 1})
		require.NoError(t, err)
		assert.Equal(t, int64(4), total)
		require.Len(t, recs, 2)
		assert.Equal(t, uint(4), recs[0].Note.ID)
		assert.Equal(t, uint(1), recs[1].Note.ID)
		assert.Empty(t, recs[0].ModelName)
	})

	t.Run("delete chunk", func(t *testing.T) {
		chunk, err := ds.SpeciesWorkspaceDeleteChunk(ctx, "Turdus merula", 2)
		require.NoError(t, err)
		assert.Len(t, chunk.Deleted, 2)
		assert.Equal(t, int64(1), chunk.Remaining)
		chunk, err = ds.SpeciesWorkspaceDeleteChunk(ctx, "Turdus merula", 10)
		require.NoError(t, err)
		require.Len(t, chunk.Deleted, 1)
		assert.Equal(t, int64(0), chunk.Remaining)

		var notes, results int64
		require.NoError(t, ds.DB.Model(&Note{}).Where("scientific_name = ?", "Turdus merula").Count(&notes).Error)
		assert.Equal(t, int64(1), notes, "the locked detection is kept")
		require.NoError(t, ds.DB.Model(&Results{}).Where("note_id = ?", 1).Count(&results).Error)
		assert.Zero(t, results)
		require.NoError(t, ds.DB.Model(&Note{}).Where("scientific_name = ?", "Strix aluco").Count(&notes).Error)
		assert.Equal(t, int64(1), notes, "other species are untouched")
	})
}
