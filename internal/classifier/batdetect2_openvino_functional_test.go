//go:build openvino

package classifier

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/inference"
)

// TestBatDetect2OpenVINOParity compares both stitched heatmaps and final species
// scores on every full tile window of the supplied ultrasonic clips. Set
// BATDETECT2_TESTDATA_DIR to hf_bd2, BAT_OV_PCM_DIR to signed 16-bit *.256000.pcm
// clips, OV_TEST_LIB, and ONNX_RUNTIME_PATH. Fixtures are external to the repo.
func TestBatDetect2OpenVINOParity(t *testing.T) {
	dir, pcmDir := os.Getenv(batDetect2TestDataEnv), os.Getenv(batOpenVINOPCMDirEnv)
	if dir == "" || pcmDir == "" {
		t.Skip("set BATDETECT2_TESTDATA_DIR and BAT_OV_PCM_DIR to run BatDetect2 parity")
	}
	cfg := BatDetect2ModelConfig{
		ModelPath: filepath.Join(dir, "batdetect2-uk-same.onnx"), LabelPath: filepath.Join(dir, "labels.json"),
		ONNXRuntimePath: filepath.Join(t.TempDir(), "missing-ort.so"), Threads: 1,
		Backend: conf.BackendPrefOpenVINO, OpenVINOPath: os.Getenv("OV_TEST_LIB"), OpenVINODevice: conf.OVDeviceCPU,
	}
	t.Cleanup(func() { assert.NoError(t, inference.DestroyOpenVINO()) })
	ovModel, err := NewBatDetect2(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ovModel.Close()) })
	device, backend, precision := ovModel.RuntimeInfo()
	require.Equal(t, BackendOpenVINO, backend, "fallback must fail this parity test")
	assert.Equal(t, inference.OVDeviceCPU, device)
	assert.Equal(t, string(QuantizationFP32), precision)
	assert.Nil(t, ovModel.session)
	require.NotNil(t, ovModel.ovClassifier)
	cfg.Backend = conf.BackendPrefONNX
	cfg.ONNXRuntimePath = os.Getenv("ONNX_RUNTIME_PATH")
	ortModel, err := NewBatDetect2(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ortModel.Close()) })
	assert.Equal(t, ortModel.Labels(), ovModel.Labels())
	paths := batOpenVINOPCMPaths(t, pcmDir)
	maxDetection, maxClasses, maxScores := 0.0, 0.0, 0.0
	windows, detections := 0, 0
	for _, path := range paths {
		pcm, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		samples := batOpenVINODecodePCM(t, pcm)
		for start := 0; start+batDetect2ExpectedSamples <= len(samples); start += batDetect2ExpectedSamples {
			window := samples[start : start+batDetect2ExpectedSamples]
			want, inferErr := ortModel.Predict(t.Context(), [][]float32{window})
			require.NoError(t, inferErr)
			got, inferErr := ovModel.Predict(t.Context(), [][]float32{window})
			require.NoError(t, inferErr)
			maxScores = math.Max(maxScores, batOpenVINOCompareResults(t, want, got))
			maxDetection = math.Max(maxDetection, batOpenVINOMaxDiff(ortModel.fullDetection, ovModel.fullDetection))
			maxClasses = math.Max(maxClasses, batOpenVINOMaxDiff(ortModel.fullClasses, ovModel.fullClasses))
			detections += len(want)
			windows++
		}
	}
	require.Positive(t, windows)
	require.Positive(t, detections)
	assert.LessOrEqual(t, maxDetection, batOpenVINOHeatmapTolerance)
	assert.LessOrEqual(t, maxClasses, batOpenVINOHeatmapTolerance)
	assert.LessOrEqual(t, maxScores, batOpenVINOScoreTolerance)
	t.Logf("device=%s precision=%s clips=%d windows=%d detections=%d max abs detection heatmap diff=%.9g max abs class heatmap diff=%.9g max abs detection score diff=%.9g",
		device, precision, len(paths), windows, detections, maxDetection, maxClasses, maxScores)
	require.NoError(t, ovModel.Close())
	require.NoError(t, ovModel.Close())
	_, err = ovModel.Predict(t.Context(), [][]float32{make([]float32, batDetect2ExpectedSamples)})
	require.ErrorContains(t, err, "classifier is closed")
}
