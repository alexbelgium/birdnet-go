package conf

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"math"
	"testing"
)

func TestForkBatThresholdConfig(t *testing.T) {
	t.Parallel()
	const config = "forkmodels:\n  batdetect2:\n    threshold: 0.4\n  bsgbat:\n    threshold: 0.9\n"
	var settings Settings
	require.NoError(t, yaml.Unmarshal([]byte(config), &settings))
	assert.InDelta(t, .4, BatModelThreshold(&settings, ModelIDBatDetect2), 1e-6)
	assert.InDelta(t, .9, BatModelThreshold(&settings, ModelIDBSGBat), 1e-6)
	clone := CloneSettings(&settings)
	assert.Equal(t, settings.ForkModels, clone.ForkModels)
	for _, invalid := range []float64{-.1, 1.1, math.NaN(), math.Inf(1)} {
		settings.ForkModels.BatDetect2.Threshold = invalid
		require.ErrorContains(t, validateModelThresholds(&settings), "forkmodels.batdetect2.threshold")
		settings.normalizeForkBatThresholds()
		assert.Zero(t, settings.ForkModels.BatDetect2.Threshold)
	}
}
