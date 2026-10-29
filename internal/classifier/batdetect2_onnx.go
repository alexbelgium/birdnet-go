package classifier

import (
	"context"
	"sort"
	"time"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/inference"
	inferenceonnx "github.com/tphakala/birdnet-go/internal/inference/onnx"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	batDetect2DetectionThreshold float32 = 0.01
	batDetect2ClassThreshold     float32 = 0.1
	batDetect2TopKPerSecond              = 100
	batDetect2ExpectedSamples            = 144000
	batDetect2InputName                  = "input"
	batDetect2DetectionOutput            = "detection_probs"
	batDetect2ClassOutput                = "class_probs"
)

var batDetect2CodeLabels = map[string]string{
	"MYOMYS": "Myotis mystacinus", "MYOALC": "Myotis alcathoe", "CNESER": "Eptesicus serotinus",
	"PIPNAT": "Pipistrellus nathusii", "BARBAR": "Barbastella barbastellus", "MYONAT": "Myotis nattereri",
	"MYODAU": "Myotis daubentonii", "MYOBRA": "Myotis brandtii", "PIPPIP": "Pipistrellus pipistrellus",
	"MYOBEC": "Myotis bechsteinii", "PIPPYG": "Pipistrellus pygmaeus", "RHIHIP": "Rhinolophus hipposideros",
	"NYCLEI": "Nyctalus leisleri", "RHIFER": "Rhinolophus ferrumequinum", "PLEAUR": "Plecotus auritus",
	"NYCNOC": "Nyctalus noctula", "PLEAUS": "Plecotus austriacus",
}

// BatDetect2ModelConfig configures a BatDetect2 model instance.
type BatDetect2ModelConfig struct {
	// ModelPath is the ONNX classifier model path.
	ModelPath string
	// LabelPath is the species label file path.
	LabelPath string
	// ONNXRuntimePath optionally selects the ONNX Runtime library.
	ONNXRuntimePath string
	// Threads sets the intra-operation CPU thread budget.
	Threads int
}

// BatDetect2 runs the BatDetect2 spectrogram detector and species classifier.
type BatDetect2 struct {
	*batORTBase
	dsp                              *batDetect2DSP
	input, detection, classes        *ort.Tensor[float32]
	tile, fullDetection, fullClasses []float32
}

// NewBatDetect2 creates a BatDetect2 instance.
func NewBatDetect2(cfg *BatDetect2ModelConfig) (*BatDetect2, error) {
	if cfg == nil {
		return nil, errors.Newf("BatDetect2 configuration is required").Component("classifier").Context("operation", "initialize_batdetect2").Context("model", RegistryIDBatDetect2).Category(errors.CategoryValidation).Build()
	}
	if err := inference.InitONNXRuntime(cfg.ONNXRuntimePath); err != nil {
		return nil, errors.New(err).Component("classifier").Context("operation", "initialize_batdetect2").Context("model", RegistryIDBatDetect2).Category(errors.CategoryModelInit).Build()
	}
	rawLabels, err := inferenceonnx.LoadLabels(cfg.LabelPath)
	if err != nil {
		return nil, errors.New(err).Component("classifier").Context("operation", "validate_batdetect2_labels").Context("model", RegistryIDBatDetect2).Category(errors.CategoryModelInit).Build()
	}
	if len(rawLabels) != len(batDetect2CodeLabels) {
		return nil, errors.Newf("BatDetect2 label count is %d, want %d", len(rawLabels), len(batDetect2CodeLabels)).Component("classifier").Context("operation", "validate_batdetect2_labels").Context("model", RegistryIDBatDetect2).Category(errors.CategoryValidation).Build()
	}
	labels := make([]string, len(rawLabels))
	seenCodes := make(map[string]bool, len(rawLabels))
	for i, code := range rawLabels {
		if seenCodes[code] {
			return nil, errors.Newf("duplicate BatDetect2 label code %q", code).Component("classifier").Context("operation", "validate_batdetect2_labels").Context("model", RegistryIDBatDetect2).Category(errors.CategoryValidation).Build()
		}
		seenCodes[code] = true
		scientificName, ok := batDetect2CodeLabels[code]
		if !ok {
			return nil, errors.Newf("unknown BatDetect2 label code %q", code).Component("classifier").Context("operation", "validate_batdetect2_labels").Context("model", RegistryIDBatDetect2).Category(errors.CategoryValidation).Build()
		}
		commonName, ok := batCommonNames[scientificName]
		if !ok {
			return nil, errors.Newf("unknown bat scientific name %q", scientificName).Component("classifier").Context("operation", "validate_batdetect2_labels").Context("model", RegistryIDBatDetect2).Category(errors.CategoryValidation).Build()
		}
		labels[i] = scientificName + "_" + commonName
	}
	session, err := newBatORTSession(RegistryIDBatDetect2, cfg.ModelPath, []string{batDetect2InputName}, []string{batDetect2DetectionOutput, batDetect2ClassOutput}, cfg.Threads)
	if err != nil {
		return nil, errors.New(err).Component("classifier").Context("operation", "initialize_batdetect2").Context("model", RegistryIDBatDetect2).Category(errors.CategoryModelInit).Context("model_path", cfg.ModelPath).Build()
	}
	info := ModelRegistry[RegistryIDBatDetect2]
	info.NumSpecies = len(labels)
	dsp, err := newBatDetect2DSP(batDetect2ExpectedSamples)
	if err != nil {
		_ = session.Destroy()
		return nil, errors.New(err).Component("classifier").Category(errors.CategoryModelInit).Context("operation", "initialize_batdetect2").Context("model", RegistryIDBatDetect2).Build()
	}
	input, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, batDetect2ImageHeight, batDetect2TileWidth))
	if err != nil {
		_ = session.Destroy()
		return nil, errors.New(err).Component("classifier").Category(errors.CategoryModelInit).Context("operation", "initialize_batdetect2").Context("model", RegistryIDBatDetect2).Build()
	}
	detection, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, batDetect2ImageHeight, batDetect2TileWidth))
	if err != nil {
		_ = input.Destroy()
		_ = session.Destroy()
		return nil, errors.New(err).Component("classifier").Category(errors.CategoryModelInit).Context("operation", "initialize_batdetect2").Context("model", RegistryIDBatDetect2).Build()
	}
	classes, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(len(labels)), batDetect2ImageHeight, batDetect2TileWidth))
	if err != nil {
		_ = detection.Destroy()
		_ = input.Destroy()
		_ = session.Destroy()
		return nil, errors.New(err).Component("classifier").Category(errors.CategoryModelInit).Context("operation", "initialize_batdetect2").Context("model", RegistryIDBatDetect2).Build()
	}
	base := &batORTBase{session: session, labels: labels, info: info, modelPath: cfg.ModelPath, values: []ort.Value{input, detection, classes}}
	return &BatDetect2{batORTBase: base, dsp: dsp, input: input, detection: detection, classes: classes, tile: make([]float32, batDetect2ImageHeight*batDetect2TileWidth), fullDetection: make([]float32, len(dsp.image)), fullClasses: make([]float32, len(labels)*len(dsp.image))}, nil
}

