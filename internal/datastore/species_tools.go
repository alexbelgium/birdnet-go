package datastore

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tphakala/birdnet-go/internal/datastore/entities"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/errors"
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
	projection, err := SelectSpeciesToolFields(fields, "COUNT(*)", "MAX(CASE WHEN r.verified IS NULL OR r.verified != 'false_positive' THEN n.confidence END)", "MAX(n.date || ' ' || n.time) AS last_heard")
	if err != nil {
		return nil, err
	}
	columns := make([]string, 0, 3+len(projection))
	columns = append(columns, "n.scientific_name", "MAX(n.common_name) AS common_name", "MAX(n.species_code) AS species_code")
	if ds.DB.Name() == DialectMySQL {
		for i, expression := range projection {
			projection[i] = strings.ReplaceAll(expression, "n.date || ' ' || n.time", "CONCAT(n.date, ' ', n.time)")
		}
	}
	query := ds.DB.WithContext(ctx).Table("notes n")
	if slices.Contains(fields, "max_confidence") {
		query = query.Joins("LEFT JOIN note_reviews r ON r.note_id = n.id")
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

// SpeciesReviewStat holds per-species detection and review counts used by the
// analytics "Manage" view. Total counts every detection (including false
// positives) so the verified/rejected ratio reflects all reviews.
type SpeciesReviewStat struct {
	ScientificName string `json:"scientificName"`
	CommonName     string `json:"commonName"`
	Total          int    `json:"total"`
	Verified       int    `json:"verified"`
	Rejected       int    `json:"rejected"`
}

// GetSpeciesReviewStats returns per-species detection and review counts across
// all time. Unlike GetSpeciesSummaryData it intentionally does not exclude false
// positives, so the rejected count reflects every detection marked as such.
func (ds *DataStore) GetSpeciesReviewStats(ctx context.Context) ([]SpeciesReviewStat, error) {
	stats := make([]SpeciesReviewStat, 0, 100)

	// note_reviews.note_id carries a uniqueIndex (at most one review per note), so
	// the LEFT JOIN below is 1:1 and cannot multiply rows per note. Plain COUNT is
	// therefore already fan-out-immune; DISTINCT would only add a needless dedup
	// pass (computed three times per group) on what can be a large table.
	const query = `
		SELECT
			notes.scientific_name AS scientific_name,
			COALESCE(MAX(notes.common_name), '') AS common_name,
			COUNT(notes.id) AS total,
			COUNT(CASE WHEN note_reviews.verified = ? THEN notes.id END) AS verified,
			COUNT(CASE WHEN note_reviews.verified = ? THEN notes.id END) AS rejected
		FROM notes
		LEFT JOIN note_reviews ON notes.id = note_reviews.note_id
		GROUP BY notes.scientific_name`

	if err := ds.DB.WithContext(ctx).Raw(query,
		string(entities.VerificationCorrect), string(entities.VerificationFalsePositive)).Scan(&stats).Error; err != nil {
		return nil, dbError(err, "get_species_review_stats", errors.PriorityMedium,
			"action", "generate_species_review_stats")
	}

	return stats, nil
}

// GetSpeciesNoteIDs returns the string IDs of every note for the given scientific
// name. Legacy callers may pass a raw BirdNET label ("ScientificName_CommonName");
// only the scientific-name portion before the first underscore is used.
func (ds *DataStore) GetSpeciesNoteIDs(ctx context.Context, scientificName string) ([]string, error) {
	name := detection.ExtractScientificName(scientificName)
	if name == "" {
		return []string{}, nil
	}

	var ids []uint
	if err := ds.DB.WithContext(ctx).Model(&Note{}).Where("scientific_name = ?", name).Pluck("id", &ids).Error; err != nil {
		return nil, dbError(err, "get_species_note_ids", errors.PriorityMedium,
			"action", "lookup_species_note_ids")
	}

	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, strconv.FormatUint(uint64(id), 10))
	}

	return result, nil
}
