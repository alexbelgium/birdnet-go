package v2only

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/detection"
)

// likeEscapeScientificName escapes LIKE metacharacters with '!' (the
// dialect-portable escape char used across the codebase).
func likeEscapeScientificName(name string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(name)
}

// GetSpeciesTools projects only the enabled metrics, without filtering rejected species out of the inventory.
func (ds *Datastore) GetSpeciesTools(ctx context.Context, fields []string) ([]datastore.SpeciesToolRow, error) {
	projection, err := datastore.SelectSpeciesToolFields(fields, "COUNT(*)", "MAX(CASE WHEN r.verified IS NULL OR r.verified != 'false_positive' THEN d.confidence END)", "MAX(d.detected_at) AS last_timestamp")
	if err != nil {
		return nil, err
	}
	prefix := ds.manager.TablePrefix()
	query := ds.manager.DB().WithContext(ctx).Table(prefix + "detections d").Joins(fmt.Sprintf("JOIN %slabels l ON l.id = d.label_id", prefix))
	if slices.Contains(fields, "max_confidence") {
		query = query.Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix))
	}
	rows := make([]datastore.SpeciesToolRow, 0)
	err = query.Select(strings.Join(append([]string{"l.scientific_name"}, projection...), ", ")).Group("l.scientific_name").Order("l.scientific_name").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	merged := make([]datastore.SpeciesToolRow, 0, len(rows))
	positions := make(map[string]int, len(rows))
	for _, row := range rows {
		row.ScientificName = detection.ExtractScientificName(row.ScientificName)
		row.CommonName = ds.resolveCommonName(row.ScientificName)
		row.SpeciesCode = ds.speciesCodeMap[row.ScientificName]
		if i, ok := positions[row.ScientificName]; ok {
			old := &merged[i]
			if row.Count != nil && old.Count != nil {
				*old.Count += *row.Count
			}
			if row.MaxConfidence != nil && (old.MaxConfidence == nil || *row.MaxConfidence > *old.MaxConfidence) {
				old.MaxConfidence = row.MaxConfidence
			}
			if row.LastTimestamp != nil && (old.LastTimestamp == nil || *row.LastTimestamp > *old.LastTimestamp) {
				old.LastTimestamp = row.LastTimestamp
			}
		} else {
			positions[row.ScientificName] = len(merged)
			merged = append(merged, row)
		}
	}
	for i := range merged {
		if merged[i].LastTimestamp != nil {
			value := time.Unix(*merged[i].LastTimestamp, 0).In(ds.timezone).Format(time.RFC3339)
			merged[i].LastHeard = &value
		}
	}
	return merged, nil
}

// GetSpeciesToolRecordings retrieves primary predictions only, with locked recordings first.
func (ds *Datastore) GetSpeciesToolRecordings(ctx context.Context, names []string, offset int) ([]datastore.SpeciesRecordingCandidate, error) {
	prefix := ds.manager.TablePrefix()
	query := ds.manager.DB().Table(prefix+"detections d").
		Select("d.id, l.scientific_name, d.confidence, d.clip_name, CASE WHEN k.detection_id IS NULL THEN 0 ELSE 1 END AS locked").
		Joins(fmt.Sprintf("JOIN %slabels l ON l.id = d.label_id", prefix)).
		Joins(fmt.Sprintf("LEFT JOIN %sdetection_locks k ON k.detection_id = d.id", prefix)).
		Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix)).
		Where("d.clip_name IS NOT NULL AND d.clip_name != ''").Where("r.verified IS NULL OR r.verified != ?", "false_positive")
	// Match both current labels and historical Scientific_Common labels without
	// interpreting user-provided wildcard characters as SQL patterns.
	clauses := make([]string, 0, len(names))
	args := make([]any, 0, 2*len(names))
	for _, name := range names {
		clauses = append(clauses, "l.scientific_name = ? OR l.scientific_name LIKE ? ESCAPE '!'")
		args = append(args, name, likeEscapeScientificName(name)+"!_%")
	}
	if len(clauses) == 0 {
		return []datastore.SpeciesRecordingCandidate{}, nil
	}
	rows, err := datastore.SpeciesToolsCandidates(ctx, query.Where("("+strings.Join(clauses, ") OR (")+")", args...), offset)
	for i := range rows {
		rows[i].ScientificName = detection.ExtractScientificName(rows[i].ScientificName)
	}
	return rows, err
}

