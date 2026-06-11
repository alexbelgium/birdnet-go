package spectrogram

import (
	"strings"

	"github.com/tphakala/birdnet-go/internal/conf"
)

// FrequencyProfile controls spectrogram frequency range and resampling
// per detection. Bat models use 0-120 kHz spectrograms, and everything
// else gets bird defaults (resample to 24 kHz, full range).
type FrequencyProfile struct {
	ResampleRate int // Target sample rate in Hz; 0 means keep native rate
	HighPassHz   int // High-pass filter cutoff in Hz; 0 means no filter
}

const (
	batSpectrogramSampleRateHz = 240000
	birdResampleHz             = 24000
)

// BirdProfile returns the default frequency profile for bird detections.
func BirdProfile() FrequencyProfile {
	return FrequencyProfile{
		ResampleRate: birdResampleHz,
		HighPassHz:   0,
	}
}

// BatProfile returns the frequency profile for bat detections. Resampling to
// 240 kHz makes the spectrogram span 0-120 kHz.
func BatProfile() FrequencyProfile {
	return FrequencyProfile{
		ResampleRate: batSpectrogramSampleRateHz,
		HighPassHz:   0,
	}
}

// ProfileForModelType selects the appropriate frequency profile based on
// the AI model's type string (as stored in ai_models.model_type).
func ProfileForModelType(modelType string) FrequencyProfile {
	if strings.EqualFold(modelType, conf.ModelIDBat) {
		return BatProfile()
	}
	return BirdProfile()
}
