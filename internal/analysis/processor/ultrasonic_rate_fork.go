package processor

import "github.com/tphakala/birdnet-go/internal/classifier"

// ultrasonicPCMRate resolves the rate of the already-resampled analysis PCM.
func ultrasonicPCMRate(modelID string, sourceRate int) int {
	if info, ok := classifier.ModelRegistry[modelID]; ok && info.Spec.RawSampleRate > 0 {
		return info.Spec.EffectiveSampleRate()
	}
	return sourceRate
}
