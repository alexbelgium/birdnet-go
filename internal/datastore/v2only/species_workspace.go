// species_workspace.go: v2 implementation of the species workspace capability
// (see internal/datastore/species_workspace.go for the contract).
package v2only

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/errors"
	"gorm.io/gorm"
)

// speciesLabel is one label row of a species; a species has one label per model
// and possibly a legacy "Scientific_Common" label.
type speciesLabel struct {
	ID             uint
	ScientificName string
}

// wsError wraps a workspace query failure with datastore telemetry context.
func wsError(err error, action string) error {
	return errors.New(err).Component("datastore").Category(errors.CategoryDatabase).
		Context("operation", "species_workspace").Context("action", action).Build()
}

// speciesLabelIDs returns every label ID of a species: exact name, or a legacy
// concatenated label whose scientific part equals the name (see
// scientificNameLikeEscaper for why '!' is the escape character).
func (ds *Datastore) speciesLabelIDs(db *gorm.DB, scientificName string) ([]uint, error) {
	var ids []uint
	err := db.Table(ds.manager.TablePrefix()+"labels").
		Where("scientific_name = ? OR scientific_name LIKE ? ESCAPE '!'",
			scientificName, scientificNameLikeEscaper.Replace(scientificName)+`!_%`).
		Pluck("id", &ids).Error
	return ids, err
}

// labelsByScientificName groups every label by its normalized scientific name.
func (ds *Datastore) labelsByScientificName(db *gorm.DB) (map[string][]uint, error) {
	var labels []speciesLabel
	if err := db.Table(ds.manager.TablePrefix() + "labels").Select("id, scientific_name").Scan(&labels).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]uint, len(labels))
	for _, l := range labels {
		name := detection.ExtractScientificName(l.ScientificName)
		out[name] = append(out[name], l.ID)
	}
	return out, nil
}

// SpeciesWorkspaceInventory returns every species with detections (or only
// scientificName), read in one transaction.
func (ds *Datastore) SpeciesWorkspaceInventory(ctx context.Context, scientificName string) ([]datastore.SpeciesWorkspaceRow, error) {
	prefix := ds.manager.TablePrefix()
	var rows []datastore.SpeciesWorkspaceRow
	err := ds.manager.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		labels, err := ds.labelsByScientificName(tx)
		if err != nil {
			return err
		}
		type countRow struct {
			LabelID uint
			Total   int64
		}
		var counts []countRow
		cq := tx.Table(prefix + "detections").Select("label_id, COUNT(*) AS total").Group("label_id")
		if scientificName != "" {
			cq = cq.Where("label_id IN ?", append(labels[scientificName], 0))
		}
		if err := cq.Scan(&counts).Error; err != nil {
			return err
		}
		var locks []countRow
		lq := tx.Table(prefix + "detection_locks k").Select("d.label_id, COUNT(*) AS total").
			Joins(fmt.Sprintf("JOIN %sdetections d ON d.id = k.detection_id", prefix)).Group("d.label_id")
		if scientificName != "" {
			lq = lq.Where("d.label_id IN ?", append(labels[scientificName], 0))
		}
		if err := lq.Scan(&locks).Error; err != nil {
			return err
		}
		totalByLabel := make(map[uint]int64, len(counts))
		for _, c := range counts {
			totalByLabel[c.LabelID] = c.Total
		}
		lockedByLabel := make(map[uint]int64, len(locks))
		for _, l := range locks {
			lockedByLabel[l.LabelID] = l.Total
		}
		for name, ids := range labels {
			if scientificName != "" && name != scientificName {
				continue
			}
			row := datastore.SpeciesWorkspaceRow{ScientificName: name}
			var first, last int64
			for _, id := range ids {
				if totalByLabel[id] == 0 {
					continue
				}
				row.Total += totalByLabel[id]
				row.Locked += lockedByLabel[id]
				// MIN/MAX seeks on idx_detection_label_date.
				var lo, hi int64
				if err := tx.Table(prefix+"detections").Where("label_id = ?", id).Select("MIN(detected_at)").Scan(&lo).Error; err != nil {
					return err
				}
				if err := tx.Table(prefix+"detections").Where("label_id = ?", id).Select("MAX(detected_at)").Scan(&hi).Error; err != nil {
					return err
				}
				if first == 0 || lo < first {
					first = lo
				}
				last = max(last, hi)
			}
			if row.Total == 0 {
				continue
			}
			row.CommonName = ds.resolveCommonName(name)
			row.SpeciesCode = ds.speciesCodeMap[name]
			row.FirstSeen = time.Unix(first, 0).In(ds.timezone)
			row.LastSeen = time.Unix(last, 0).In(ds.timezone)
			rows = append(rows, row)
		}
		return nil
	})
	if err != nil {
		return nil, wsError(err, "load_species_inventory")
	}
	return rows, nil
}

