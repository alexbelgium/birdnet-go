package classifier

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestForkBatCandidatesBelowConfiguredThreshold(t *testing.T) {
	settings := conftest.GetTestSettings()
	settings.Bat.Threshold = 1
	conftest.SetTestSettings(settings)
	t.Cleanup(func() { conf.StoreSettings(nil) })
	for _, id := range []string{RegistryIDBatDetect2, RegistryIDBSGBat} {
		t.Run(id, func(t *testing.T) {
			var model ModelInstance
			var err error
			var samples int
			if id == RegistryIDBatDetect2 {
				dir := os.Getenv(batDetect2TestDataEnv)
				if dir == "" {
					t.Skip("BATDETECT2_TESTDATA_DIR is not configured")
				}
				model, err = NewBatDetect2(&BatDetect2ModelConfig{ModelPath: filepath.Join(dir, "batdetect2-uk-same.onnx"), LabelPath: filepath.Join(dir, "labels.json"), ONNXRuntimePath: os.Getenv("ONNX_RUNTIME_PATH"), Threads: 1})
				samples = batDetect2ExpectedSamples
			} else {
				dir := os.Getenv(bsgBatTestDataEnv)
				if dir == "" {
					t.Skip("BSGBAT_TESTDATA_DIR is not configured")
				}
				model, err = NewBSGBat(&BSGBatModelConfig{ModelPath: filepath.Join(dir, "bsgbat_v0.21_r3.onnx"), LabelPath: filepath.Join(dir, "bsgbat_labels.txt"), ONNXRuntimePath: os.Getenv("ONNX_RUNTIME_PATH"), Threads: 1})
				samples = bsgBatRawSampleRate
			}
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, model.Close()) })
			audio := make([]float32, samples)
			// Repeated ultrasonic sweeps exercise the actual ONNX output path. Silence
			// legitimately produces no BSG-BAT candidates above the fixed floor.
			const pulsePeriod = 0.05
			const pulseDuration = 0.01
			const startFrequency = 30000
			const sweepHzPerSecond = 2000000
			const amplitude = .5
			rate := float64(model.Spec().EffectiveSampleRate())
			for i := range audio {
				position := math.Mod(float64(i)/rate, pulsePeriod)
				if position < pulseDuration {
					audio[i] = amplitude * float32(math.Sin(2*math.Pi*(startFrequency*position+sweepHzPerSecond*position*position/2)))
				}
			}
			results, err := model.Predict(t.Context(), [][]float32{audio})
			require.NoError(t, err)
			require.NotEmpty(t, results, "candidate visibility must not depend on the shared threshold")
			for _, result := range results {
				assert.GreaterOrEqual(t, result.Confidence, BatCandidateFloor)
				assert.Less(t, result.Confidence, float32(settings.Bat.Threshold))
			}
		})
	}
}
