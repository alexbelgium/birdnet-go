package classifier

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/inference"
)

func TestBatDetect2ORTFallbackFunctional(t *testing.T) {
	testBatORTFallback(t, batDetect2TestDataEnv, batDetect2ExpectedSamples, func(t *testing.T, dir, backend, library string) ModelInstance {
		t.Helper()
		cfg := BatDetect2ModelConfig{
			ModelPath:       filepath.Join(dir, "batdetect2-uk-same.onnx"),
			LabelPath:       filepath.Join(dir, "labels.json"),
			ONNXRuntimePath: os.Getenv("ONNX_RUNTIME_PATH"), Threads: 1,
			Backend: backend, OpenVINODevice: conf.OVDeviceCPU,
			OpenVINOPath: library,
		}
		model, err := NewBatDetect2(&cfg)
		require.NoError(t, err)
		assert.Nil(t, model.ovClassifier)
		require.NotNil(t, model.session)
		return model
	})
}

func TestTryBatDetect2OpenVINOFailureFallsBack(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, preference := range []string{conf.BackendPrefONNX, conf.BackendPrefOpenVINO} {
		t.Run(preference, func(t *testing.T) {
			classifier, device, precision, ok := tryBatDetect2OpenVINO(&BatDetect2ModelConfig{
				Backend: preference, OpenVINODevice: conf.OVDeviceCPU,
				OpenVINOPath: missing, ModelPath: missing,
			}, len(batDetect2CodeLabels))
			assert.False(t, ok)
			assert.Nil(t, classifier)
			assert.Empty(t, device)
			assert.Empty(t, precision)
		})
	}
}

func TestBatDetect2RuntimeInfoAndClose(t *testing.T) {
	t.Parallel()
	testBatRuntimeInfoAndClose(t, batDetect2ExpectedSamples, func(backend string) ModelInstance {
		return &BatDetect2{
			batORTBase: &batORTBase{info: ModelInfo{Backend: backend}, modelPath: "bat-fp32.onnx"},
			device:     inference.OVDeviceGPU, precision: inference.OVPrecisionF32,
		}
	})
}

func TestBatDetect2BackendSettingsReload(t *testing.T) {
	testBatBackendSettingsReload(t, RegistryIDBatDetect2, func(t *testing.T, settings *conf.Settings, threads int) {
		t.Helper()
		cfg := batDetect2ConfigFor(settings, "batdetect2.onnx", "labels.txt", threads)
		assert.Equal(t, settings.BirdNET.Backend, cfg.Backend)
		assert.Equal(t, settings.BirdNET.OpenVINODevice, cfg.OpenVINODevice)
		assert.Equal(t, settings.BirdNET.OpenVINOPath, cfg.OpenVINOPath)
		assert.Equal(t, settings.BirdNET.ONNXRuntimePath, cfg.ONNXRuntimePath)
		assert.Equal(t, settings.BirdNET.Threads, cfg.Threads)
	})
}
