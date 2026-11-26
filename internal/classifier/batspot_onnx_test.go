package classifier

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/detection"
)

func TestBatSpotRegistration(t *testing.T) {
	t.Parallel()
	id, ok := ResolveConfigModelID("BATSPOT")
	require.True(t, ok)
	require.Equal(t, RegistryIDBatSpot, id)
	assert.True(t, conf.ValidAudioModels[conf.ModelIDBatSpot])
	// 256 and 384 kHz captures resample to the native 250 kHz.
	assert.GreaterOrEqual(t, conf.MaxValidSampleRate, batSpotSampleRate)
	info, err := DetermineModelInfo("batspot_m09.onnx")
	require.NoError(t, err)
	assert.Equal(t, RegistryIDBatSpot, info.ID)
	assert.Equal(t, batSpotVersion, info.DetectionVersion)
	assert.Equal(t, entities.ModelTypeBat, detection.ResolveModelType(info.DetectionName, info.DetectionVersion))
	assert.Equal(t, TaxonGroupBat, info.taxonGroup)
	assert.True(t, info.scheduleGated)
	assert.Equal(t, batSpotSampleRate, info.Spec.MinRawSampleRate)
	assert.Equal(t, 252500, info.Spec.ClipSampleCount())
	assert.Equal(t, batSpotStepSamples, info.Spec.StepSamples)
}

func TestBatSpotLabels(t *testing.T) {
	t.Parallel()
	data := readBatSpotFixture(t, "batspot_labels.txt")
	species, err := parseBatSpotLabels(data)
	require.NoError(t, err)
	require.Len(t, species, batSpotSpeciesCount)
	assert.Equal(t, 1, species[0].column)
	assert.Equal(t, 14, species[len(species)-1].column)
	for _, invalid := range []string{
		strings.Replace(string(data), "\nnoise\n", "\n", 1),
		strings.Replace(string(data), "\nnoise\n", "\nBackground\n", 1),
		strings.Replace(string(data), "Eptesicus serotinus_Serotine", "B", 1),
	} {
		_, err := parseBatSpotLabels([]byte(invalid))
		require.Error(t, err)
	}
}

type batSpotTestRunner struct {
	batches []int
}

func (r *batSpotTestRunner) Run(input, output []float32) error {
	batch := len(input) / batSpotInputSize
	r.batches = append(r.batches, batch)
	clear(output)
	for i := range batch {
		// The non-species logit suppresses otherwise high species scores.
		output[i*batSpotClassCount+8] = 10
		output[i*batSpotClassCount+1] = 7
	}
	// Make another species peak in the last row to test max aggregation.
	output[(batch-1)*batSpotClassCount+14] = 13
	return nil
}
func (r *batSpotTestRunner) Close() error { return nil }

func TestBatSpotPredict(t *testing.T) {
	t.Parallel()
	species, err := parseBatSpotLabels(readBatSpotFixture(t, "batspot_labels.txt"))
	require.NoError(t, err)
	frontend, err := newBatSpotFrontend()
	require.NoError(t, err)
	runner := &batSpotTestRunner{}
	model := &batSpot{
		model:    runner,
		frontend: frontend,
		species:  species,
		input:    make([]float32, batSpotMaxCalls*batSpotInputSize),
		logits:   make([]float32, batSpotMaxCalls*batSpotClassCount),
	}

	pcm := make([]float32, model.Spec().ClipSampleCount())
	// The first silent clip (the orchestrator warm-up) scores one zero row.
	silent, err := model.Predict(t.Context(), [][]float32{pcm})
	require.NoError(t, err)
	assert.Empty(t, silent)
	assert.Equal(t, []int{1}, runner.batches)
	runner.batches = nil
	// Later silent clips never reach the model.
	silent, err = model.Predict(t.Context(), [][]float32{pcm})
	require.NoError(t, err)
	assert.Empty(t, silent)
	assert.Empty(t, runner.batches)

	// A loud pulse at the start activates exactly one candidate.
	const oneCallAmplitude = 0.25
	pcm[batSpotHopSamples/2] = oneCallAmplitude
	single, err := model.Predict(t.Context(), [][]float32{pcm})
	require.NoError(t, err)
	require.NotEmpty(t, single)
	assert.Equal(t, []int{1}, runner.batches)
	runner.batches = nil

	// Separate pulses provide more eligible, non-overlapping calls than the cap.
	const pulseSpacing = 4 * batSpotCallSamples
	for i := batSpotHopSamples / 2; i < len(pcm); i += pulseSpacing {
		pcm[i] = oneCallAmplitude
	}
	results, err := model.Predict(t.Context(), [][]float32{pcm})
	require.NoError(t, err)
	require.Len(t, results, batSpotSpeciesCount)
	assert.Equal(t, species[len(species)-1].label, results[0].Species)
	assert.Greater(t, results[0].Confidence, float32(0.9))
	got := make(map[string]float32, len(results))
	for _, result := range results {
		got[result.Species] = result.Confidence
	}
	assert.InDelta(t, 0.0474, got[species[0].label], 0.0001)
	assert.Len(t, got, len(model.Labels()))
	for _, label := range model.Labels() {
		assert.Contains(t, got, label)
	}
	assert.Equal(t, []int{batSpotMaxCalls}, runner.batches)
}
