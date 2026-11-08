package classifier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/inference/bsgbat"

	"github.com/tphakala/birdnet-go/internal/conf/conftest"
)

// TestBSGBat_BackgroundGate pins bsgbat.backgroundmax: a window whose Background
// sigmoid exceeds it yields no results (not an error), below it results are
// returned, and 0 disables the gate. The value is read per prediction.
// Not parallel: it publishes process-wide settings.
func TestBSGBat_BackgroundGate(t *testing.T) {
	t.Cleanup(func() { conftest.SetTestSettings(nil) })
	window := make([]float32, bsgbat.ClipSamples)
	for i := range window {
		window[i] = 0.1
	}
	// Background is the middle logit (backgroundIndex 1 in newFakeBSGBat).
	for _, tc := range []struct {
		name        string
		limit       float64
		background  float32
		wantResults bool
	}{
		{"disabled keeps loud background", 0, 8, true},
		{"above limit is rejected", 0.9, 8, false},
		{"below limit is kept", 0.9, -2, true},
		{"limit of one never rejects", 1, 8, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := conftest.GetTestSettings()
			s.BSGBat.BackgroundMax = tc.limit
			conftest.SetTestSettings(s)
			inst, _ := newFakeBSGBat(t, []float32{1, tc.background, 2})
			results, err := inst.Predict(t.Context(), [][]float32{window})
			require.NoError(t, err)
			if tc.wantResults {
				assert.Len(t, results, 2)
			} else {
				assert.Empty(t, results)
			}
		})
	}
}
