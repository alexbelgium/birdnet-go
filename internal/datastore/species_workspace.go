// species_workspace.go: data access for the analytics species workspace.
//
// These methods are an optional capability: API handlers discover them by type
// assertion, so neither the Interface nor its generated mocks change. The v2
// normalized store implements the same method set in internal/datastore/v2only.
package datastore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tphakala/birdnet-go/internal/datastore/entities"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/logger"
	"gorm.io/gorm"
)

// Species workspace sort orders accepted by SpeciesWorkspaceRecordings.
const (
	SpeciesSortConfidenceDesc = "confidence_desc"
	SpeciesSortConfidenceAsc  = "confidence_asc"
	SpeciesSortDateDesc       = "date_desc"
	SpeciesSortDateAsc        = "date_asc"
)

// speciesWorkspaceNoteIndex backs best-recording and max-confidence lookups. It
// is partial (clip rows only) so the planner never picks it for existing
// searches, which do not carry the clip predicate.
const speciesWorkspaceNoteIndex = "idx_notes_sciname_confidence_clip"

// SpeciesWorkspaceRow is one species in the workspace inventory. Total counts
// every detection including false positives, which is the population a species
// delete acts on; Locked detections are the ones a delete keeps.
type SpeciesWorkspaceRow struct {
	ScientificName string
	CommonName     string
	SpeciesCode    string
	Total          int64
	Locked         int64
	FirstSeen      time.Time
	LastSeen       time.Time
}

// SpeciesWorkspaceStats holds review counts and the highest confidence among
// non-false-positive detections that have a recording.
type SpeciesWorkspaceStats struct {
	ScientificName string
	Correct        int64
	FalsePositive  int64
	MaxConfidence  *float64
}

// SpeciesRecordingCandidate is a detection that may provide the species' best
// recording. The API checks that the clip exists before choosing it.
type SpeciesRecordingCandidate struct {
	ID         uint
	Confidence float64
	ClipName   string
	Locked     bool
}

// SpeciesRecordingQuery selects one page of a species' recordings.
type SpeciesRecordingQuery struct {
	ScientificName string
	SortBy         string
	LockedOnly     bool
	Limit          int
	Offset         int
}

// SpeciesWorkspaceRecording is a detection plus the metadata the workspace shows
// that Note does not carry reliably on every store.
type SpeciesWorkspaceRecording struct {
	Note       Note
	DetectedAt time.Time
	ModelName  string // empty when the store does not record the model
}

// DeletedDetection is a detection removed by a species delete chunk.
type DeletedDetection struct {
	ID       uint
	ClipName string
}

// SpeciesDeleteChunk is the result of deleting one chunk of a species' unlocked
// detections. Locked and Reassigned count candidates that were kept because they
// were locked, or moved to another species, between selection and deletion.
// Remaining is the number of unlocked detections of the species still stored.
type SpeciesDeleteChunk struct {
	Deleted    []DeletedDetection
	Locked     int
	Reassigned int
	Remaining  int64
}

// SpeciesSortClause maps a workspace sort to an ORDER BY using the given column
// expressions (timeCols is a comma-separated list). Unknown values fall back to confidence descending.
func SpeciesSortClause(sortBy, confidenceCol, timeCols, idCol string) string {
	switch sortBy {
	case SpeciesSortConfidenceAsc:
		return fmt.Sprintf("%s ASC, %s ASC", confidenceCol, idCol)
	case SpeciesSortDateDesc:
		return fmt.Sprintf("%s DESC, %s DESC", strings.ReplaceAll(timeCols, ",", " DESC,"), idCol)
	case SpeciesSortDateAsc:
		return fmt.Sprintf("%s ASC, %s ASC", strings.ReplaceAll(timeCols, ",", " ASC,"), idCol)
	default:
		return fmt.Sprintf("%s DESC, %s DESC", confidenceCol, idCol)
	}
}

// ensureSpeciesWorkspaceIndex creates the partial index used by the species
// workspace on SQLite. MySQL has no partial indexes, so it is skipped there.
// Failure is logged and ignored: the workspace still works, only slower.
func ensureSpeciesWorkspaceIndex(db *gorm.DB, dbType string, log logger.Logger) {
	if !strings.EqualFold(dbType, DialectSQLite) {
		return
	}
	start := time.Now()
	err := db.Exec(fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON notes (scientific_name, confidence DESC, id) WHERE clip_name <> ''",
		speciesWorkspaceNoteIndex)).Error
	if err != nil {
		log.Warn("Failed to create species workspace index", logger.Error(err))
		return
	}
	log.Debug("Species workspace index ready", logger.Duration("duration", time.Since(start)))
}

