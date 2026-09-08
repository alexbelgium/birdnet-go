package reanalyze

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository"
)

// newLegacySchemaDB returns an in-memory SQLite database carrying the legacy v1
// tables the correction touches, so applyLegacyCorrection is exercised against a
// real schema and real SQL rather than a mock.
func newLegacySchemaDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&datastore.Note{}, &datastore.NoteLock{}, &datastore.NoteReview{}))
	return db
}

func seedNote(t *testing.T, db *gorm.DB) *datastore.Note {
	t.Helper()

	note := &datastore.Note{
		Date:              "2026-09-08",
		Time:              "07:14:00",
		ScientificName:    "Aegithalos caudatus",
		CommonName:        "Long-tailed Tit",
		SpeciesCode:       "lottit1",
		RawScientificName: "Aegithalos caudatus europaeus",
		Confidence:        0.99,
	}
	require.NoError(t, db.Create(note).Error)
	return note
}

func TestApplyLegacyCorrection_RewritesSpeciesAndRecordsTheReview(t *testing.T) {
	t.Parallel()

	db := newLegacySchemaDB(t)
	note := seedNote(t, db)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return applyLegacyCorrection(tx, note.ID, "Ficedula hypoleuca", "Pied Flycatcher", "piefly1", 0.87)
	}))

	var got datastore.Note
	require.NoError(t, db.First(&got, note.ID).Error)
	assert.Equal(t, "Ficedula hypoleuca", got.ScientificName)
	assert.Equal(t, "Pied Flycatcher", got.CommonName)
	// species_code must move with the species: a stale eBird code outlives the
	// correction into every consumer keyed on it.
	assert.Equal(t, "piefly1", got.SpeciesCode)
	assert.Empty(t, got.RawScientificName)
	assert.InDelta(t, 0.87, got.Confidence, 1e-9)

	var review datastore.NoteReview
	require.NoError(t, db.Where("note_id = ?", note.ID).First(&review).Error)
	assert.Equal(t, "correct", review.Verified)
}

func TestApplyLegacyCorrection_OverwritesAnExistingFalsePositiveReview(t *testing.T) {
	t.Parallel()

	db := newLegacySchemaDB(t)
	note := seedNote(t, db)
	require.NoError(t, db.Create(&datastore.NoteReview{NoteID: note.ID, Verified: "false_positive"}).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return applyLegacyCorrection(tx, note.ID, "Ficedula hypoleuca", "Pied Flycatcher", "piefly1", 0.87)
	}))

	// The upsert must replace the old verdict, not leave the detection both
	// corrected and still flagged as a false positive.
	var reviews []datastore.NoteReview
	require.NoError(t, db.Where("note_id = ?", note.ID).Find(&reviews).Error)
	require.Len(t, reviews, 1, "the upsert must not create a second review row")
	assert.Equal(t, "correct", reviews[0].Verified)
}

func TestApplyLegacyCorrection_RefusesALockedDetection(t *testing.T) {
	t.Parallel()

	db := newLegacySchemaDB(t)
	note := seedNote(t, db)
	require.NoError(t, db.Create(&datastore.NoteLock{NoteID: note.ID}).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return applyLegacyCorrection(tx, note.ID, "Ficedula hypoleuca", "Pied Flycatcher", "piefly1", 0.87)
	})
	require.ErrorIs(t, err, errDetectionLocked)

	// The whole thing must roll back: no species change and no review.
	var got datastore.Note
	require.NoError(t, db.First(&got, note.ID).Error)
	assert.Equal(t, "Aegithalos caudatus", got.ScientificName)
	var reviewCount int64
	require.NoError(t, db.Model(&datastore.NoteReview{}).Where("note_id = ?", note.ID).Count(&reviewCount).Error)
	assert.Zero(t, reviewCount)
}

func TestApplyLegacyCorrection_ReportsAMissingDetection(t *testing.T) {
	t.Parallel()

	db := newLegacySchemaDB(t)

	err := db.Transaction(func(tx *gorm.DB) error {
		return applyLegacyCorrection(tx, 4242, "Ficedula hypoleuca", "Pied Flycatcher", "piefly1", 0.87)
	})
	// A missing detection is a 404, not the 409 a locked one gets.
	require.ErrorIs(t, err, repository.ErrDetectionNotFound)
}

func TestApplyLegacyCorrection_ConfirmingTheExistingSpeciesStillRecordsTheReview(t *testing.T) {
	t.Parallel()

	// Confirming the species a detection already has is a normal action: the
	// original species is usually the top row of the reanalysis grid.
	//
	// NOTE: on SQLite this goes through the ordinary RowsAffected == 1 path,
	// because SQLite counts MATCHED rows. It does not reach the zero-row
	// disambiguation — that is MySQL-only behaviour, covered directly by
	// TestClassifyZeroRowUpdate_* below. This test exists to prove the outcome
	// (the review is recorded) rather than the mechanism.
	db := newLegacySchemaDB(t)
	note := seedNote(t, db)
	require.NoError(t, db.Model(&datastore.Note{}).Where("id = ?", note.ID).
		Updates(map[string]any{"raw_scientific_name": ""}).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return applyLegacyCorrection(tx, note.ID, "Aegithalos caudatus", "Long-tailed Tit", "lottit1", 0.99)
	})
	require.NoError(t, err)

	var review datastore.NoteReview
	require.NoError(t, db.Where("note_id = ?", note.ID).First(&review).Error)
	assert.Equal(t, "correct", review.Verified)
}

func TestClassifyZeroRowUpdate_DistinguishesAllThreeCauses(t *testing.T) {
	t.Parallel()

	// MySQL reports zero affected rows both for "the guard excluded this row" and
	// for "the row matched and nothing changed". Collapsing the two tells an
	// operator confirming an existing species to go unlock a detection that was
	// never locked. SQLite cannot reproduce the third case at all (it counts
	// matched rows), so this drives the classifier directly.
	db := newLegacySchemaDB(t)

	unlocked := seedNote(t, db)
	locked := seedNote(t, db)
	require.NoError(t, db.Create(&datastore.NoteLock{NoteID: locked.ID}).Error)

	// Present and unlocked => the update was a no-op; carry on and save the review.
	require.NoError(t, classifyZeroRowUpdate(db, unlocked.ID))

	// Present but locked => 409.
	require.ErrorIs(t, classifyZeroRowUpdate(db, locked.ID), errDetectionLocked)

	// Absent => 404, not 409.
	require.ErrorIs(t, classifyZeroRowUpdate(db, 987654), repository.ErrDetectionNotFound)
}

func TestApplyLegacyCorrection_LeavesOtherDetectionsAlone(t *testing.T) {
	t.Parallel()

	db := newLegacySchemaDB(t)
	target := seedNote(t, db)
	bystander := seedNote(t, db)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return applyLegacyCorrection(tx, target.ID, "Ficedula hypoleuca", "Pied Flycatcher", "piefly1", 0.87)
	}))

	var got datastore.Note
	require.NoError(t, db.First(&got, bystander.ID).Error)
	assert.Equal(t, "Aegithalos caudatus", got.ScientificName, "the update must be scoped to one id")
	var reviewCount int64
	require.NoError(t, db.Model(&datastore.NoteReview{}).Where("note_id = ?", bystander.ID).Count(&reviewCount).Error)
	assert.Zero(t, reviewCount)
}