// SpeciesWorkspaceStats returns review counts and max recording confidence per species.
func (ds *Datastore) SpeciesWorkspaceStats(ctx context.Context) ([]datastore.SpeciesWorkspaceStats, error) {
	prefix := ds.manager.TablePrefix()
	db := ds.manager.DB().WithContext(ctx)
	labels, err := ds.labelsByScientificName(db)
	if err != nil {
		return nil, wsError(err, "load_labels")
	}
	type reviewRow struct {
		LabelID  uint
		Verified string
		N        int64
	}
	var reviews []reviewRow
	if err := db.Table(prefix + "detection_reviews r").Select("d.label_id, r.verified, COUNT(*) AS n").
		Joins(fmt.Sprintf("JOIN %sdetections d ON d.id = r.detection_id", prefix)).
		Group("d.label_id, r.verified").Scan(&reviews).Error; err != nil {
		return nil, wsError(err, "load_review_counts")
	}
	nameByLabel := make(map[uint]string)
	for name, ids := range labels {
		for _, id := range ids {
			nameByLabel[id] = name
		}
	}
	stats := make(map[string]*datastore.SpeciesWorkspaceStats, len(labels))
	statFor := func(name string) *datastore.SpeciesWorkspaceStats {
		s, ok := stats[name]
		if !ok {
			s = &datastore.SpeciesWorkspaceStats{ScientificName: name}
			stats[name] = s
		}
		return s
	}
	for _, r := range reviews {
		s := statFor(nameByLabel[r.LabelID])
		switch r.Verified {
		case string(entities.VerificationCorrect):
			s.Correct += r.N
		case string(entities.VerificationFalsePositive):
			s.FalsePositive += r.N
		}
	}
	for name, ids := range labels {
		best, err := ds.topCandidates(db, ids, 1)
		if err != nil {
			return nil, wsError(err, "load_max_confidence")
		}
		if len(best) > 0 {
			conf := best[0].Confidence
			statFor(name).MaxConfidence = &conf
		}
	}
	out := make([]datastore.SpeciesWorkspaceStats, 0, len(stats))
	for _, s := range stats {
		if s.ScientificName != "" {
			out = append(out, *s)
		}
	}
	return out, nil
}

