package conf

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBSGBatBackgroundMaxConfig(t *testing.T) {
	t.Parallel()
	var settings Settings
	require.NoError(t, yaml.Unmarshal([]byte("forkmodels:\n  bsgbat:\n    backgroundmax: 0.99\n"), &settings))
	assert.InDelta(t, .99, settings.ForkModels.BSGBat.BackgroundMax, 1e-6)
	assert.Equal(t, settings.ForkModels, CloneSettings(&settings).ForkModels)
	encoded, err := json.Marshal(settings.ForkModels)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"backgroundMax":0.99`)
}

func TestBSGBatBackgroundMaxValidationAndNormalization(t *testing.T) {
	t.Parallel()
	for _, valid := range []float64{0, .5, .98, .99, 1} {
		settings := &Settings{}
		settings.ForkModels.BSGBat.BackgroundMax = valid
		require.NoError(t, validateModelThresholds(settings))
		settings.normalizeForkBatThresholds()
		assert.InDelta(t, valid, settings.ForkModels.BSGBat.BackgroundMax, 1e-6)
		assert.Empty(t, settings.ValidationWarnings)
	}
	for _, invalid := range []float64{-.1, .001, .499, 1.001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		settings := &Settings{}
		settings.ForkModels.BSGBat.BackgroundMax = invalid
		require.ErrorContains(t, validateModelThresholds(settings), "forkmodels.bsgbat.backgroundmax")
		settings.normalizeForkBatThresholds()
		assert.Zero(t, settings.ForkModels.BSGBat.BackgroundMax)
		assert.Contains(t, strings.Join(settings.ValidationWarnings, " "), "forkmodels.bsgbat.backgroundmax")
		require.NoError(t, validateModelThresholds(settings))
	}
}
