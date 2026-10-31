package classifier

import (
	"github.com/tphakala/birdnet-go/internal/inference"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
)

func testBatBackendSettingsReload(t *testing.T, modelID string, checkConfig func(*testing.T, *conf.Settings, int)) {
	t.Helper()
	for _, change := range []string{"backend", "device", "library"} {
		t.Run(change, func(t *testing.T) {
			settings := conftest.GetTestSettings()
			settings.BirdNET.Backend = conf.BackendPrefONNX
			settings.BirdNET.OpenVINODevice = conf.OVDeviceCPU
			settings.BirdNET.Threads = 1
			oldKey := secondaryTripletFor(settings)
			switch change {
			case "backend":
				settings.BirdNET.Backend = conf.BackendPrefOpenVINO
			case "device":
				settings.BirdNET.OpenVINODevice = conf.OVDeviceGPU
			case "library":
				settings.BirdNET.OpenVINOPath = "/opt/openvino/libopenvino_c.so"
			}
			conftest.SetTestSettings(settings)
			t.Cleanup(func() { conftest.SetTestSettings(nil) })
			builder, ok := openvinoCapableSecondaryBuilders[modelID]
			require.True(t, ok)
			t.Cleanup(func() { openvinoCapableSecondaryBuilders[modelID] = builder })
			old := &reloadFakeModel{id: modelID}
			fresh := &reloadFakeModel{id: modelID}
			o := newTestOrchestrator(t)
			o.models[modelID] = &modelEntry{instance: old, backend: oldKey}
			builds := 0
			openvinoCapableSecondaryBuilders[modelID] = func(_ *Orchestrator, snapshot *conf.Settings, threads int) (ModelInstance, error) {
				builds++
				checkConfig(t, snapshot, threads)
				return fresh, nil
			}
			require.NoError(t, o.ReloadSecondaryModels())
			assert.Equal(t, 1, builds)
			assert.Same(t, ModelInstance(fresh), o.models[modelID].instance)
			assert.EqualValues(t, 1, old.closes.Load())
			require.NoError(t, o.ReloadSecondaryModels())
			assert.Equal(t, 1, builds, "unchanged settings must not rebuild again")
		})
	}
}

func testBatORTFallback(t *testing.T, modelEnv string, samples int, load func(*testing.T, string, string, string) ModelInstance) {
	t.Helper()
	dir := os.Getenv(modelEnv)
	if dir == "" {
		t.Skipf("set %s to verify ORT fallback", modelEnv)
	}
	for _, backend := range []string{conf.BackendPrefONNX, conf.BackendPrefOpenVINO} {
		t.Run(backend, func(t *testing.T) {
			// A missing library must be tried before any successful global OV load.
			loaded := inference.OpenVINORuntimeSnapshot().LibraryLoaded
			if backend == conf.BackendPrefOpenVINO && loaded {
				t.Skip("OpenVINO library already loaded; run alone to exercise a missing library")
			}
			model := load(t, dir, backend, filepath.Join(t.TempDir(), "missing-openvino.so"))
			t.Cleanup(func() { assert.NoError(t, model.Close()) })
			device, gotBackend, _ := model.RuntimeInfo()
			assert.Equal(t, BackendONNX, gotBackend)
			assert.Equal(t, deviceCPU, device)
			_, err := model.Predict(t.Context(), [][]float32{make([]float32, samples)})
			require.NoError(t, err)
		})
	}
}

func testBatRuntimeInfoAndClose(t *testing.T, samples int, create func(string) ModelInstance) {
	t.Helper()
	for _, backend := range []string{BackendONNX, BackendOpenVINO} {
		t.Run(backend, func(t *testing.T) {
			model := create(backend)
			device, gotBackend, precision := model.RuntimeInfo()
			assert.Equal(t, backend, gotBackend)
			assert.Equal(t, string(QuantizationFP32), precision)
			if backend == BackendOpenVINO {
				assert.Equal(t, inference.OVDeviceGPU, device)
			} else {
				assert.Equal(t, deviceCPU, device)
			}
			require.NoError(t, model.Close())
			require.NoError(t, model.Close())
			_, err := model.Predict(t.Context(), [][]float32{make([]float32, samples)})
			require.ErrorContains(t, err, "classifier is closed")
		})
	}
}
