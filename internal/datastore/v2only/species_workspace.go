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
			FirstAt int64
			LastAt  int64
		}
		// MySQL answers counts and first/last in one grouped query (one round trip);
		// SQLite is faster with a covering count plus per-label MIN/MAX seeks.
		grouped := ds.manager.IsMySQL()
		var counts []countRow
		columns := "label_id, COUNT(*) AS total"
		if grouped {
			columns += ", MIN(detected_at) AS first_at, MAX(detected_at) AS last_at"
		}
		cq := tx.Table(prefix + "detections").Select(columns).Group("label_id")
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
		spanByLabel := make(map[uint][2]int64, len(counts))
		for _, c := range counts {
			totalByLabel[c.LabelID] = c.Total
			spanByLabel[c.LabelID] = [2]int64{c.FirstAt, c.LastAt}
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
				lo, hi := spanByLabel[id][0], spanByLabel[id][1]
				if !grouped {
					var err error
					if lo, hi, err = ds.labelSpan(tx, id); err != nil {
						return err
					}
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

// labelSpan returns the first and last detection time of a label using MIN/MAX
// seeks on idx_detection_label_date (faster on SQLite than grouping every row).
func (ds *Datastore) labelSpan(tx *gorm.DB, labelID uint) (first, last int64, err error) {
	table := ds.manager.TablePrefix() + "detections"
	if err = tx.Table(table).Where("label_id = ?", labelID).Select("MIN(detected_at)").Scan(&first).Error; err != nil {
		return 0, 0, err
	}
	err = tx.Table(table).Where("label_id = ?", labelID).Select("MAX(detected_at)").Scan(&last).Error
	return first, last, err
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
	if ds.manager.IsMySQL() {
		// Without the SQLite partial index, one grouped scan beats a sort per label.
		type maxRow struct {
			LabelID uint
			MaxConf *float64
		}
		var maxes []maxRow
		if err := db.Table(prefix+"detections d").Select("d.label_id, MAX(d.confidence) AS max_conf").
			Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix)).
			Where("d.clip_name IS NOT NULL AND d.clip_name <> ''").
			Where("r.verified IS NULL OR r.verified != ?", string(entities.VerificationFalsePositive)).
			Group("d.label_id").Scan(&maxes).Error; err != nil {
			return nil, wsError(err, "load_max_confidence")
		}
		for _, m := range maxes {
			name, ok := nameByLabel[m.LabelID]
			if !ok || m.MaxConf == nil {
				continue
			}
			if st := statFor(name); st.MaxConfidence == nil || *m.MaxConf > *st.MaxConfidence {
				conf := *m.MaxConf
				st.MaxConfidence = &conf
			}
		}
		return collectStats(stats), nil
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
	return collectStats(stats), nil
}

// collectStats flattens per-species stats, dropping unnamed labels.
func collectStats(stats map[string]*datastore.SpeciesWorkspaceStats) []datastore.SpeciesWorkspaceStats {
	out := make([]datastore.SpeciesWorkspaceStats, 0, len(stats))
	for _, s := range stats {
		if s.ScientificName != "" {
			out = append(out, *s)
		}
	}
	return out
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

// SpeciesWorkspaceCandidates returns, per species, up to limit best-recording
// candidates: locked ones first, then the highest confidence. Locked candidates
// of every requested species come from one query over the (small) locks table.
func (ds *Datastore) SpeciesWorkspaceCandidates(ctx context.Context, names []string, limit int) (map[string][]datastore.SpeciesRecordingCandidate, error) {
	prefix := ds.manager.TablePrefix()
	db := ds.manager.DB().WithContext(ctx)
	out := make(map[string][]datastore.SpeciesRecordingCandidate, len(names))
	if len(names) == 0 {
		return out, nil
	}
	labels, err := ds.labelsByScientificName(db)
	if err != nil {
		return nil, wsError(err, "resolve_labels")
	}
	nameByLabel := make(map[uint]string)
	var allIDs []uint
	for _, name := range names {
		for _, id := range labels[name] {
			nameByLabel[id] = name
			allIDs = append(allIDs, id)
		}
	}
	lockedBy := make(map[string][]datastore.SpeciesRecordingCandidate, len(names))
	if len(allIDs) > 0 {
		type lockedRow struct {
			datastore.SpeciesRecordingCandidate
			LabelID uint
		}
		var locked []lockedRow
		// CROSS JOIN pins SQLite's join order to the small locks table; otherwise the
		// planner walks every clip row of the species through the partial index.
		if err := db.Table(prefix+"detection_locks k").Select("d.id, d.label_id, d.confidence, d.clip_name, 1 AS locked").
			Joins(fmt.Sprintf("CROSS JOIN %sdetections d ON d.id = k.detection_id", prefix)).
			Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix)).
			Where("d.label_id IN ? AND d.clip_name IS NOT NULL AND d.clip_name <> ''", allIDs).
			Where("r.verified IS NULL OR r.verified != ?", string(entities.VerificationFalsePositive)).
			Order("d.confidence DESC, d.id ASC").Scan(&locked).Error; err != nil {
			return nil, wsError(err, "load_locked_candidates")
		}
		for i := range locked {
			name := nameByLabel[locked[i].LabelID]
			lockedBy[name] = append(lockedBy[name], locked[i].SpeciesRecordingCandidate)
		}
	}
	for _, name := range names {
		top, err := ds.topCandidates(db, labels[name], limit)
		if err != nil {
			return nil, wsError(err, "load_top_candidates")
		}
		out[name] = datastore.MergeSpeciesCandidates(lockedBy[name], top, limit)
	}
	return out, nil
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

// SpeciesWorkspaceDeleteChunk deletes up to limit unlocked detections of a
// species. The DELETE re-checks label and lock, so a detection locked or
// corrected after selection is kept. Reviews, locks, comments and predictions
// cascade through foreign keys, as in the repository's single-row delete.
func (ds *Datastore) SpeciesWorkspaceDeleteChunk(ctx context.Context, scientificName string, limit int) (datastore.SpeciesDeleteChunk, error) {
	prefix := ds.manager.TablePrefix()
	db := ds.manager.DB().WithContext(ctx)
	var chunk datastore.SpeciesDeleteChunk
	labelIDs, err := ds.speciesLabelIDs(db, scientificName)
	if err != nil {
		return chunk, wsError(err, "resolve_labels")
	}
	if len(labelIDs) == 0 {
		return chunk, nil
	}
	lockFree := fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %sdetection_locks k WHERE k.detection_id = d.id)", prefix)
	type pickedRow struct {
		ID       uint
		ClipName *string
	}
	var picked []pickedRow
	if err := db.Table(prefix+"detections d").Select("d.id, d.clip_name").
		Where("d.label_id IN ?", labelIDs).Where(lockFree).Limit(limit).Scan(&picked).Error; err != nil {
		return chunk, wsError(err, "select_chunk")
	}
	if len(picked) > 0 {
		ids := make([]uint, len(picked))
		for i, p := range picked {
			ids[i] = p.ID
		}
		err = datastore.RetryOnLock(ctx, "species_workspace_delete", func() error {
			return db.Exec(fmt.Sprintf(
				"DELETE FROM %sdetections WHERE id IN ? AND label_id IN ? AND NOT EXISTS (SELECT 1 FROM %sdetection_locks k WHERE k.detection_id = %sdetections.id)",
				prefix, prefix, prefix), ids, labelIDs).Error
		}, ds.metrics)
		if err != nil {
			return chunk, wsError(err, "delete_chunk")
		}
		type keptRow struct {
			ID      uint
			LabelID uint
		}
		var kept []keptRow
		if err := db.Table(prefix+"detections").Select("id, label_id").Where("id IN ?", ids).Scan(&kept).Error; err != nil {
			return chunk, wsError(err, "recheck_chunk")
		}
		keptIDs := make(map[uint]bool, len(kept))
		for _, k := range kept {
			keptIDs[k.ID] = true
			if slices.Contains(labelIDs, k.LabelID) {
				chunk.Locked++
			} else {
				chunk.Reassigned++
			}
		}
		for _, p := range picked {
			if keptIDs[p.ID] {
				continue
			}
			d := datastore.DeletedDetection{ID: p.ID}
			if p.ClipName != nil {
				d.ClipName = strings.TrimSpace(*p.ClipName)
			}
			chunk.Deleted = append(chunk.Deleted, d)
		}
	}
	// Remaining = total - locked, from two index-driven counts; a NOT EXISTS scan
	// over every detection of a large species takes most of a second.
	var total, locked int64
	if err := db.Table(prefix+"detections").Where("label_id IN ?", labelIDs).Count(&total).Error; err != nil {
		return chunk, wsError(err, "count_remaining")
	}
	if err := db.Table(prefix+"detection_locks k").
		Joins(fmt.Sprintf("JOIN %sdetections d ON d.id = k.detection_id", prefix)).
		Where("d.label_id IN ?", labelIDs).Count(&locked).Error; err != nil {
		return chunk, wsError(err, "count_remaining")
	}
	chunk.Remaining = max(total-locked, 0)
	return chunk, nil
}
