package classifier

import (
	"time"

	"github.com/tphakala/birdnet-go/internal/conf"
)

// RegistryIDBSGBat is the registry ID of the BSG-BAT European classifier.
const RegistryIDBSGBat = "BSGBat"

func init() {
	batFamilyIDs[RegistryIDBSGBat] = struct{}{}
	ModelRegistry[RegistryIDBSGBat] = ModelInfo{
		ID: RegistryIDBSGBat, Name: "BSG-BAT (Europe)", Backend: BackendONNX,
		DetectionName: "BSG-BAT", DetectionVersion: "0.21",
		Description: "BSG-BAT European bat species classifier", NumSpecies: 21,
		Spec: ModelSpec{SampleRate: bsgBatRawSampleRate, ClipLength: time.Second, RawSampleRate: bsgBatRawSampleRate,
			MinRawSampleRate: 96000, RecommendedSampleRate: 384000},
		ConfigAliases: []string{conf.ModelIDBSGBat}, rangeFilterCompat: rangeFilterCompatNone, scheduleGated: true,
	}
	modelLoaders[RegistryIDBSGBat] = (*Orchestrator).loadBSGBat
}

func (o *Orchestrator) loadBSGBat(threads int) error {
	settings := o.currentSettings()
	return o.loadBatONNX(RegistryIDBSGBat, ModelRegistry[RegistryIDBSGBat].DetectionName, settings.BirdNET.ONNXRuntimePath,
		modelFileSet{model: settings.ForkModels.BSGBat.ModelPath, labels: settings.ForkModels.BSGBat.LabelPath}, threads,
		func(modelPath, labelPath, runtimePath string, modelThreads int) (ModelInstance, error) {
			return NewBSGBat(&BSGBatModelConfig{ModelPath: modelPath, LabelPath: labelPath, ONNXRuntimePath: runtimePath, Threads: modelThreads})
		})
}
