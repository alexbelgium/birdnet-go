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
	"github.com/tphakala/birdnet-go/internal/audiocore/resample"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/inference"
)

const (
	batOpenVINOPCMDirEnv          = "BAT_OV_PCM_DIR"
	batOpenVINOPCMBytes           = 2
	batOpenVINOPCMScale           = 32768
	batOpenVINOHeatmapTolerance   = 0.0001
	batOpenVINOScoreTolerance     = 0.0001
	batOpenVINOEmbeddingTolerance = 0.05
	batOpenVINOEmbeddingRate      = 48000
)

// TestBatOpenVINOEmbeddingRegressionParity covers the production NewBat and
// tryBatOpenVINO path, including the rank-2 BirdNET embedding backbone and ORT
// classifier head. Set BAT_OV_MODEL_DIR to bat_hf, BAT_OV_PCM_DIR to a directory
// of *.256000.pcm signed 16-bit clips, OV_TEST_LIB, and ONNX_RUNTIME_PATH.
func TestBatOpenVINOEmbeddingRegressionParity(t *testing.T) {
	dir, pcmDir := os.Getenv("BAT_OV_MODEL_DIR"), os.Getenv(batOpenVINOPCMDirEnv)
	if dir == "" || pcmDir == "" {
		t.Skip("set BAT_OV_MODEL_DIR and BAT_OV_PCM_DIR to run BattyBirdNET parity")
	}
	settings := conftest.GetTestSettings()
	settings.Bat.Threshold = 0
	conftest.SetTestSettings(settings)
	t.Cleanup(func() { conftest.SetTestSettings(nil) })
	cfg := BatModelConfig{
		EmbeddingModelPath: filepath.Join(dir, "embedding.onnx"), EmbeddingLabels: []string{"placeholder"},
		ClassifierModelPath: filepath.Join(dir, "bavaria.onnx"), ClassifierLabelPath: filepath.Join(dir, "bavaria-labels.txt"),
		ONNXRuntimePath: os.Getenv("ONNX_RUNTIME_PATH"), Threads: 1,
		Backend: conf.BackendPrefOpenVINO, OpenVINOPath: os.Getenv("OV_TEST_LIB"), OpenVINODevice: conf.OVDeviceCPU,
	}
	t.Cleanup(func() { assert.NoError(t, inference.DestroyOpenVINO()) })
	ovModel, err := NewBat(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ovModel.Close()) })
	device, backend, precision := ovModel.RuntimeInfo()
	require.Equal(t, BackendOpenVINO, backend, "fallback must fail this regression check")
	assert.Equal(t, inference.OVDeviceCPU, device)
	assert.Equal(t, string(QuantizationFP32), precision)
	cfg.Backend = conf.BackendPrefONNX
	ortModel, err := NewBat(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ortModel.Close()) })
	paths := batOpenVINOPCMPaths(t, pcmDir)
	maxEmbedding, maxScore := 0.0, 0.0
	windows, detections := 0, 0
	for _, path := range paths {
		pcm, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		pcm, readErr = resample.ResampleBytes(pcm, batDetect2SampleRate, batOpenVINOEmbeddingRate)
		require.NoError(t, readErr)
		samples := batOpenVINODecodePCM(t, pcm)
		for start := 0; start+batDetect2ExpectedSamples <= len(samples); start += batDetect2ExpectedSamples {
			window := samples[start : start+batDetect2ExpectedSamples]
			_, wantEmbedding, inferErr := ortModel.embeddingExtractor.PredictWithEmbeddings(window)
			require.NoError(t, inferErr)
			_, gotEmbedding, inferErr := ovModel.embeddingExtractor.PredictWithEmbeddings(window)
			require.NoError(t, inferErr)
			require.Len(t, gotEmbedding, len(wantEmbedding))
			maxEmbedding = math.Max(maxEmbedding, batOpenVINOMaxDiff(wantEmbedding, gotEmbedding))
			want, inferErr := ortModel.Predict(t.Context(), [][]float32{window})
			require.NoError(t, inferErr)
			got, inferErr := ovModel.Predict(t.Context(), [][]float32{window})
			require.NoError(t, inferErr)
			maxScore = math.Max(maxScore, batOpenVINOCompareResults(t, want, got))
			detections += len(want)
			windows++
		}
	}
	require.Positive(t, windows)
	require.Positive(t, detections)
	assert.LessOrEqual(t, maxEmbedding, batOpenVINOEmbeddingTolerance)
	assert.LessOrEqual(t, maxScore, batOpenVINOScoreTolerance)
	t.Logf("device=%s precision=%s clips=%d windows=%d detections=%d max abs embedding diff=%.9g max abs detection score diff=%.9g",
		device, precision, len(paths), windows, detections, maxEmbedding, maxScore)
}

func batOpenVINOPCMPaths(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.256000.pcm"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	return paths
}

func batOpenVINODecodePCM(t *testing.T, pcm []byte) []float32 {
	t.Helper()
	require.Zero(t, len(pcm)%batOpenVINOPCMBytes)
	samples := make([]float32, len(pcm)/batOpenVINOPCMBytes)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*batOpenVINOPCMBytes:]))) / batOpenVINOPCMScale
	}
	return samples
}

func batOpenVINOMaxDiff(want, got []float32) float64 {
	maxDiff := 0.0
	for i, value := range want {
		diff := math.Abs(float64(value) - float64(got[i]))
		if math.IsNaN(diff) || math.IsInf(diff, 0) {
			return math.Inf(1)
		}
		maxDiff = math.Max(maxDiff, diff)
	}
	return maxDiff
}

func batOpenVINOCompareResults(t *testing.T, want, got []datastore.Results) float64 {
	t.Helper()
	require.Len(t, got, len(want), "the same species must pass the detection floor")
	scores := make(map[string]float32, len(got))
	for _, result := range got {
		scores[result.Species] = result.Confidence
	}
	maxDiff := 0.0
	for _, result := range want {
		score, ok := scores[result.Species]
		require.True(t, ok, "missing species %s", result.Species)
		diff := math.Abs(float64(result.Confidence) - float64(score))
		require.False(t, math.IsNaN(diff) || math.IsInf(diff, 0))
		maxDiff = math.Max(maxDiff, diff)
	}
	return maxDiff
}
