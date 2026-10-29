package reanalyze

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/detection"
)

func TestCorrectionNote_DescribesASpeciesChange(t *testing.T) {
	t.Parallel()

	existing := &detection.Result{
		Species:    detection.Species{ScientificName: "Aegithalos caudatus", CommonName: "Long-tailed Tit"},
		Confidence: 0.996,
	}
	model := classifier.ModelInfo{Name: "BirdNET v2.4", DetectionName: "BirdNET", DetectionVersion: "2.4"}

	got := correctionNote(existing, "Ficedula hypoleuca", "Pied Flycatcher", &model, 0.869)

	assert.Contains(t, got, "Aegithalos caudatus (Long-tailed Tit)")
	assert.Contains(t, got, "Ficedula hypoleuca (Pied Flycatcher)")
	assert.Contains(t, got, "BirdNET v2.4")
	assert.Contains(t, got, "86.9%")
	assert.Contains(t, got, "99.6%")
}

func TestCorrectionNote_IsEmptyWhenNothingChanged(t *testing.T) {
	t.Parallel()

	// Confirming the species a detection already has records a review, but a note
	// reading "changed from X to X" is noise, not an audit trail.
	existing := &detection.Result{
		Species:    detection.Species{ScientificName: "Parus major", CommonName: "Great Tit"},
		Confidence: 0.9,
	}
	model := classifier.ModelInfo{Name: "BirdNET v2.4", DetectionName: "BirdNET", DetectionVersion: "2.4"}

	assert.Empty(t, correctionNote(existing, "Parus major", "Great Tit", &model, 0.9))
	// Case differences are not a change either.
	assert.Empty(t, correctionNote(existing, "parus major", "Great Tit", &model, 0.9))
}

func TestCorrectionNote_ReportsAModelChangeOnItsOwn(t *testing.T) {
	t.Parallel()

	// Re-attributing the same species to a different classifier IS a change worth
	// recording, even though the species text is identical.
	existing := &detection.Result{
		Species:    detection.Species{ScientificName: "Parus major", CommonName: "Great Tit"},
		Confidence: 0.71,
		Model:      detection.ModelInfo{Name: "BirdNET", Version: "2.4"},
	}
	model := classifier.ModelInfo{Name: "Google Perch v2", DetectionName: "Perch", DetectionVersion: "V2"}

	got := correctionNote(existing, "Parus major", "Great Tit", &model, 0.88)
	assert.Contains(t, got, "confirmed as Parus major")
	assert.Contains(t, got, "model changed from BirdNET 2.4 to Google Perch v2")
}

func TestCorrectionNote_DoesNotGuessAtAnUnknownPreviousModel(t *testing.T) {
	t.Parallel()

	// The legacy notes schema has no model column, so Note.Model is empty on that
	// read path. Claiming "model changed from  to X" there would invent history.
	existing := &detection.Result{
		Species:    detection.Species{ScientificName: "Parus major", CommonName: "Great Tit"},
		Confidence: 0.71,
	}
	model := classifier.ModelInfo{Name: "Google Perch v2", DetectionName: "Perch", DetectionVersion: "V2"}

	// Same species and no known previous model: nothing demonstrably changed.
	assert.Empty(t, correctionNote(existing, "Parus major", "Great Tit", &model, 0.88))

	// A species change still records, and attributes rather than claims a change.
	got := correctionNote(existing, "Ficedula hypoleuca", "Pied Flycatcher", &model, 0.88)
	assert.Contains(t, got, "attributed to Google Perch v2")
	assert.NotContains(t, got, "model changed")
}

func TestFormatSpecies_OmitsARedundantCommonName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Parus major (Great Tit)", formatSpecies("Parus major", "Great Tit"))
	// Perch-style bare scientific labels resolve to themselves; "X (X)" is noise.
	assert.Equal(t, "Parus major", formatSpecies("Parus major", ""))
	assert.Equal(t, "Parus major", formatSpecies("Parus major", "parus major"))
}

func TestCorrectionNote_TreatsAVariantSwapAsAModelChange(t *testing.T) {
	t.Parallel()

	// Variant is part of the persisted model identity, so re-attributing the same
	// species from the stock model to a custom variant of the same name and
	// version is a real change — and one the operator has no other record of.
	existing := &detection.Result{
		Species:    detection.Species{ScientificName: "Parus major", CommonName: "Great Tit"},
		Confidence: 0.8,
		Model:      detection.ModelInfo{Name: "BirdNET", Version: "2.4"},
	}
	custom := classifier.ModelInfo{
		Name:             "BirdNET v2.4 (custom)",
		DetectionName:    "BirdNET",
		DetectionVersion: "2.4",
		CustomPath:       "/models/mine.tflite",
	}

	got := correctionNote(existing, "Parus major", "Great Tit", &custom, 0.9)
	require.NotEmpty(t, got, "a variant swap must still be recorded")
	assert.Contains(t, got, "model changed from BirdNET 2.4")
}

func TestCorrectionNote_SameModelIdentityIsStillNoChange(t *testing.T) {
	t.Parallel()

	// The mirror of the above: an identical identity must not manufacture a note,
	// or confirming an existing species would spam the detection's notes.
	stock := classifier.ModelInfo{Name: "BirdNET v2.4", DetectionName: "BirdNET", DetectionVersion: "2.4"}
	existing := &detection.Result{
		Species:    detection.Species{ScientificName: "Parus major", CommonName: "Great Tit"},
		Confidence: 0.8,
		Model:      stock.ToDetectionModelInfo(),
	}

	assert.Empty(t, correctionNote(existing, "Parus major", "Great Tit", &stock, 0.8))
}

func TestTaxonomicClassForModelType_MatchesTheSavePath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Aves", taxonomicClassForModelType(entities.ModelTypeBird))
	assert.Equal(t, "Chiroptera", taxonomicClassForModelType(entities.ModelTypeBat))
	// Multi-taxa models (Perch) span classes, so a new label gets none rather
	// than being filed as a bird.
	assert.Empty(t, taxonomicClassForModelType(entities.ModelTypeMulti))
}
