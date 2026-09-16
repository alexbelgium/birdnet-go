package datastore

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// SpeciesToolRow contains only the metrics requested by the species workspace.
type SpeciesToolRow struct {
	ScientificName string   `json:"scientific_name"`
	CommonName     string   `json:"common_name"`
	SpeciesCode    string   `json:"species_code"`
	Count          *int64   `json:"count,omitempty"`
	MaxConfidence  *float64 `json:"max_confidence,omitempty"`
	LastHeard      *string  `json:"last_heard,omitempty"`
	LastTimestamp  *int64   `json:"-"`
}

// SpeciesRecordingCandidate is metadata only; availability is checked by the API's secure filesystem.
type SpeciesRecordingCandidate struct {
	ID             uint    `json:"id"`
	ScientificName string  `json:"scientific_name"`
	Confidence     float64 `json:"confidence"`
	ClipName       string  `json:"-"`
	Locked         bool    `json:"locked"`
}

// SelectSpeciesToolFields builds a projection from trusted expressions, never client SQL.
func SelectSpeciesToolFields(fields []string, count, confidence, last string) ([]string, error) {
	expressions := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case "count":
			expressions = append(expressions, count+" AS count")
		case "max_confidence":
			expressions = append(expressions, confidence+" AS max_confidence")
		case "last_heard":
			expressions = append(expressions, last)
		default:
			return nil, fmt.Errorf("unknown species tools field %q", field)
		}
	}
	return expressions, nil
}

// GetSpeciesTools returns every stored species, including species with only rejected detections.
func (ds *DataStore) GetSpeciesTools(ctx context.Context, fields []string) ([]SpeciesToolRow, error) {
	columns := []string{"n.scientific_name", "MAX(n.common_name) AS common_name", "MAX(n.species_code) AS species_code"}
	projection, err := SelectSpeciesToolFields(fields, "COUNT(*)", "MAX(CASE WHEN r.verified IS NULL OR r.verified != 'false_positive' THEN n.confidence END)", "MAX(n.date || ' ' || n.time) AS last_heard")
	if err != nil {
		return nil, err
	}
	if ds.DB.Dialector.Name() == "mysql" {
		for i, expression := range projection {
			projection[i] = strings.ReplaceAll(expression, "n.date || ' ' || n.time", "CONCAT(n.date, ' ', n.time)")
		}
	}
	query := ds.DB.WithContext(ctx).Table("notes n")
	for _, field := range fields {
		if field == "max_confidence" {
			query = query.Joins("LEFT JOIN note_reviews r ON r.note_id = n.id")
			break
		}
	}
	rows := make([]SpeciesToolRow, 0)
	err = query.Select(strings.Join(append(columns, projection...), ", ")).Group("n.scientific_name").Order("n.scientific_name").Scan(&rows).Error
	return rows, err
}

// SpeciesToolsCandidates queries a bounded page ordered by lock preference and confidence.
func SpeciesToolsCandidates(ctx context.Context, query *gorm.DB, offset int) ([]SpeciesRecordingCandidate, error) {
	const pageSize = 200
	rows := make([]SpeciesRecordingCandidate, 0)
	if offset < 0 {
		return nil, fmt.Errorf("negative recording offset")
	}
	// Isolate projected names from joined tables (each relation also has an id).
	err := query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Table("(?) AS candidates", query).
		Order("locked DESC, confidence DESC, id ASC").Limit(pageSize).Offset(offset).Scan(&rows).Error
	return rows, err
}

// GetSpeciesToolRecordings returns candidates without reading any audio files.
func (ds *DataStore) GetSpeciesToolRecordings(ctx context.Context, names []string, offset int) ([]SpeciesRecordingCandidate, error) {
	query := ds.DB.Table("notes n").Select("n.id, n.scientific_name, n.confidence, n.clip_name, CASE WHEN l.note_id IS NULL THEN 0 ELSE 1 END AS locked").
		Joins("LEFT JOIN note_locks l ON l.note_id = n.id").Joins("LEFT JOIN note_reviews r ON r.note_id = n.id").
		Where("n.clip_name IS NOT NULL AND n.clip_name != ''").Where("r.verified IS NULL OR r.verified != ?", "false_positive")
	return SpeciesToolsCandidates(ctx, query.Where("n.scientific_name IN ?", names), offset)
}
