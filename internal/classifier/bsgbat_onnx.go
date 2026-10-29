package classifier

import (
	"context"
	"time"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/inference"
	inferenceonnx "github.com/tphakala/birdnet-go/internal/inference/onnx"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	bsgBatInputName        = "spectrogram"
	bsgBatOutputName       = "logits"
	bsgBatOutputClasses    = 22
	bsgBatBackgroundLabel  = "Background"
	errTypeBatInvokeFailed = "invoke_failed"
	errTypeBatNonFinite    = "non_finite_logits"
	errTypeBatInvalidInput = "invalid_input"
)

// BSGBatModelConfig configures a BSG-BAT model instance.
type BSGBatModelConfig struct {
	// ModelPath is the ONNX classifier model path.
	ModelPath string
	// LabelPath is the species label file path.
	LabelPath string
	// ONNXRuntimePath optionally selects the ONNX Runtime library.
	ONNXRuntimePath string
	// Threads sets the intra-operation CPU thread budget.
	Threads int
}

// BSGBat runs the BSG-BAT European species classifier.
type BSGBat struct {
	*batORTBase
	dsp           *bsgBatDSP
	input, output *ort.Tensor[float32]
}

// NewBSGBat creates a BSG-BAT model instance.
func NewBSGBat(cfg *BSGBatModelConfig) (*BSGBat, error) {
	if cfg == nil {
		return nil, errors.Newf("BSG-BAT configuration is required").Component("classifier").Context("operation", "initialize_bsgbat").Context("model", RegistryIDBSGBat).Category(errors.CategoryValidation).Build()
	}
	if err := inference.InitONNXRuntime(cfg.ONNXRuntimePath); err != nil {
		return nil, errors.New(err).Component("classifier").Context("operation", "initialize_bsgbat").Context("model", RegistryIDBSGBat).Category(errors.CategoryModelInit).Build()
	}
	rawLabels, err := inferenceonnx.LoadLabels(cfg.LabelPath)
	if err != nil {
		return nil, errors.New(err).Component("classifier").Context("operation", "validate_bsgbat_labels").Context("model", RegistryIDBSGBat).Category(errors.CategoryModelInit).Build()
	}
	if len(rawLabels) != bsgBatOutputClasses {
		return nil, errors.Newf("BSG-BAT label count is %d, want %d", len(rawLabels), bsgBatOutputClasses).Component("classifier").Context("operation", "validate_bsgbat_labels").Context("model", RegistryIDBSGBat).Category(errors.CategoryValidation).Build()
	}
	labels := make([]string, 0, bsgBatOutputClasses-1)
	for i, name := range rawLabels {
		if name == bsgBatBackgroundLabel {
			if i != bsgBatOutputClasses-1 {
				return nil, errors.Newf("BSG-BAT background label must be last").Component("classifier").Context("operation", "validate_bsgbat_labels").Context("model", RegistryIDBSGBat).Category(errors.CategoryValidation).Build()
			}
			continue
		}
		common, ok := batCommonNames[name]
		if !ok {
			return nil, errors.Newf("unknown BSG-BAT scientific name %q", name).Component("classifier").Context("operation", "validate_bsgbat_labels").Context("model", RegistryIDBSGBat).Category(errors.CategoryValidation).Build()
		}
		labels = append(labels, name+"_"+common)
	}
	dsp, err := newBSGBatDSP()
	if err != nil {
		return nil, errors.New(err).Component("classifier").Context("operation", "validate_bsgbat_labels").Context("model", RegistryIDBSGBat).Category(errors.CategoryModelInit).Build()
	}
	session, err := newBatORTSession(RegistryIDBSGBat, cfg.ModelPath, []string{bsgBatInputName}, []string{bsgBatOutputName}, cfg.Threads)
	if err != nil {
		return nil, errors.New(err).Component("classifier").Context("operation", "initialize_bsgbat").Context("model", RegistryIDBSGBat).Category(errors.CategoryModelInit).Build()
	}
	input, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, bsgBatFrames, bsgBatMelBands))
	if err != nil {
		_ = session.Destroy()
		return nil, errors.New(err).Component("classifier").Category(errors.CategoryModelInit).Context("operation", "initialize_bsgbat").Context("model", RegistryIDBSGBat).Build()
	}
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, bsgBatOutputClasses))
	if err != nil {
		_ = input.Destroy()
		_ = session.Destroy()
		return nil, errors.New(err).Component("classifier").Category(errors.CategoryModelInit).Context("operation", "initialize_bsgbat").Context("model", RegistryIDBSGBat).Build()
	}
	info := ModelRegistry[RegistryIDBSGBat]
	info.NumSpecies = len(labels)
	base := &batORTBase{session: session, labels: labels, info: info, modelPath: cfg.ModelPath, values: []ort.Value{input, output}}
	return &BSGBat{batORTBase: base, dsp: dsp, input: input, output: output}, nil
}