// SpeciesWorkspaceInventory returns every stored species, or only scientificName
// when it is non-empty, read in one transaction so the columns are consistent.
func (ds *DataStore) SpeciesWorkspaceInventory(ctx context.Context, scientificName string) ([]SpeciesWorkspaceRow, error) {
	type countRow struct {
		ScientificName string
		CommonName     string
		SpeciesCode    string
		Total          int64
	}
	var rows []SpeciesWorkspaceRow
	err := ds.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var counts []countRow
		q := tx.Table("notes").Select("scientific_name, COUNT(*) AS total").Group("scientific_name")
		if scientificName != "" {
			q = q.Where("scientific_name = ?", scientificName)
		}
		if err := q.Scan(&counts).Error; err != nil {
			return err
		}
		type lockRow struct {
			ScientificName string
			Locked         int64
		}
		var locks []lockRow
		lq := tx.Table("note_locks k").Select("n.scientific_name, COUNT(*) AS locked").
			Joins("JOIN notes n ON n.id = k.note_id").Group("n.scientific_name")
		if scientificName != "" {
			lq = lq.Where("n.scientific_name = ?", scientificName)
		}
		if err := lq.Scan(&locks).Error; err != nil {
			return err
		}
		lockedBy := make(map[string]int64, len(locks))
		for _, l := range locks {
			lockedBy[l.ScientificName] = l.Locked
		}
		rows = make([]SpeciesWorkspaceRow, 0, len(counts))
		for _, c := range counts {
			row := SpeciesWorkspaceRow{ScientificName: c.ScientificName, Total: c.Total, Locked: lockedBy[c.ScientificName]}
			// Index seeks on (scientific_name, date): one row per end.
			var first, last Note
			if err := tx.Model(&Note{}).Select("date, time, common_name, species_code").
				Where("scientific_name = ?", c.ScientificName).Order("date ASC, time ASC").Limit(1).Scan(&first).Error; err != nil {
				return err
			}
			if err := tx.Model(&Note{}).Select("date, time, common_name, species_code").
				Where("scientific_name = ?", c.ScientificName).Order("date DESC, time DESC").Limit(1).Scan(&last).Error; err != nil {
				return err
			}
			row.CommonName, row.SpeciesCode = last.CommonName, last.SpeciesCode
			// Legacy rows store local wall-clock strings; interpret them in the process zone.
			row.FirstSeen, _ = time.ParseInLocation(time.DateTime, first.Date+" "+first.Time, time.Local)
			row.LastSeen, _ = time.ParseInLocation(time.DateTime, last.Date+" "+last.Time, time.Local)
			rows = append(rows, row)
		}
		return nil
	})
	if err != nil {
		return nil, dbError(err, "species_workspace_inventory", errors.PriorityMedium, "action", "load_species_inventory")
	}
	return rows, nil
}

// SpeciesWorkspaceStats returns review counts and max recording confidence per species.
func (ds *DataStore) SpeciesWorkspaceStats(ctx context.Context) ([]SpeciesWorkspaceStats, error) {
	type reviewRow struct {
		ScientificName string
		Verified       string
		N              int64
	}
	var reviews []reviewRow
	db := ds.DB.WithContext(ctx)
	if err := db.Table("note_reviews r").Select("n.scientific_name, r.verified, COUNT(*) AS n").
		Joins("JOIN notes n ON n.id = r.note_id").Group("n.scientific_name, r.verified").Scan(&reviews).Error; err != nil {
		return nil, dbError(err, "species_workspace_stats", errors.PriorityMedium, "action", "load_review_counts")
	}
	var names []string
	if err := db.Table("notes").Distinct("scientific_name").Pluck("scientific_name", &names).Error; err != nil {
		return nil, dbError(err, "species_workspace_stats", errors.PriorityMedium, "action", "load_species_names")
	}
	byName := make(map[string]*SpeciesWorkspaceStats, len(names))
	out := make([]SpeciesWorkspaceStats, len(names))
	for i, name := range names {
		out[i].ScientificName = name
		byName[name] = &out[i]
		best, err := ds.topNoteCandidates(db, name, 1)
		if err != nil {
			return nil, err
		}
		if len(best) > 0 {
			conf := best[0].Confidence
			out[i].MaxConfidence = &conf
		}
	}
	for _, r := range reviews {
		stat, ok := byName[r.ScientificName]
		if !ok {
			continue
		}
		switch r.Verified {
		case string(entities.VerificationCorrect):
			stat.Correct += r.N
		case string(entities.VerificationFalsePositive):
			stat.FalsePositive += r.N
		}
	}
	return out, nil
}

