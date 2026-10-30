package classifier

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
)

func TestBSGBatBackgroundGateHotReload(t *testing.T) {
	original := conf.GetSettings()
	t.Cleanup(func() { conftest.SetTestSettings(original) })
	conftest.SetTestSettings(nil)

	fallback := &conf.Settings{}
	fallback.ForkModels.BSGBat.BackgroundMax = .99
	cfg := bsgBatConfigFor(fallback, "model.onnx", "labels.txt", 1)
	require.Same(t, fallback, cfg.Settings)
	dsp, err := newBSGBatDSP()
	require.NoError(t, err)
	logits := make([]float32, bsgBatOutputClasses)
	for i := range logits {
		logits[i] = -10
	}
	logits[0], logits[1] = 3, 2
	backend := &predTelemetryClassifier{logits: logits}
	model := &BSGBat{
		batORTBase: &batORTBase{labels: []string{"Bat A", "Bat B"}},
		settings:   cfg.Settings, dsp: dsp, ovClassifier: backend,
	}
	audio := [][]float32{make([]float32, bsgBatRawSampleRate)}

	// Direct callers use their supplied settings until the live snapshot exists.
	logits[bsgBatOutputClasses-1] = 6
	results, err := model.Predict(t.Context(), audio)
	require.NoError(t, err)
	assert.Empty(t, results)

	for _, tc := range []struct {
		name           string
		limit          float64
		background     float32
		wantCandidates bool
	}{
		{"disabled", 0, 6, true},
		{"reject all species", .99, 6, false},
		{"keep real call", .99, 4, true},
		{"lower limit", .98, 4, false},
		{"equal limit is kept", .5, 0, true},
		{"just above limit", .5, .01, false},
		{"maximum limit", 1, 6, true},
		{"disabled again", 0, 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := conf.CloneSettings(fallback)
			settings.ForkModels.BSGBat.BackgroundMax = tc.limit
			conftest.SetTestSettings(settings)
			logits[bsgBatOutputClasses-1] = tc.background
			results, predictErr := model.Predict(t.Context(), audio)
			require.NoError(t, predictErr)
			if !tc.wantCandidates {
				assert.Empty(t, results)
				return
			}
			require.Len(t, results, 2)
			assert.Equal(t, "Bat A", results[0].Species)
			assert.Equal(t, "Bat B", results[1].Species)
			assert.InDelta(t, customSigmoid(3, 1), results[0].Confidence, 1e-6)
			assert.InDelta(t, customSigmoid(2, 1), results[1].Confidence, 1e-6)
		})
	}

	logits[bsgBatOutputClasses-1] = float32(math.NaN())
	_, err = model.Predict(t.Context(), audio)
	require.Error(t, err, "non-finite Background must still be rejected as an inference error")
}
