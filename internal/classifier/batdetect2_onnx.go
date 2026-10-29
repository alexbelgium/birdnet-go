package classifier

import (
	"context"
	"math"
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
	ModelPath, LabelPath, ONNXRuntimePath string
	Threads                               int
}

// BatDetect2 runs the BatDetect2 spectrogram detector and species classifier.
type BatDetect2 struct {
	*batORTBase
	dsp                       *batDetect2DSP
	input, detection, classes *ort.Tensor[float32]
}

// NewBatDetect2 creates a BatDetect2 instance.
func NewBatDetect2(cfg *BatDetect2ModelConfig) (*BatDetect2, error) {
	if cfg == nil {
		return nil, errors.Newf("BatDetect2 configuration is required").Category(errors.CategoryValidation).Build()
	}
	if err := inference.InitONNXRuntime(cfg.ONNXRuntimePath); err != nil {
		return nil, errors.New(err).Category(errors.CategoryModelInit).Build()
	}
	rawLabels, err := inferenceonnx.LoadLabels(cfg.LabelPath)
	if err != nil {
		return nil, errors.New(err).Category(errors.CategoryModelInit).Build()
	}
	if len(rawLabels) != len(batDetect2CodeLabels) {
		return nil, errors.Newf("BatDetect2 label count is %d, want %d", len(rawLabels), len(batDetect2CodeLabels)).Category(errors.CategoryValidation).Build()
	}
	labels := make([]string, len(rawLabels))
	seenCodes := make(map[string]bool, len(rawLabels))
	for i, code := range rawLabels {
		if seenCodes[code] {
			return nil, errors.Newf("duplicate BatDetect2 label code %q", code).Category(errors.CategoryValidation).Build()
		}
		seenCodes[code] = true
		scientificName, ok := batDetect2CodeLabels[code]
		if !ok {
			return nil, errors.Newf("unknown BatDetect2 label code %q", code).Category(errors.CategoryValidation).Build()
		}
		commonName, ok := batCommonNames[scientificName]
		if !ok {
			return nil, errors.Newf("unknown bat scientific name %q", scientificName).Category(errors.CategoryValidation).Build()
		}
		labels[i] = scientificName + "_" + commonName
	}
	session, err := newBatORTSession(cfg.ModelPath, []string{batDetect2InputName}, []string{batDetect2DetectionOutput, batDetect2ClassOutput}, cfg.Threads)
	if err != nil {
		return nil, errors.New(err).Category(errors.CategoryModelInit).Context("model_path", cfg.ModelPath).Build()
	}
	info := ModelRegistry[RegistryIDBatDetect2]
	info.NumSpecies = len(labels)
	dsp, err := newBatDetect2DSP(batDetect2ExpectedSamples)
	if err != nil {
		_ = session.Destroy()
		return nil, err
	}
	input, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, batDetect2ImageHeight, batDetect2TileWidth))
	if err != nil {
		_ = session.Destroy()
		return nil, err
	}
	detection, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, batDetect2ImageHeight, batDetect2TileWidth))
	if err != nil {
		_ = input.Destroy()
		_ = session.Destroy()
		return nil, err
	}
	classes, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(len(labels)), batDetect2ImageHeight, batDetect2TileWidth))
	if err != nil {
		_ = detection.Destroy()
		_ = input.Destroy()
		_ = session.Destroy()
		return nil, err
	}
	base := &batORTBase{session: session, labels: labels, info: info, modelPath: cfg.ModelPath, values: []ort.Value{input, detection, classes}}
	return &BatDetect2{batORTBase: base, dsp: dsp, input: input, detection: detection, classes: classes}, nil
}

type batDetect2Candidate struct {
	score       float32
	classScores []float32
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
		return nil, errors.Newf("empty audio sample").Category(errors.CategoryValidation).Build()
	}
	if len(samples[0]) != batDetect2ExpectedSamples {
		return nil, errors.Newf("BatDetect2 requires %d samples, got %d", batDetect2ExpectedSamples, len(samples[0])).Category(errors.CategoryValidation).Build()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session == nil {
		return nil, errors.Newf("BatDetect2 classifier is closed").Category(errors.CategoryModelInit).Build()
	}
	image, width := b.dsp.preprocess(samples[0])
	candidates := make([]batDetect2Candidate, 0)
	for start := 0; start < width; start += batDetect2TileWidth {
		tile := make([]float32, batDetect2ImageHeight*batDetect2TileWidth)
		copyWidth := min(batDetect2TileWidth, width-start)
		for y := range batDetect2ImageHeight {
			copy(tile[y*batDetect2TileWidth:y*batDetect2TileWidth+copyWidth], image[y*width+start:y*width+start+copyWidth])
		}
		det, classes, err := b.runTile(tile)
		if err != nil {
			return nil, errors.New(err).Category(errors.CategoryAudio).Context("model", RegistryIDBatDetect2).Build()
		}
		peaks := batDetect2NMS(det, batDetect2ImageHeight, batDetect2TileWidth, copyWidth)
		plane := batDetect2ImageHeight * batDetect2TileWidth
		for _, p := range peaks {
			classScores := make([]float32, len(b.labels))
			for c := range b.labels {
				classScores[c] = classes[c*plane+p.y*batDetect2TileWidth+p.x]
			}
			candidates = append(candidates, batDetect2Candidate{score: p.score, classScores: classScores})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	limit := max(1, int(math.Ceil(float64(width)*batDetect2TopKPerSecond/batDetect2OutputFrameRate)))
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	maxScores := make([]float32, len(b.labels))
	for _, candidate := range candidates {
		for c, score := range candidate.classScores {
			if score >= batDetect2ClassThreshold && score > maxScores[c] {
				maxScores[c] = score
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