// GetSpeciesReviewStats retrieves per-species detection and review counts across
// all time, including false positives.
func (ds *Datastore) GetSpeciesReviewStats(ctx context.Context) ([]datastore.SpeciesReviewStat, error) {
	type labelReviewStat struct {
		ScientificName string
		Total          int64
		Verified       int64
		Rejected       int64
	}
	var v2Data []labelReviewStat
	prefix := ds.manager.TablePrefix()
	// detection_reviews.detection_id is unique, so the LEFT JOIN is 1:1 and plain
	// COUNT cannot fan out.
	err := ds.manager.DB().WithContext(ctx).Table(prefix+"detections d").
		Select("l.scientific_name, COUNT(d.id) AS total, "+
			"COUNT(CASE WHEN r.verified = ? THEN d.id END) AS verified, "+
			"COUNT(CASE WHEN r.verified = ? THEN d.id END) AS rejected",
			string(entities.VerificationCorrect), string(entities.VerificationFalsePositive)).
		Joins(fmt.Sprintf("JOIN %slabels l ON l.id = d.label_id", prefix)).
		Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix)).
		Group("l.scientific_name").
		Scan(&v2Data).Error
	if err != nil {
		return nil, err
	}

	// The query groups by the raw label scientific_name. A legacy
	// "ScientificName_CommonName" label and the clean scientific name are distinct
	// label rows that both normalize to the same species here, so merge their counts
	// rather than emitting duplicate rows: the Manage view keys stats by scientific
	// name, so duplicates would otherwise silently drop (undercount) one label's counts.
	byName := make(map[string]*datastore.SpeciesReviewStat, len(v2Data))
	order := make([]string, 0, len(v2Data))
	for _, d := range v2Data {
		sciName := detection.ExtractScientificName(d.ScientificName)
		stat, ok := byName[sciName]
		if !ok {
			stat = &datastore.SpeciesReviewStat{
				ScientificName: sciName,
				CommonName:     ds.resolveCommonName(sciName),
			}
			byName[sciName] = stat
			order = append(order, sciName)
		}
		stat.Total += int(d.Total)
		stat.Verified += int(d.Verified)
		stat.Rejected += int(d.Rejected)
	}

	result := make([]datastore.SpeciesReviewStat, 0, len(order))
	for _, sciName := range order {
		result = append(result, *byName[sciName])
	}
	return result, nil
}

// GetSpeciesNoteIDs returns the string IDs of all detections for the given
// scientific name, matching legacy "ScientificName_CommonName" labels on the
// scientific-name portion.
func (ds *Datastore) GetSpeciesNoteIDs(ctx context.Context, scientificName string) ([]string, error) {
	name := detection.ExtractScientificName(scientificName)
	if name == "" {
		return []string{}, nil
	}
	prefix := ds.manager.TablePrefix()
	// Match the clean scientific name exactly, or a legacy concatenated
	// "ScientificName_CommonName" label. The separator underscore is a LIKE
	// wildcard, so escape it (and any metacharacters in name) with '!' so e.g.
	// "Motacilla alba" does not also match "Motacilla alba alba".
	var ids []uint
	err := ds.manager.DB().WithContext(ctx).Table(prefix+"detections d").
		Joins(fmt.Sprintf("JOIN %slabels l ON l.id = d.label_id", prefix)).
		Where("l.scientific_name = ? OR l.scientific_name LIKE ? ESCAPE '!'", name, likeEscapeScientificName(name)+"!_%").
		Pluck("d.id", &ids).Error
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, strconv.FormatUint(uint64(id), 10))
	}
	return result, nil
}
