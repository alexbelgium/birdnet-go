package conf

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBSGBatBackgroundMaxValidationAndNormalize(t *testing.T) {
	t.Parallel()
	for _, v := range []float64{0, 0.5, DefaultBSGBatBackgroundMax, 1} {
		s := createMinimalValidSettings()
		s.BSGBat.BackgroundMax = v
		require.NoError(t, ValidateSettings(s), "value %v", v)
	}
	for _, v := range []float64{-0.1, 1.001, math.NaN(), math.Inf(1)} {
		s := createMinimalValidSettings()
		s.BSGBat.BackgroundMax = v
		err := ValidateSettings(s)
		require.Error(t, err, "value %v", v)
		assert.Contains(t, err.Error(), "bsgbat.backgroundmax")

		s.normalizeModelThresholds()
		assert.InDelta(t, DefaultBSGBatBackgroundMax, s.BSGBat.BackgroundMax, 1e-9)
		assert.NotEmpty(t, s.ValidationWarnings)
		require.NoError(t, ValidateSettings(s))
	}
}
