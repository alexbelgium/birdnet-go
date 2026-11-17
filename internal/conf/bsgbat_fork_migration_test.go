package conf

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateLegacyForkBSGBat(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("forkmodels.bsgbat.modelpath", "/models/legacy.onnx")
	viper.Set("forkmodels.bsgbat.labelpath", "/models/legacy.txt")
	viper.Set("forkmodels.bsgbat.threshold", 0.7)

	settings := &Settings{}
	require.True(t, settings.MigrateLegacyForkBSGBat())
	assert.Equal(t, "/models/legacy.onnx", settings.BSGBat.ModelPath)
	assert.Equal(t, "/models/legacy.txt", settings.BSGBat.LabelPath)
	assert.InDelta(t, 0.7, settings.BSGBat.Threshold, 1e-6)
}

func TestMigrateLegacyForkBSGBatNativeValuesWin(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("forkmodels.bsgbat.modelpath", "/models/legacy.onnx")
	viper.Set("bsgbat.modelpath", "/models/native.onnx")

	settings := &Settings{BSGBat: BSGBatConfig{ModelPath: "/models/native.onnx"}}
	assert.False(t, settings.MigrateLegacyForkBSGBat())
	assert.Equal(t, "/models/native.onnx", settings.BSGBat.ModelPath)
}

func TestMigrateLegacyForkBSGBatInheritedThreshold(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("forkmodels.bsgbat.threshold", 0)

	settings := &Settings{}
	settings.Bat.Threshold = 0.65
	require.True(t, settings.MigrateLegacyForkBSGBat())
	assert.InDelta(t, 0.65, settings.BSGBat.Threshold, 1e-6)
}
