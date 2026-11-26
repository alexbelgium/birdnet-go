package classifier

import (
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/inference"
	"github.com/tphakala/birdnet-go/internal/inference/onnx"
)

// RegistryIDBatSpot is the registry identifier for BatSpot m09.
const RegistryIDBatSpot = "BATSPOT"

const (
	batSpotVersion      = "m09"
	batSpotClassCount   = 15
	batSpotSpeciesCount = batSpotClassCount - 3
	// Cover every 10 ms candidate hop once, including windows that straddle a step boundary.
	batSpotStepSamples = batSpotSampleRate // 1 second
	batSpotClipSamples = batSpotStepSamples + batSpotCallSamples - batSpotHopSamples
	batSpotClipLength  = batSpotClipSamples * time.Second / batSpotSampleRate
	batSpotInputName   = "spectrogram"
	batSpotOutputName  = "logits"
)

func batSpotModelInfo() ModelInfo {
	return ModelInfo{
		ID:               RegistryIDBatSpot,
		Name:             detection.ModelNameBatSpot,
		DetectionName:    detection.ModelNameBatSpot,
		DetectionVersion: batSpotVersion,
		Backend:          BackendONNX,
		Quantization:     QuantizationFP32,
		Description:      "BatSpot m09 search-phase classifier for European bats (Smeele et al. 2026, CC BY 4.0)",
		NumSpecies:       batSpotSpeciesCount,
		ConfigAliases:    []string{conf.ModelIDBatSpot},
		Spec: ModelSpec{
			SampleRate:              batSpotSampleRate,
			RawSampleRate:           batSpotSampleRate,
			MinRawSampleRate:        batSpotSampleRate,
			RecommendedSampleRate:   batSpotSampleRate,
			ClipLength:              batSpotClipLength,
			ClipSamples:             batSpotClipSamples,
			StepSamples:             batSpotStepSamples,
			RequireContiguousWindow: true,
		},
		rangeFilterCompat: rangeFilterCompatNone,
		scheduleGated:     true,
		taxonGroup:        TaxonGroupBat,
	}
}

type batSpotRunner interface {
	Run(input, output []float32) error
	Close() error
}

type batSpotSpecies struct {
	label  string
	column int
}

// batSpot is the BatSpot m09 ONNX classifier. The mutex guards preprocessing
// scratch, batch buffers, inference and the session's lifetime.
type batSpot struct {
	mu        sync.Mutex
	model     batSpotRunner
	frontend  *batSpotFrontend
	species   []batSpotSpecies
	input     []float32
	logits    []float32
	modelPath string
	labelPath string
	// ran records a completed ONNX run. See predictClip.
	ran bool
}

const (
	batSpotBuzzColumn   = 0
	batSpotNoiseColumn  = 8
	batSpotSocialColumn = 13

	batSpotBuzzLabel   = "B"
	batSpotNoiseLabel  = "noise"
	batSpotSocialLabel = "s"
)

func batSpotNonSpecies(column int) string {
	switch column {
	case batSpotBuzzColumn:
		return batSpotBuzzLabel
	case batSpotNoiseColumn:
		return batSpotNoiseLabel
	case batSpotSocialColumn:
		return batSpotSocialLabel
	default:
		return ""
	}
}

func parseBatSpotLabels(data []byte) ([]batSpotSpecies, error) {
	labels := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(labels) != batSpotClassCount {
		return nil, fmt.Errorf("BatSpot label file has %d lines, expected %d", len(labels), batSpotClassCount)
	}
	species := make([]batSpotSpecies, 0, batSpotSpeciesCount)
	seen := make(map[string]bool, batSpotSpeciesCount)
	for i, line := range labels {
		label := strings.TrimSpace(line)
		if expected := batSpotNonSpecies(i); expected != "" {
			if label != expected {
				return nil, fmt.Errorf("BatSpot label %d must be %q", i+1, expected)
			}
			continue
		}
		switch label {
		case "", batSpotBuzzLabel, batSpotNoiseLabel, batSpotSocialLabel:
			return nil, fmt.Errorf("invalid BatSpot species label at line %d", i+1)
		}
		if seen[label] {
			return nil, fmt.Errorf("duplicate BatSpot species label at line %d", i+1)
		}
		seen[label] = true
		species = append(species, batSpotSpecies{label: label, column: i})
	}
	return species, nil
}

func newBatSpot(modelPath, labelPath, runtimePath string, threads int) (*batSpot, error) {
	data, err := os.ReadFile(labelPath)
	if err != nil {
		return nil, fmt.Errorf("read BatSpot labels: %w", err)
	}
	species, err := parseBatSpotLabels(data)
	if err != nil {
		return nil, err
	}
	frontend, err := newBatSpotFrontend()
	if err != nil {
		return nil, err
	}
	if err := inference.InitONNXRuntime(runtimePath); err != nil {
		return nil, err
	}
	dims := []int64{1, batSpotFrames, batSpotFrequencyBins}
	model, err := onnx.NewBatchedTensorModel(modelPath, batSpotInputName, batSpotOutputName, dims, batSpotClassCount, threads)
	if err != nil {
		return nil, err
	}
	return &batSpot{
		model:     model,
		frontend:  frontend,
		species:   species,
		input:     make([]float32, batSpotMaxCalls*batSpotInputSize),
		logits:    make([]float32, batSpotMaxCalls*batSpotClassCount),
		modelPath: modelPath,
		labelPath: labelPath,
	}, nil
}

