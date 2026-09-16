package v2only

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/detection"
)

// GetSpeciesTools projects only the enabled metrics, without filtering rejected species out of the inventory.
func (ds *Datastore) GetSpeciesTools(ctx context.Context, fields []string) ([]datastore.SpeciesToolRow, error) {
	projection, err := datastore.SelectSpeciesToolFields(fields, "COUNT(*)", "MAX(CASE WHEN r.verified IS NULL OR r.verified != 'false_positive' THEN d.confidence END)", "MAX(d.detected_at) AS last_timestamp")
	if err != nil {
		return nil, err
	}
	prefix := ds.manager.TablePrefix()
	query := ds.manager.DB().WithContext(ctx).Table(prefix + "detections d").Joins(fmt.Sprintf("JOIN %slabels l ON l.id = d.label_id", prefix))
	for _, field := range fields {
		if field == "max_confidence" {
			query = query.Joins(fmt.Sprintf("LEFT JOIN %sdetection_reviews r ON r.detection_id = d.id", prefix))
			break
		}
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
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(name)
		clauses = append(clauses, "l.scientific_name = ? OR l.scientific_name LIKE ? ESCAPE '!'")
		args = append(args, name, escaped+"!_%")
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
