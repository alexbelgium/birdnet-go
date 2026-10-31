package classifier

import (
	"time"

	"github.com/tphakala/birdnet-go/internal/inference"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// Output port order of the legojoey17 BatDetect2 export used by the catalog.
const (
	batDetect2DetectionOutputIndex = 0
	batDetect2ClassOutputIndex     = 1
)

func tryBatDetect2OpenVINO(cfg *BatDetect2ModelConfig, species int) (classifier inference.MultiOutputClassifier, device, precision string, ok bool) {
	plan, ok := prepareBatOpenVINO(cfg.Backend, cfg.OpenVINODevice, RegistryIDBatDetect2, cfg.OpenVINOPath, batDetect2DetectionOutputIndex)
	if !ok {
		return nil, "", "", false
	}
	start := time.Now()
	const plane = batDetect2ImageHeight * batDetect2TileWidth
	classifier, err := inference.NewOpenVINOMultiOutputClassifier(cfg.ModelPath, &inference.OpenVINOMultiOutputOptions{
		Threads: cfg.Threads, Device: plan.device, PrecisionHint: plan.precision,
		OutputIndices: []int{batDetect2DetectionOutputIndex, batDetect2ClassOutputIndex},
		OutputSizes:   []int{plane, species * plane},
	})
	if err == nil {
		// Validate the fixed tile input and first inference before selecting OV.
		// DSP, stitching, NMS, and species scoring remain shared with the ORT path.
		_, err = classifier.PredictOutputs(make([]float32, plane))
		if err != nil {
			classifier.Close()
		}
	}
	if err != nil {
		GetLogger().Warn("BatDetect2 OpenVINO classifier init failed; using ONNX Runtime",
			logger.String("device", plan.device), logger.Error(err))
		return nil, "", "", false
	}
	GetLogger().Info("BatDetect2 model using OpenVINO backend",
		logger.String("device", plan.device),
		logger.String("precision", openVINOPrecisionLabel(plan.precision)),
		logger.String("init_time", time.Since(start).String()))
	return classifier, plan.device, plan.precision, true
}

// RuntimeInfo reports the active backend, device, and compute precision.
func (b *BatDetect2) RuntimeInfo() (device, backend, precision string) {
	if b.info.Backend == BackendOpenVINO {
		return b.device, BackendOpenVINO, openVINOEffectivePrecision(b.precision)
	}
	return b.batORTBase.RuntimeInfo()
}

// Close releases the active backend under the prediction lock. It is idempotent.
func (b *BatDetect2) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ovClassifier != nil {
		b.ovClassifier.Close()
		b.ovClassifier = nil
	}
	return b.closeLocked()
}
