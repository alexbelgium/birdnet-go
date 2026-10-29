package classifier

import (
	"time"

	"github.com/tphakala/birdnet-go/internal/conf"
)

// RegistryIDBatDetect2 is the registry ID of the BatDetect2 UK classifier.
const RegistryIDBatDetect2 = "BatDetect2"

func init() {
	batFamilyIDs[RegistryIDBatDetect2] = struct{}{}
	ModelRegistry[RegistryIDBatDetect2] = ModelInfo{
		ID: RegistryIDBatDetect2, Name: "BatDetect2 (UK)", Backend: BackendONNX,
		DetectionName: "BatDetect2", DetectionVersion: "2.0.0b3",
		Description: "BatDetect2 UK bat species classifier", NumSpecies: 17,
		Spec:          ModelSpec{SampleRate: 48000, ClipLength: 3 * time.Second, RawSampleRate: 256000, MinRawSampleRate: 96000, RecommendedSampleRate: 192000},
		ConfigAliases: []string{conf.ModelIDBatDetect2}, rangeFilterCompat: rangeFilterCompatNone, scheduleGated: true,
	}
	// ORT-only models ignore the OpenVINO preference, but rebuild for thread changes.
	openvinoCapableSecondaryBuilders[RegistryIDBatDetect2] = func(o *Orchestrator, settings *conf.Settings, threads int) (ModelInstance, error) {
		paths := settings.ForkModels.BatDetect2
		res, err := o.resolveBatONNXFiles(RegistryIDBatDetect2, ModelRegistry[RegistryIDBatDetect2].DetectionName, settings.BirdNET.ONNXRuntimePath, modelFileSet{model: paths.ModelPath, labels: paths.LabelPath})
		if err != nil {
			return nil, err
		}
		model, err := NewBatDetect2(&BatDetect2ModelConfig{ModelPath: res.resolved.model, LabelPath: res.resolved.labels, ONNXRuntimePath: settings.BirdNET.ONNXRuntimePath, Threads: threads})
		if err != nil {
			return nil, err
		}
		return model, nil
	}

	modelLoaders[RegistryIDBatDetect2] = (*Orchestrator).loadBatDetect2
}

func (o *Orchestrator) loadBatDetect2(threads int) error {
	settings := o.currentSettings()
	return o.loadBatONNX(RegistryIDBatDetect2, ModelRegistry[RegistryIDBatDetect2].DetectionName, settings.BirdNET.ONNXRuntimePath,
		modelFileSet{model: settings.ForkModels.BatDetect2.ModelPath, labels: settings.ForkModels.BatDetect2.LabelPath}, threads,
		func(modelPath, labelPath, runtimePath string, modelThreads int) (ModelInstance, error) {
			return NewBatDetect2(&BatDetect2ModelConfig{ModelPath: modelPath, LabelPath: labelPath, ONNXRuntimePath: runtimePath, Threads: modelThreads})
		})
}