// SpeciesWorkspaceCandidates returns, per species, up to limit non-false-positive
// detections with a clip: locked detections first, then the highest confidence.
// Locked candidates of every requested species come from one query.
func (ds *DataStore) SpeciesWorkspaceCandidates(ctx context.Context, names []string, limit int) (map[string][]SpeciesRecordingCandidate, error) {
	db := ds.DB.WithContext(ctx)
	out := make(map[string][]SpeciesRecordingCandidate, len(names))
	if len(names) == 0 {
		return out, nil
	}
	type lockedRow struct {
		SpeciesRecordingCandidate
		ScientificName string
	}
	var locked []lockedRow
	// CROSS JOIN pins SQLite's join order to the small locks table.
	if err := db.Table("note_locks k").Select("n.id, n.scientific_name, n.confidence, n.clip_name, 1 AS locked").
		Joins("CROSS JOIN notes n ON n.id = k.note_id").
		Joins("LEFT JOIN note_reviews r ON r.note_id = n.id").
		Where("n.scientific_name IN ? AND n.clip_name <> ''", names).
		Where("r.verified IS NULL OR r.verified != ?", string(entities.VerificationFalsePositive)).
		Order("n.confidence DESC, n.id ASC").Scan(&locked).Error; err != nil {
		return nil, dbError(err, "species_workspace_candidates", errors.PriorityMedium, "action", "load_locked_candidates")
	}
	lockedBy := make(map[string][]SpeciesRecordingCandidate, len(names))
	for i := range locked {
		lockedBy[locked[i].ScientificName] = append(lockedBy[locked[i].ScientificName], locked[i].SpeciesRecordingCandidate)
	}
	for _, name := range names {
		top, err := ds.topNoteCandidates(db, name, limit)
		if err != nil {
			return nil, err
		}
		out[name] = MergeSpeciesCandidates(lockedBy[name], top, limit)
	}
	return out, nil
}

// topNoteCandidates walks the partial (scientific_name, confidence) index.
func (ds *DataStore) topNoteCandidates(db *gorm.DB, name string, limit int) ([]SpeciesRecordingCandidate, error) {
	var top []SpeciesRecordingCandidate
	err := db.Table("notes n").Select("n.id, n.confidence, n.clip_name, CASE WHEN k.note_id IS NULL THEN 0 ELSE 1 END AS locked").
		Joins("LEFT JOIN note_reviews r ON r.note_id = n.id").
		Joins("LEFT JOIN note_locks k ON k.note_id = n.id").
		Where("n.scientific_name = ? AND n.clip_name <> ''", name).
		Where("r.verified IS NULL OR r.verified != ?", string(entities.VerificationFalsePositive)).
		Order("n.confidence DESC, n.id ASC").Limit(limit).Scan(&top).Error
	if err != nil {
		return nil, dbError(err, "species_workspace_candidates", errors.PriorityMedium, "action", "load_top_candidates")
	}
	return top, nil
}

// MergeSpeciesCandidates puts locked candidates first, then the rest, without duplicates.
func MergeSpeciesCandidates(locked, top []SpeciesRecordingCandidate, limit int) []SpeciesRecordingCandidate {
	out := make([]SpeciesRecordingCandidate, 0, min(limit, len(locked)+len(top)))
	seen := make(map[uint]struct{}, len(locked)+len(top))
	for _, list := range [][]SpeciesRecordingCandidate{locked, top} {
		for _, c := range list {
			if _, dup := seen[c.ID]; dup || len(out) >= limit {
				continue
			}
			seen[c.ID] = struct{}{}
			out = append(out, c)
		}
	}
	return out
}

