package classifier

import (
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/logger"
)

func (o *Orchestrator) resolveBatONNXFiles(modelID, displayName, runtimePath string, files modelFileSet) (pathResolution, error) {
	res := o.resolveFamilyPaths(modelID, files, true)
	if res.resolved.model == "" || res.resolved.labels == "" {
		return pathResolution{}, errors.Newf("%s model files not installed or configured", displayName).Component("classifier.orchestrator").Category(errors.CategoryModelInit).Context("operation", "load_bat_model").Context("model", modelID).Build()
	}
	if err := checkORTOrFail(runtimePath, displayName+" model", modelID, "classifier.orchestrator"); err != nil {
		return pathResolution{}, err
	}
	return res, nil
}

func (o *Orchestrator) registerLoadedBatModel(model ModelInstance, res pathResolution, before uint64) {
	o.models[model.ModelID()] = &modelEntry{instance: model, backend: secondaryTripletFor(o.currentSettings())}
	o.queuePathCorrection(model.ModelID(), res)
	o.deferWarmup(model.ModelID(), before)
	GetLogger().Info(model.ModelName()+" model loaded into Orchestrator", logger.String("model_id", model.ModelID()), logger.Int("species", model.NumSpecies()))
}

func (o *Orchestrator) loadBatONNX(modelID, displayName, runtimePath string, files modelFileSet, threads int, constructor func(string, string, string, int) (ModelInstance, error)) error {
	before := o.captureRSSBefore()
	res, err := o.resolveBatONNXFiles(modelID, displayName, runtimePath, files)
	if err != nil {
		return err
	}
	model, err := constructor(res.resolved.model, res.resolved.labels, runtimePath, threads)
	if err != nil {
		return errors.New(err).Component("classifier.orchestrator").Category(errors.CategoryModelInit).Context("operation", "load_bat_model").Context("model", modelID).Build()
	}
	o.registerLoadedBatModel(model, res, before)
	return nil
}