// topCandidates returns the highest-confidence non-false-positive detections with
// a clip across labels. Each label is queried on its own so SQLite walks the
// partial index instead of sorting an IN-list range.
func (ds *Datastore) topCandidates(db *gorm.DB, labelIDs []uint, limit int) ([]datastore.SpeciesRecordingCandidate, error) {
	prefix := ds.manager.TablePrefix()
	var all []datastore.SpeciesRecordingCandidate
	for _, id := range labelIDs {
		var rows []datastore.SpeciesRecordingCandidate
		err := db.Table(prefix+"detections d").
			Select("d.id, d.confidence, d.clip_name, CASE WHEN k.detection_id IS NULL THEN 0 ELSE 1 END AS locked").
			Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix)).
			Joins(fmt.Sprintf("LEFT JOIN %sdetection_locks k ON k.detection_id = d.id", prefix)).
			Where("d.label_id = ? AND d.clip_name IS NOT NULL AND d.clip_name <> ''", id).
			Where("r.verified IS NULL OR r.verified != ?", string(entities.VerificationFalsePositive)).
			Order("d.confidence DESC, d.id ASC").Limit(limit).Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
	}
	slices.SortStableFunc(all, func(a, b datastore.SpeciesRecordingCandidate) int {
		if c := cmp.Compare(b.Confidence, a.Confidence); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// SpeciesWorkspaceCandidates returns up to limit best-recording candidates:
// locked ones first, then the highest confidence.
func (ds *Datastore) SpeciesWorkspaceCandidates(ctx context.Context, scientificName string, limit int) ([]datastore.SpeciesRecordingCandidate, error) {
	prefix := ds.manager.TablePrefix()
	db := ds.manager.DB().WithContext(ctx)
	ids, err := ds.speciesLabelIDs(db, scientificName)
	if err != nil {
		return nil, wsError(err, "resolve_labels")
	}
	if len(ids) == 0 {
		return []datastore.SpeciesRecordingCandidate{}, nil
	}
	var locked []datastore.SpeciesRecordingCandidate
	if err := db.Table(prefix+"detection_locks k").Select("d.id, d.confidence, d.clip_name, 1 AS locked").
		Joins(fmt.Sprintf("JOIN %sdetections d ON d.id = k.detection_id", prefix)).
		Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix)).
		Where("d.label_id IN ? AND d.clip_name IS NOT NULL AND d.clip_name <> ''", ids).
		Where("r.verified IS NULL OR r.verified != ?", string(entities.VerificationFalsePositive)).
		Order("d.confidence DESC, d.id ASC").Limit(limit).Scan(&locked).Error; err != nil {
		return nil, wsError(err, "load_locked_candidates")
	}
	top, err := ds.topCandidates(db, ids, limit)
	if err != nil {
		return nil, wsError(err, "load_top_candidates")
	}
	return datastore.MergeSpeciesCandidates(locked, top, limit), nil
}

// SpeciesWorkspaceRecordings returns one page of a species' detections and the total.
func (ds *Datastore) SpeciesWorkspaceRecordings(ctx context.Context, q datastore.SpeciesRecordingQuery) ([]datastore.SpeciesWorkspaceRecording, int64, error) {
	prefix := ds.manager.TablePrefix()
	db := ds.manager.DB().WithContext(ctx)
	ids, err := ds.speciesLabelIDs(db, q.ScientificName)
	if err != nil {
		return nil, 0, wsError(err, "resolve_labels")
	}
	if len(ids) == 0 {
		return []datastore.SpeciesWorkspaceRecording{}, 0, nil
	}
	base := db.Table(prefix+"detections d").Where("d.label_id IN ?", ids)
	if q.LockedOnly {
		base = base.Joins(fmt.Sprintf("JOIN %sdetection_locks k ON k.detection_id = d.id", prefix))
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, wsError(err, "count_recordings")
	}
	var dets []*entities.Detection
	if err := base.Session(&gorm.Session{}).Select("d.*").
		Order(datastore.SpeciesSortClause(q.SortBy, "d.confidence", "d.detected_at", "d.id")).
		Limit(q.Limit).Offset(q.Offset).Find(&dets).Error; err != nil {
		return nil, 0, wsError(err, "page_recordings")
	}
	if err := ds.loadDetectionRelations(ctx, dets); err != nil {
		return nil, 0, wsError(err, "load_relations")
	}
	out := make([]datastore.SpeciesWorkspaceRecording, 0, len(dets))
	for _, det := range dets {
		rec := datastore.SpeciesWorkspaceRecording{
			Note:       ds.detectionToNote(det),
			DetectedAt: time.Unix(det.DetectedAt, 0).In(ds.timezone),
		}
		if det.Model != nil {
			rec.ModelName = det.Model.Name
		}
		out = append(out, rec)
	}
	return out, total, nil
}

