package classifier

import (
	"time"

	"github.com/tphakala/birdnet-go/internal/inference"
	"github.com/tphakala/birdnet-go/internal/logger"
)

const bsgBatLogitsOutputIndex = 0

// tryBSGBatOpenVINO uses the same host/device gate as the bat embedding model.
// Every initialization, compilation, or output validation failure is logged and
// swallowed so NewBSGBat can fall back to ONNX Runtime. Preprocessing and sigmoid
// stay in Go; the graph consumes a fixed [1,1,512,128] mel tensor and emits logits.
func tryBSGBatOpenVINO(cfg *BSGBatModelConfig, labels []string) (classifier inference.Classifier, device, precision string, ok bool) {
	plan, ok := prepareBatOpenVINO(cfg.Backend, cfg.OpenVINODevice, RegistryIDBSGBat, cfg.OpenVINOPath, bsgBatLogitsOutputIndex)
	if !ok {
		return nil, "", "", false
	}
	log := GetLogger()
	start := time.Now()
	classifier, err := inference.NewOpenVINOClassifier(cfg.ModelPath, inference.OpenVINOClassifierOptions{
		Labels: labels, Threads: cfg.Threads, Device: plan.device,
		OutputIndex: plan.outputIndex, PrecisionHint: plan.precision,
	})
	if err != nil {
		log.Warn("BSG-BAT OpenVINO classifier init failed; using ONNX Runtime",
			logger.String("device", plan.device), logger.Error(err))
		return nil, "", "", false
	}
	log.Info("BSG-BAT model using OpenVINO backend",
		logger.String("device", plan.device),
		logger.String("precision", openVINOPrecisionLabel(plan.precision)),
		logger.Int("outputs", classifier.NumSpecies()),
		logger.String("init_time", time.Since(start).String()))
	return classifier, plan.device, plan.precision, true
}

// RuntimeInfo reports the backend, device, and compute precision in use.
func (b *BSGBat) RuntimeInfo() (device, backend, precision string) {
	if b.info.Backend == BackendOpenVINO {
		return b.device, BackendOpenVINO, openVINOEffectivePrecision(b.precision)
	}
	return b.batORTBase.RuntimeInfo()
}

// Close releases the active backend under the prediction lock. It is idempotent.
func (b *BSGBat) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ovClassifier != nil {
		b.ovClassifier.Close()
		b.ovClassifier = nil
	}
	return b.closeLocked()
}
