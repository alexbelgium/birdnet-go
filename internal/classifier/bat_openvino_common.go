package classifier

import (
	"github.com/tphakala/birdnet-go/internal/inference"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// prepareBatOpenVINO shares the host/device gate and runtime initialization for
// the spectrogram bat models. Any failure leaves the caller free to load ORT.
func prepareBatOpenVINO(backend, device, modelID, libraryPath string, outputIndex int) (openVINOPlan, bool) {
	plan, ok, reason := openVINOPlanFor(backend, device, modelID, libraryPath, outputIndex)
	if !ok {
		logOpenVINODeclined(modelID, backend, reason)
		return plan, false
	}
	// Keep these graphs at f32 until parity is validated on genuine f16 hardware.
	// x86 CPUs may promote an f16 hint to f32, so CPU parity cannot validate f16.
	plan.precision = inference.OVPrecisionF32
	if err := inference.InitOpenVINO(libraryPath); err != nil {
		GetLogger().Warn("Bat model OpenVINO init failed; using ONNX Runtime",
			logger.String("model", modelID), logger.Error(err))
		return plan, false
	}
	return plan, true
}
