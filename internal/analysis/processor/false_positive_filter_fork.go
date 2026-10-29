package processor

import (
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
)

// batFamilyMinDetections returns the bat false-positive confirmation count for a
// bat-family model. Models sharing the BattyBirdNET 3s clip use the fixed 1.5s
// step of calculateBatMinDetections; a bat model with a different clip (BSG-BAT,
// 1s) derives its step from its own clip and fixed 50% overlap, so the
// confirmation window matches its real buffer cadence.
func batFamilyMinDetections(settings *conf.Settings, modelID string) int {
	info, ok := classifier.ModelRegistry[modelID]
	if !ok || info.Spec.ClipLength <= 0 || info.Spec.ClipLength == classifier.AnalysisBaseClipLength {
		return calculateBatMinDetections(settings)
	}
	// Bat-family overlap is fixed by ResolveModelOverlap and ignores the base overlap.
	step := info.Spec.BufferInterval(classifier.ResolveModelOverlap(modelID, info.Spec, 0)).Seconds()
	return minDetectionsForSegment(step, settings.Bat.FalsePositiveFilter.Level)
}

// addForkBatVisibilityThresholds adds a visibility threshold for every bat-family
// model other than BattyBirdNET, each derived from its own cadence.
func addForkBatVisibilityThresholds(vt visibilityThresholds, settings *conf.Settings) {
	for id := range classifier.ModelRegistry {
		if id != classifier.RegistryIDBat && classifier.IsBatFamily(id) {
			vt[id] = CalculateVisibilityThreshold(batFamilyMinDetections(settings, id))
		}
	}
}
