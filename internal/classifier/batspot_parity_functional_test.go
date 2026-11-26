//go:build onnx

package classifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
)

const (
	envBatSpotModel  = "BATSPOT_TEST_MODEL"
	envBatSpotLabels = "BATSPOT_TEST_LABELS"
	envBatSpotORTLib = "ONNX_TEST_ORT_LIB"
)

// TestBatSpotParity_Functional compares batched ONNX inference with the
// upstream PyTorch model at the same 250 kHz PCM inputs.
func TestBatSpotParity_Functional(t *testing.T) {
	modelPath := os.Getenv(envBatSpotModel)
	if modelPath == "" {
		t.Skipf("set %s to the BatSpot m09 ONNX export", envBatSpotModel)
	}
	labelPath := os.Getenv(envBatSpotLabels)
	if labelPath == "" {
		labelPath = batSpotFixtureDir + "/batspot_labels.txt"
	}
	inst, err := newBatSpot(modelPath, labelPath, os.Getenv(envBatSpotORTLib), 1)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, inst.Close()) })

	batch := len(batSpotFixtures)
	for i, name := range batSpotFixtures {
		pcm := readBatSpotFloat32(t, name+".pcm.f32", batSpotCallSamples)
		inst.frontend.window(inst.input[i*batSpotInputSize:][:batSpotInputSize], pcm)
	}
	logits := inst.logits[:batch*batSpotClassCount]
	require.NoError(t, inst.model.Run(inst.input[:batch*batSpotInputSize], logits))
	for i, name := range batSpotFixtures {
		t.Run(name, func(t *testing.T) {
			var expected struct {
				Logits        []float32 `json:"logits"`
				Probabilities []float32 `json:"probabilities"`
			}
			require.NoError(t, json.Unmarshal(readBatSpotFixture(t, name+".json"), &expected))
			row := logits[i*batSpotClassCount:][:batSpotClassCount]
			require.InDeltaSlice(t, expected.Logits, row, 1e-3)
			probs := batSpotSoftmax(row)
			require.InDeltaSlice(t, expected.Probabilities, probs[:], 1e-4)
		})
	}
}

func TestBatSpotConfiguredReload_Functional(t *testing.T) {
	modelPath := os.Getenv(envBatSpotModel)
	if modelPath == "" {
		t.Skipf("set %s to the BatSpot m09 ONNX export", envBatSpotModel)
	}
	isolateTestConfig(t)
	previous := conf.GetSettings()
	t.Cleanup(func() { conftest.SetTestSettings(previous) })

	// Start from the exact files an install places under the gallery directory.
	settings := conftest.GetTestSettings()
	settings.Models.Directory = t.TempDir()
	installed := filepath.Join(settings.Models.Directory, conf.ModelIDBatSpot)
	require.NoError(t, os.Mkdir(installed, 0o700))
	graph, err := os.ReadFile(modelPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(installed, batSpotModelFile), graph, 0o600))
	labels := readBatSpotFixture(t, batSpotLabelsFile)
	require.NoError(t, os.WriteFile(filepath.Join(installed, batSpotLabelsFile), labels, 0o600))

	settings.Models.Enabled = []string{conf.ModelIDBatSpot}
	settings.BirdNET.ONNXRuntimePath = os.Getenv(envBatSpotORTLib)
	settings.BirdNET.Threads = 1
	settings.BatSpot = conf.BatSpotConfig{Threshold: 0.5}
	conftest.SetTestSettings(settings)

	o, err := NewOrchestrator(settings)
	require.NoError(t, err)
	t.Cleanup(o.Delete)
	require.True(t, o.IsModelLoaded(RegistryIDBatSpot), "load failures: %v", o.LoadErrors())

	clip := make([]float32, batSpotClipSamples)
	pcm := readBatSpotFloat32(t, "sweep40k.pcm.f32", batSpotCallSamples)
	copy(clip, pcm)
	results, err := o.PredictModel(t.Context(), RegistryIDBatSpot, [][]float32{clip})
	require.NoError(t, err)
	const expected = "Myotis daubentonii_Daubenton's Bat"
	scores := make(map[string]float32, len(results))
	for _, result := range results {
		scores[result.Species] = result.Confidence
	}
	require.Greater(t, scores[expected], float32(0.9))

	bad := conf.CloneSettings(settings)
	bad.BatSpot.LabelPath = filepath.Join(t.TempDir(), "invalid.txt")
	require.NoError(t, os.WriteFile(bad.BatSpot.LabelPath, []byte("invalid"), 0o600))
	conftest.SetTestSettings(bad)
	_, _, err = o.ReconcileEnabledModels()
	require.Error(t, err)
	require.Contains(t, o.AllLabels(), expected)

	good := conf.CloneSettings(settings)
	good.BatSpot.LabelPath = filepath.Join(t.TempDir(), "changed.txt")
	newLabel := strings.Replace(expected, "Daubenton's Bat", "Water Bat", 1)
	changed := strings.Replace(string(labels), expected, newLabel, 1)
	require.NoError(t, os.WriteFile(good.BatSpot.LabelPath, []byte(changed), 0o600))
	conftest.SetTestSettings(good)
	_, _, err = o.ReconcileEnabledModels()
	require.NoError(t, err)
	require.Contains(t, o.AllLabels(), newLabel)
	require.NotContains(t, o.AllLabels(), expected)
}