// SpeciesWorkspaceDeletable returns up to limit unlocked detection IDs of the
// species (lowest first) and how many unlocked detections remain in total.
func (ds *Datastore) SpeciesWorkspaceDeletable(ctx context.Context, scientificName string, limit int) (ids []uint, remaining int64, err error) {
	prefix := ds.manager.TablePrefix()
	db := ds.manager.DB().WithContext(ctx)
	labelIDs, err := ds.speciesLabelIDs(db, scientificName)
	if err != nil {
		return nil, 0, wsError(err, "resolve_labels")
	}
	if len(labelIDs) == 0 {
		return []uint{}, 0, nil
	}
	base := db.Table(prefix+"detections d").Where("d.label_id IN ?", labelIDs).
		Where(fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %sdetection_locks k WHERE k.detection_id = d.id)", prefix))
	if err := base.Session(&gorm.Session{}).Count(&remaining).Error; err != nil {
		return nil, 0, wsError(err, "count_deletable")
	}
	if err := base.Session(&gorm.Session{}).Order("d.id ASC").Limit(limit).Pluck("d.id", &ids).Error; err != nil {
		return nil, 0, wsError(err, "list_deletable")
	}
	return ids, remaining, nil
}

// SpeciesWorkspaceDeleteDetection deletes one detection only if it still belongs
// to the species and is not locked; the check and delete are one statement.
// Reviews, locks, comments and predictions cascade through foreign keys.
func (ds *Datastore) SpeciesWorkspaceDeleteDetection(ctx context.Context, scientificName string, id uint) (outcome datastore.SpeciesDeleteOutcome, clipName string, err error) {
	prefix := ds.manager.TablePrefix()
	db := ds.manager.DB().WithContext(ctx)
	labelIDs, err := ds.speciesLabelIDs(db, scientificName)
	if err != nil {
		return 0, "", wsError(err, "resolve_labels")
	}
	var row struct {
		LabelID  uint
		ClipName *string
	}
	res := db.Table(prefix+"detections").Select("label_id, clip_name").Where("id = ?", id).Limit(1).Scan(&row)
	if res.Error != nil {
		return 0, "", wsError(res.Error, "load_detection")
	}
	if res.RowsAffected == 0 {
		return datastore.SpeciesDeleteMissing, "", nil
	}
	if len(labelIDs) == 0 || !slices.Contains(labelIDs, row.LabelID) {
		return datastore.SpeciesDeleteReassigned, "", nil
	}
	var affected int64
	err = datastore.RetryOnLock(ctx, "species_workspace_delete", func() error {
		del := db.Exec(fmt.Sprintf(
			"DELETE FROM %sdetections WHERE id = ? AND label_id IN ? AND NOT EXISTS (SELECT 1 FROM %sdetection_locks WHERE detection_id = ?)",
			prefix, prefix), id, labelIDs, id)
		affected = del.RowsAffected
		return del.Error
	}, ds.metrics)
	if err != nil {
		return 0, "", wsError(err, "delete_detection")
	}
	if affected == 0 {
		// The row existed a moment ago: it was locked, relabelled or deleted since.
		var still struct{ LabelID uint }
		if r := db.Table(prefix+"detections").Select("label_id").Where("id = ?", id).Limit(1).Scan(&still); r.Error != nil {
			return 0, "", wsError(r.Error, "recheck_detection")
		} else if r.RowsAffected == 0 {
			return datastore.SpeciesDeleteMissing, "", nil
		}
		if !slices.Contains(labelIDs, still.LabelID) {
			return datastore.SpeciesDeleteReassigned, "", nil
		}
		return datastore.SpeciesDeleteLocked, "", nil
	}
	if row.ClipName != nil {
		clipName = strings.TrimSpace(*row.ClipName)
	}
	return datastore.SpeciesDeleteDeleted, clipName, nil
}
