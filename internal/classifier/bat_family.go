package classifier

import (
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// isBatFamily reports whether id identifies a bat classifier that shares the
// bat scheduling, filtering, and threshold settings.
var batFamilyIDs = map[string]struct{}{RegistryIDBat: {}}

func isBatFamily(id string) bool { _, ok := batFamilyIDs[id]; return ok }

// IsBatFamily reports whether id identifies a supported bat classifier.
func IsBatFamily(id string) bool { return isBatFamily(id) }

func hasScheduleGated(models map[string]*modelEntry) bool {
	for id := range models {
		if isScheduleGated(id) {
			return true
		}
	}
	return false
}

func (o *Orchestrator) resolveBatONNXFiles(modelID, displayName, runtimePath string, files modelFileSet) (pathResolution, error) {
	res := o.resolveFamilyPaths(modelID, files, true)
	if res.resolved.model == "" || res.resolved.labels == "" {
		return pathResolution{}, errors.Newf("%s model files not installed or configured", displayName).Component("classifier.orchestrator").Category(errors.CategoryModelInit).Context("model", modelID).Build()
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
		return errors.New(err).Component("classifier.orchestrator").Category(errors.CategoryModelInit).Context("model", modelID).Build()
	}
	o.registerLoadedBatModel(model, res, before)
	return nil
}
