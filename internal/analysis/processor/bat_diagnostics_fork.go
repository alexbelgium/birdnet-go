package processor

import "github.com/tphakala/birdnet-go/internal/classifier"

// Fork bat candidates are useful in the diagnostic feed even when they do not
// clear the recording threshold. Persistence still uses shouldFilterDetection.
func recordModelPrediction(modelID string, confidence, baseThreshold float32) bool {
	switch modelID {
	case classifier.RegistryIDBatDetect2, classifier.RegistryIDBSGBat:
		return confidence >= classifier.BatCandidateFloor
	default:
		return confidence > baseThreshold
	}
}
