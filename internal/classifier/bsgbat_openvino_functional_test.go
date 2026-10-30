//go:build openvino

package classifier

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/inference"
)

const (
	bsgBatPCMDirectoryEnv        = "BSGBAT_PCM_DIR"
	bsgBatPCMBytes               = 2
	bsgBatPCMScale               = 32768
	bsgBatOpenVINOLogitTolerance = 0.05
	bsgBatOpenVINOScoreTolerance = 0.01
)

// TestBSGBatOpenVINOParity exercises the production constructor, DSP, logits,
// sigmoid, runtime reporting, and cleanup against ORT. Set BSGBAT_TESTDATA_DIR
// to the model/labels directory, BSGBAT_PCM_DIR to a directory of signed 16-bit
// little-endian *.384000.pcm clips, OV_TEST_LIB to libopenvino_c, and optionally
// OV_TEST_DEVICE to cpu (default) or gpu. These fixtures are not shipped in CI.
func TestBSGBatOpenVINOParity(t *testing.T) {
	modelDir, pcmDir := os.Getenv(bsgBatTestDataEnv), os.Getenv(bsgBatPCMDirectoryEnv)
	if modelDir == "" || pcmDir == "" {
		t.Skip("set BSGBAT_TESTDATA_DIR and BSGBAT_PCM_DIR to run OpenVINO/ORT parity")
	}
	device := os.Getenv("OV_TEST_DEVICE")
	if device == "" {
		device = conf.OVDeviceCPU
	}
	cfg := BSGBatModelConfig{
		ModelPath:       filepath.Join(modelDir, "bsgbat_v0.21_r3.onnx"),
		LabelPath:       filepath.Join(modelDir, "bsgbat_labels.txt"),
		ONNXRuntimePath: filepath.Join(t.TempDir(), "missing-ort.so"), Threads: 1,
		Backend: conf.BackendPrefOpenVINO, OpenVINOPath: os.Getenv("OV_TEST_LIB"), OpenVINODevice: device,
	}
	// A successful OV load must not depend on ORT being loadable.
	t.Cleanup(func() { assert.NoError(t, inference.DestroyOpenVINO()) })
	ovModel, err := NewBSGBat(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ovModel.Close()) })
	boundDevice, backend, precision := ovModel.RuntimeInfo()
	require.Equal(t, BackendOpenVINO, backend, "OpenVINO must actually run; fallback is a test failure")
	require.Nil(t, ovModel.session)
	require.NotNil(t, ovModel.ovClassifier)
	assert.Equal(t, bsgBatOutputClasses, ovModel.ovClassifier.NumSpecies())
	assert.Equal(t, bsgBatOutputClasses-1, ovModel.NumSpecies())
	cfg.Backend = conf.BackendPrefONNX
	cfg.ONNXRuntimePath = os.Getenv("ONNX_RUNTIME_PATH")
	ortModel, err := NewBSGBat(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ortModel.Close()) })
	assert.Equal(t, ortModel.Labels(), ovModel.Labels())
	assert.Equal(t, openVINOEffectivePrecision(ovModel.precision), precision)
	assert.Contains(t, []string{inference.OVDeviceCPU, inference.OVDeviceGPU}, boundDevice)
	paths, err := filepath.Glob(filepath.Join(pcmDir, "*.384000.pcm"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	maxLogitDiff, maxScoreDiff := 0.0, 0.0
	windows := 0
	for _, path := range paths {
		pcm, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		require.Zero(t, len(pcm)%bsgBatPCMBytes)
		samples := make([]float32, len(pcm)/bsgBatPCMBytes)
		for i := range samples {
			samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*bsgBatPCMBytes:]))) / bsgBatPCMScale
		}
		require.GreaterOrEqual(t, len(samples), bsgBatRawSampleRate, "clip must contain a full second")
		for offset := 0; offset+bsgBatRawSampleRate <= len(samples); offset += bsgBatRawSampleRate {
			window := samples[offset : offset+bsgBatRawSampleRate]
			_, predictErr := ortModel.Predict(t.Context(), [][]float32{window})
			require.NoError(t, predictErr)
			_, predictErr = ovModel.Predict(t.Context(), [][]float32{window})
			require.NoError(t, predictErr)
			// Both DSP buffers now contain the identical padded, preprocessed input.
			require.Equal(t, ortModel.dsp.output, ovModel.dsp.output)
			got, inferErr := ovModel.ovClassifier.Predict(ovModel.dsp.output)
			require.NoError(t, inferErr)
			want := ortModel.output.GetData()
			require.Len(t, got, bsgBatOutputClasses)
			for i := range want {
				require.False(t, math.IsNaN(float64(got[i])) || math.IsInf(float64(got[i]), 0))
				maxLogitDiff = math.Max(maxLogitDiff, math.Abs(float64(got[i]-want[i])))
				maxScoreDiff = math.Max(maxScoreDiff, math.Abs(customSigmoid(float64(got[i]), 1)-customSigmoid(float64(want[i]), 1)))
			}
			windows++
		}
	}
	assert.LessOrEqual(t, maxLogitDiff, bsgBatOpenVINOLogitTolerance)
	assert.LessOrEqual(t, maxScoreDiff, bsgBatOpenVINOScoreTolerance)
	t.Logf("device=%s precision=%s clips=%d windows=%d max abs logit diff=%.9g max abs sigmoid diff=%.9g",
		boundDevice, precision, len(paths), windows, maxLogitDiff, maxScoreDiff)
	require.NoError(t, ovModel.Close())
	require.NoError(t, ovModel.Close())
	_, err = ovModel.Predict(t.Context(), [][]float32{make([]float32, bsgBatRawSampleRate)})
	require.ErrorContains(t, err, "classifier is closed")
}
