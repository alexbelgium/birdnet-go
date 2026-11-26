package classifier

import (
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/errors"
)

func batSpotFamilyFields(settings *conf.Settings) familyFieldSet {
	return familyFieldSet{
		Model:     &settings.BatSpot.ModelPath,
		Labels:    &settings.BatSpot.LabelPath,
		Threshold: &settings.BatSpot.Threshold,
	}
}

// batSpotPathResolution relies on the caller holding o.mu for the gallery directory.
func (o *Orchestrator) batSpotPathResolution(settings *conf.Settings) pathResolution {
	return o.resolveFamilyPaths(RegistryIDBatSpot, modelFileSet{
		model:  settings.BatSpot.ModelPath,
		labels: settings.BatSpot.LabelPath,
	}, false)
}

func (o *Orchestrator) buildBatSpot(settings *conf.Settings, threads int) (ModelInstance, error) {
	o.mu.RLock()
	res := o.batSpotPathResolution(settings)
	o.mu.RUnlock()
	return buildBatSpotFromFiles(settings, threads, res.resolved)
}

func buildBatSpotFromFiles(settings *conf.Settings, threads int, files modelFileSet) (ModelInstance, error) {
	if !files.complete(false) {
		return nil, errors.Newf("BatSpot model and labels must be installed or configured").
			Component("classifier.orchestrator").
			Category(errors.CategoryConfiguration).
			Build()
	}
	if err := checkORTOrFail(settings.BirdNET.ONNXRuntimePath, detection.ModelNameBatSpot, RegistryIDBatSpot, "classifier.orchestrator"); err != nil {
		return nil, err
	}
	model, err := newBatSpot(files.model, files.labels, settings.BirdNET.ONNXRuntimePath, threads)
	if err != nil {
		return nil, errors.New(err).
			Component("classifier.orchestrator").
			Category(errors.CategoryModelInit).
			Context("model", RegistryIDBatSpot).
			Build()
	}
	return model, nil
}

// loadBatSpot registers the instance while the caller holds o.mu.
func (o *Orchestrator) loadBatSpot(threads int) error {
	settings := o.currentSettings()
	before := o.captureRSSBefore()
	res := o.batSpotPathResolution(settings)
	model, err := buildBatSpotFromFiles(settings, threads, res.resolved)
	if err != nil {
		return err
	}
	o.models[RegistryIDBatSpot] = &modelEntry{instance: model, backend: secondaryTripletFor(settings)}
	o.queuePathCorrection(RegistryIDBatSpot, res)
	o.deferWarmup(RegistryIDBatSpot, before)
	return nil
}