func (b *BatDetect2) runTile(tile []float32) (detection, classProbabilities []float32, err error) {
	copy(b.input.GetData(), tile)
	if err := b.session.Run([]ort.Value{b.input}, []ort.Value{b.detection, b.classes}); err != nil {
		return nil, nil, err
	}
	return b.detection.GetData(), b.classes.GetData(), nil
}

// Predict implements ModelInstance.
func (b *BatDetect2) Predict(ctx context.Context, samples [][]float32) ([]datastore.Results, error) {
	span, _ := startPredictSpan(ctx, RegistryIDBatDetect2, samples)
	defer span.Finish()
	startTime := time.Now()
	if len(samples) == 0 || len(samples[0]) == 0 {
		span.markErrored(errTypeEmptySample)
		return nil, errors.Newf("empty audio sample").Component("classifier").Context("operation", "predict").Context("model", RegistryIDBatDetect2).Category(errors.CategoryValidation).Build()
	}
	if len(samples[0]) != batDetect2ExpectedSamples {
		span.markErrored(errTypeBatInvalidInput)
		return nil, errors.Newf("BatDetect2 requires %d samples, got %d", batDetect2ExpectedSamples, len(samples[0])).Component("classifier").Context("operation", "predict").Context("model", RegistryIDBatDetect2).Category(errors.CategoryValidation).Build()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		span.markErrored(errTypeClassifierNil)
		return nil, errors.Newf("BatDetect2 classifier is closed").Component("classifier").Context("operation", "predict").Context("model", RegistryIDBatDetect2).Category(errors.CategoryModelInit).Build()
	}
	image, width := b.dsp.preprocess(samples[0])
	covered := 0
	tilePlane := batDetect2ImageHeight * batDetect2TileWidth
	fullPlane := batDetect2ImageHeight * width
	for covered < width {
		start := covered
		if width >= batDetect2TileWidth {
			start = min(start, width-batDetect2TileWidth)
		}
		copyWidth := min(batDetect2TileWidth, width-start)
		clear(b.tile)
		for y := range batDetect2ImageHeight {
			copy(b.tile[y*batDetect2TileWidth:y*batDetect2TileWidth+copyWidth], image[y*width+start:y*width+start+copyWidth])
		}
		det, classes, err := b.runTile(b.tile)
		if err != nil {
			err = errors.New(err).Component("classifier").Context("operation", "predict").Context("model", RegistryIDBatDetect2).Category(errors.CategoryAudio).Build()
			recordPredictionFailure(span, RegistryIDBatDetect2, errTypeBatInvokeFailed, startTime, err)
			return nil, err
		}
		for y := range batDetect2ImageHeight {
			copy(b.fullDetection[y*width+covered:y*width+start+copyWidth], det[y*batDetect2TileWidth+covered-start:y*batDetect2TileWidth+copyWidth])
			for c := range b.labels {
				copy(b.fullClasses[c*fullPlane+y*width+covered:c*fullPlane+y*width+start+copyWidth], classes[c*tilePlane+y*batDetect2TileWidth+covered-start:c*tilePlane+y*batDetect2TileWidth+copyWidth])
			}
		}
		covered = start + copyWidth
	}
	peaks := batDetect2NMS(b.fullDetection, batDetect2ImageHeight, width, width)
	sort.SliceStable(peaks, func(i, j int) bool { return peaks[i].score > peaks[j].score })
	limit := width * batDetect2TopKPerSecond / batDetect2OutputFrameRate
	peaks = peaks[:min(len(peaks), limit)]
	maxScores := make([]float32, len(b.labels))
	for _, peak := range peaks {
		for c := range b.labels {
			score := b.fullClasses[c*fullPlane+peak.y*width+peak.x]
			if score >= batDetect2ClassThreshold {
				maxScores[c] = max(maxScores[c], score)
			}
		}
	}
	threshold := float32(conf.Setting().Bat.Threshold)
	results := make([]datastore.Results, 0, len(b.labels))
	for i, score := range maxScores {
		if score >= threshold && score >= batDetect2ClassThreshold {
			results = append(results, datastore.Results{Species: b.labels[i], Confidence: score})
		}
	}
	results = getTopKResults(results, defaultTopKResults)
	recordPredictionSuccess(span, len(results), startTime)
	return results, nil
}
