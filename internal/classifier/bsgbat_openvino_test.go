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

func TestBSGBatORTFallbackFunctional(t *testing.T) {
	testBatORTFallback(t, bsgBatTestDataEnv, bsgBatRawSampleRate, func(t *testing.T, dir, backend, library string) ModelInstance {
		t.Helper()
		cfg := BSGBatModelConfig{
			ModelPath:       filepath.Join(dir, "bsgbat_v0.21_r3.onnx"),
			LabelPath:       filepath.Join(dir, "bsgbat_labels.txt"),
			ONNXRuntimePath: os.Getenv("ONNX_RUNTIME_PATH"), Threads: 1,
			Backend: backend, OpenVINODevice: conf.OVDeviceCPU,
			OpenVINOPath: library,
		}
		model, err := NewBSGBat(&cfg)
		require.NoError(t, err)
		assert.Nil(t, model.ovClassifier)
		require.NotNil(t, model.session)
		return model
	})
}

func TestTryBSGBatOpenVINOFallsBack(t *testing.T) {
	for _, backend := range []string{conf.BackendPrefONNX, conf.BackendPrefAuto, conf.BackendPrefOpenVINO} {
		t.Run(backend, func(t *testing.T) {
			if openvinoBackendAvailable && backend != conf.BackendPrefONNX {
				t.Skip("unavailable-backend gate is covered by builds without the openvino tag")
			}
			classifier, device, precision, ok := tryBSGBatOpenVINO(&BSGBatModelConfig{
				Backend: backend, OpenVINODevice: conf.OVDeviceCPU,
			}, []string{bsgBatBackgroundLabel})
			assert.False(t, ok)
			assert.Nil(t, classifier)
			assert.Empty(t, device)
			assert.Empty(t, precision)
		})
	}
}

func TestTryBSGBatOpenVINOFailureFallsBack(t *testing.T) {
	// Missing library/model paths exercise init or compile failure on an OpenVINO
	// build and the not-built gate on a normal build, without requiring a runtime.
	missing := filepath.Join(t.TempDir(), "missing")
	classifier, device, precision, ok := tryBSGBatOpenVINO(&BSGBatModelConfig{
		Backend: conf.BackendPrefOpenVINO, OpenVINODevice: conf.OVDeviceCPU,
		OpenVINOPath: missing, ModelPath: missing,
	}, []string{bsgBatBackgroundLabel})
	assert.False(t, ok)
	assert.Nil(t, classifier)
	assert.Empty(t, device)
	assert.Empty(t, precision)
}

func TestBSGBatRuntimeInfoAndClose(t *testing.T) {
	t.Parallel()
	testBatRuntimeInfoAndClose(t, bsgBatRawSampleRate, func(backend string) ModelInstance {
		return &BSGBat{
			batORTBase: &batORTBase{info: ModelInfo{Backend: backend}, modelPath: "bat-fp32.onnx"},
			device:     inference.OVDeviceGPU, precision: inference.OVPrecisionF32,
		}
	})
}

func TestBSGBatBackendSettingsReload(t *testing.T) {
	testBatBackendSettingsReload(t, RegistryIDBSGBat, func(t *testing.T, settings *conf.Settings, threads int) {
		t.Helper()
		cfg := bsgBatConfigFor(settings, "bsgbat.onnx", "labels.txt", threads)
		assert.Equal(t, settings.BirdNET.Backend, cfg.Backend)
		assert.Equal(t, settings.BirdNET.OpenVINODevice, cfg.OpenVINODevice)
		assert.Equal(t, settings.BirdNET.OpenVINOPath, cfg.OpenVINOPath)
		assert.Equal(t, settings.BirdNET.ONNXRuntimePath, cfg.ONNXRuntimePath)
		assert.Equal(t, settings.BirdNET.Threads, cfg.Threads)
	})
}
