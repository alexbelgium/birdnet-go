package processor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/analysis/species"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/events"
)

func TestPopulateEventMetadata_ModelName(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		model   detection.ModelInfo
		display string
	}{
		{name: "BirdNET", model: detection.DefaultModelInfo(), display: "BirdNET 2.4"},
		{name: "Perch", model: detection.ModelInfo{Name: "Perch", Version: "V2"}, display: "Perch V2"},
		{name: "name only", model: detection.ModelInfo{Name: "Custom"}, display: "Custom"},
		{name: "absent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			action := &DatabaseAction{}
			action.Result.Model = tt.model
			event, err := events.NewDetectionEvent("Robin", "Erithacus rubecula", 0.9, "", false, 0)
			require.NoError(t, err)
			action.populateEventMetadata(event, species.NoveltyStatus{})
			assert.Equal(t, tt.display, event.GetMetadata()[events.DetectionMetadataModelName])
		})
	}
}