// SpeciesWorkspaceRecordings returns one page of a species' detections and the total.
func (ds *DataStore) SpeciesWorkspaceRecordings(ctx context.Context, q SpeciesRecordingQuery) ([]SpeciesWorkspaceRecording, int64, error) {
	db := ds.DB.WithContext(ctx)
	base := db.Table("notes n").Where("n.scientific_name = ?", q.ScientificName)
	if q.LockedOnly {
		base = base.Joins("JOIN note_locks k ON k.note_id = n.id")
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, dbError(err, "species_workspace_recordings", errors.PriorityMedium, "action", "count_recordings")
	}
	var ids []uint
	if err := base.Session(&gorm.Session{}).Select("n.id").
		Order(SpeciesSortClause(q.SortBy, "n.confidence", "n.date,n.time", "n.id")).
		Limit(q.Limit).Offset(q.Offset).Pluck("n.id", &ids).Error; err != nil {
		return nil, 0, dbError(err, "species_workspace_recordings", errors.PriorityMedium, "action", "page_recordings")
	}
	out := make([]SpeciesWorkspaceRecording, 0, len(ids))
	for _, id := range ids {
		note, err := ds.Get(strconv.FormatUint(uint64(id), 10))
		if err != nil {
			return nil, 0, err
		}
		at, _ := time.ParseInLocation(time.DateTime, note.Date+" "+note.Time, time.Local)
		out = append(out, SpeciesWorkspaceRecording{Note: note, DetectedAt: at})
	}
	return out, total, nil
}

// SpeciesWorkspaceDeleteChunk deletes up to limit unlocked detections of a
// species in one transaction. The delete statement itself re-checks species and
// lock, so a detection locked or corrected after selection is kept, never deleted.
func (ds *DataStore) SpeciesWorkspaceDeleteChunk(ctx context.Context, scientificName string, limit int) (SpeciesDeleteChunk, error) {
	var chunk SpeciesDeleteChunk
	err := RetryTransactionOnLock(ctx, ds.DB.WithContext(ctx), "species_workspace_delete", func(tx *gorm.DB) error {
		chunk = SpeciesDeleteChunk{}
		var picked []DeletedDetection
		if err := tx.Table("notes n").Select("n.id, n.clip_name").
			Where("n.scientific_name = ?", scientificName).
			Where("NOT EXISTS (SELECT 1 FROM note_locks k WHERE k.note_id = n.id)").
			Limit(limit).Scan(&picked).Error; err != nil {
			return err
		}
		if len(picked) > 0 {
			ids := make([]uint, len(picked))
			for i, p := range picked {
				ids[i] = p.ID
			}
			const deletable = "id IN ? AND scientific_name = ? AND NOT EXISTS (SELECT 1 FROM note_locks k WHERE k.note_id = notes.id)"
			if err := tx.Exec("DELETE FROM results WHERE note_id IN (SELECT id FROM notes WHERE "+deletable+")",
				ids, scientificName).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM notes WHERE "+deletable, ids, scientificName).Error; err != nil {
				return err
			}
			type keptRow struct {
				ID             uint
				ScientificName string
			}
			var kept []keptRow
			if err := tx.Table("notes").Select("id, scientific_name").Where("id IN ?", ids).Scan(&kept).Error; err != nil {
				return err
			}
			keptIDs := make(map[uint]bool, len(kept))
			for _, k := range kept {
				keptIDs[k.ID] = true
				if k.ScientificName == scientificName {
					chunk.Locked++
				} else {
					chunk.Reassigned++
				}
			}
			for _, p := range picked {
				if !keptIDs[p.ID] {
					chunk.Deleted = append(chunk.Deleted, p)
				}
			}
		}
		remaining, err := legacyUnlockedCount(tx, scientificName)
		chunk.Remaining = remaining
		return err
	}, nil)
	if err != nil {
		return SpeciesDeleteChunk{}, dbError(err, "species_workspace_delete", errors.PriorityMedium, "action", "delete_chunk")
	}
	return chunk, nil
}

// legacyUnlockedCount is total minus locked detections of a species, from two
// index-only counts (a NOT EXISTS scan over every row is far slower).
func legacyUnlockedCount(tx *gorm.DB, scientificName string) (int64, error) {
	var total, locked int64
	if err := tx.Table("notes").Where("scientific_name = ?", scientificName).Count(&total).Error; err != nil {
		return 0, err
	}
	if err := tx.Table("note_locks k").Joins("JOIN notes n ON n.id = k.note_id").
		Where("n.scientific_name = ?", scientificName).Count(&locked).Error; err != nil {
		return 0, err
	}
	return max(total-locked, 0), nil
}