// Predict gates 20 ms windows on a 10 ms grid and returns each species' maximum
// softmax probability over the selected windows. A clip uses at most four
// ONNX rows in one call (about 250 ms at the measured 63 ms per row).
func (b *batSpot) Predict(ctx context.Context, samples [][]float32) ([]datastore.Results, error) {
	span, _ := startPredictSpan(ctx, RegistryIDBatSpot, samples)
	defer span.Finish()
	start := time.Now()

	if len(samples) == 0 || len(samples[0]) != b.Spec().ClipSampleCount() {
		span.markErrored(errTypeEmptySample)
		return nil, fmt.Errorf("BatSpot requires a contiguous clip of %d samples", b.Spec().ClipSampleCount())
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.model == nil {
		span.markErrored(errTypeClassifierNil)
		return nil, errors.NewStd("BatSpot classifier is closed")
	}
	results, err := b.predictClip(ctx, samples[0])
	if err != nil {
		kind := errTypeInvokeFailed
		if errors.Is(err, ErrNonFiniteScore) {
			kind = errTypeNonFiniteLogits
		}
		recordPredictionFailure(span, RegistryIDBatSpot, kind, start, err)
		return nil, err
	}
	recordPredictionSuccess(span, len(results), start)
	return results, nil
}

func (b *batSpot) predictClip(ctx context.Context, pcm []float32) ([]datastore.Results, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selected := b.frontend.selectCalls(pcm)
	if len(selected) == 0 {
		if b.ran {
			return nil, nil
		}
		// The orchestrator warms up with a silent clip, which the gate drops.
		// Score one zero row the first time so session allocation and first-run
		// latency are paid at warm-up rather than on the first bat call.
		clear(b.input[:batSpotInputSize])
		var maxima [batSpotClassCount]float32
		return nil, b.scoreBatch(ctx, 1, &maxima)
	}

	for i, start := range selected {
		b.frontend.window(b.input[i*batSpotInputSize:][:batSpotInputSize], pcm[start:][:batSpotCallSamples])
	}
	var maxima [batSpotClassCount]float32
	if err := b.scoreBatch(ctx, len(selected), &maxima); err != nil {
		return nil, err
	}

	results := make([]datastore.Results, 0, len(b.species))
	for _, species := range b.species {
		results = append(results, datastore.Results{Species: species.label, Confidence: maxima[species.column]})
	}
	sortResults(results)
	return results, nil
}

func (b *batSpot) scoreBatch(ctx context.Context, batch int, maxima *[batSpotClassCount]float32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	logits := b.logits[:batch*batSpotClassCount]
	if err := b.model.Run(b.input[:batch*batSpotInputSize], logits); err != nil {
		return err
	}
	b.ran = true
	if idx := firstNonFinite(logits); idx != noNonFiniteScore {
		return newNonFiniteScoreError(nonFiniteScore{modelID: RegistryIDBatSpot, index: idx, count: len(logits)}, b.RuntimeInfo)
	}
	for row := range slices.Chunk(logits, batSpotClassCount) {
		probs := batSpotSoftmax(row)
		for _, species := range b.species {
			i := species.column
			maxima[i] = max(maxima[i], probs[i])
		}
	}
	return nil
}

// All classes contribute to the denominator, including buzz, noise and social calls.
func batSpotSoftmax(logits []float32) (probs [batSpotClassCount]float32) {
	peak := float64(slices.Max(logits))
	var exp [batSpotClassCount]float64
	var total float64
	for i, logit := range logits {
		exp[i] = math.Exp(float64(logit) - peak)
		total += exp[i]
	}
	for i := range exp {
		probs[i] = float32(exp[i] / total)
	}
	return probs
}

// Spec returns the contiguous, ultrasonic clip requirements.
func (b *batSpot) Spec() ModelSpec { return ModelRegistry[RegistryIDBatSpot].Spec }

// ModelID returns the registry identifier.
func (b *batSpot) ModelID() string { return RegistryIDBatSpot }

// ModelName returns the detection name.
func (b *batSpot) ModelName() string { return detection.ModelNameBatSpot }

// ModelVersion returns the checkpoint version.
func (b *batSpot) ModelVersion() string { return batSpotVersion }

// NumSpecies returns the number of species labels, excluding non-species classes.
func (b *batSpot) NumSpecies() int { return len(b.species) }

// Labels returns a copy of the species labels.
func (b *batSpot) Labels() []string {
	labels := make([]string, 0, len(b.species))
	for _, species := range b.species {
		labels = append(labels, species.label)
	}
	return labels
}

// RuntimeInfo reports the inference backend, device and precision.
func (b *batSpot) RuntimeInfo() (device, backend, precision string) {
	return deviceCPU, BackendONNX, string(QuantizationFP32)
}

// ResolvedModelPath returns the file backing the loaded instance.
func (b *batSpot) ResolvedModelPath() string { return b.modelPath }

// Close releases the ONNX session.
func (b *batSpot) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.model == nil {
		return nil
	}
	err := b.model.Close()
	b.model = nil
	return err
}
