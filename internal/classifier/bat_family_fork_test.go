package classifier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
)

func TestMultipleGatedModelsScheduler(t *testing.T) {
	o := newTestOrchestrator(t, &mockModelInstance{id: RegistryIDBatDetect2}, &mockModelInstance{id: RegistryIDBSGBat})
	scheduler := newNighttimeScheduler(nil)
	scheduler.start(func() bool { return false })
	t.Cleanup(scheduler.stop)
	o.scheduler.Store(scheduler)
	require.NoError(t, o.UnloadModel(RegistryIDBatDetect2))
	select {
	case <-scheduler.stopChan:
		assert.Fail(t, "scheduler stopped with another gated model loaded")
	default:
	}
	require.NoError(t, o.UnloadModel(RegistryIDBSGBat))
	assert.Eventually(t, func() bool {
		select {
		case <-scheduler.stopChan:
			return true
		default:
			return false
		}
	}, schedulerRefreshInterval, schedulerRefreshInterval/10)
}

func TestForkBatThreadReload(t *testing.T) {
	original := conf.Setting()
	settings := *original
	settings.BirdNET.Backend = BackendONNX
	settings.BirdNET.Threads = 1
	conftest.SetTestSettings(&settings)
	t.Cleanup(func() { conftest.SetTestSettings(original) })
	for _, id := range []string{RegistryIDBatDetect2, RegistryIDBSGBat} {
		t.Run(id, func(t *testing.T) {
			builder, ok := openvinoCapableSecondaryBuilders[id]
			require.True(t, ok)
			t.Cleanup(func() { openvinoCapableSecondaryBuilders[id] = builder })
			old, fresh := &reloadFakeModel{id: id}, &reloadFakeModel{id: id}
			o := newTestOrchestrator(t)
			key := secondaryTripletFor(&settings)
			key.threads = 4
			o.models[id] = &modelEntry{instance: old, backend: key}
			gotThreads := 0
			openvinoCapableSecondaryBuilders[id] = func(_ *Orchestrator, _ *conf.Settings, threads int) (ModelInstance, error) {
				gotThreads = threads
				return fresh, nil
			}
			require.NoError(t, o.ReloadSecondaryModels())
			assert.Equal(t, 1, gotThreads)
			assert.Same(t, ModelInstance(fresh), o.models[id].instance)
			assert.EqualValues(t, 1, old.closes.Load())
		})
	}
}

func TestOrderedBatEntries(t *testing.T) {
	bat, detector, bsg := &modelEntry{}, &modelEntry{}, &modelEntry{}
	models := map[string]*modelEntry{RegistryIDBSGBat: bsg, RegistryIDBatDetect2: detector, RegistryIDBat: bat, RegistryIDBirdNETV24: {}}
	assert.Equal(t, []*modelEntry{bat, bsg, detector}, orderedBatEntries(models))
}