func (b *BSGBat) runLogits(samples []float32) ([]float32, error) {
	data, err := b.dsp.preprocess(samples)
	if err != nil {
		return nil, err
	}
	copy(b.input.GetData(), data)
	if err := b.session.Run([]ort.Value{b.input}, []ort.Value{b.output}); err != nil {
		return nil, err
	}
	return b.output.GetData(), nil
}

// Predict classifies one second of ultrasonic PCM.
func (b *BSGBat) Predict(ctx context.Context, samples [][]float32) ([]datastore.Results, error) {
	span, _ := startPredictSpan(ctx, RegistryIDBSGBat, samples)
	defer span.Finish()
	start := time.Now()
	if len(samples) == 0 || len(samples[0]) == 0 {
		span.markErrored(errTypeEmptySample)
		return nil, errors.Newf("empty audio sample").Component("classifier").Context("operation", "predict").Context("model", RegistryIDBSGBat).Category(errors.CategoryValidation).Build()
	}
	if len(samples[0]) != bsgBatRawSampleRate {
		span.markErrored(errTypeBatInvalidInput)
		return nil, errors.Newf("BSG-BAT requires %d samples, got %d", bsgBatRawSampleRate, len(samples[0])).Component("classifier").Context("operation", "predict").Context("model", RegistryIDBSGBat).Category(errors.CategoryValidation).Build()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		span.markErrored(errTypeClassifierNil)
		return nil, errors.Newf("BSG-BAT classifier is closed").Component("classifier").Context("operation", "predict").Context("model", RegistryIDBSGBat).Category(errors.CategoryModelInit).Build()
	}
	padded := make([]float32, bsgBatInputSamples)
	copy(padded, samples[0])
	for i := len(samples[0]); i < len(padded); i++ {
		padded[i] = samples[0][2*len(samples[0])-i-2]
	}
	logits, err := b.runLogits(padded)
	if err != nil {
		err = errors.New(err).Component("classifier").Context("operation", "predict").Context("model", RegistryIDBSGBat).Category(errors.CategoryAudio).Build()
		recordPredictionFailure(span, RegistryIDBSGBat, errTypeBatInvokeFailed, start, err)
		return nil, err
	}
	if idx := firstNonFinite(logits); idx != noNonFiniteScore {
		err = newNonFiniteScoreError(nonFiniteScore{modelID: RegistryIDBSGBat, index: idx, count: len(logits)}, b.RuntimeInfo)
		recordPredictionFailure(span, RegistryIDBSGBat, errTypeBatNonFinite, start, err)
		return nil, err
	}
	threshold := float32(conf.Setting().Bat.Threshold)
	results := make([]datastore.Results, 0, len(b.labels))
	for i, label := range b.labels {
		p := float32(customSigmoid(float64(logits[i]), 1))
		if p >= threshold {
			results = append(results, datastore.Results{Species: label, Confidence: p})
		}
	}
	results = getTopKResults(results, defaultTopKResults)
	recordPredictionSuccess(span, len(results), start)
	return results, nil
}
