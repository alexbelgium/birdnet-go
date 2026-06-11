package spectrogram

// FrequencyProfile controls spectrogram frequency range and resampling
// per detection. The gate is the detection's model type: bat models get
// a 0-120 kHz spectrogram, and everything else gets bird defaults
// (resample to 24 kHz, full range).
type FrequencyProfile struct {
	ResampleRate int // Target sample rate in Hz; 0 means keep native rate
	HighPassHz   int // High-pass filter cutoff in Hz; 0 means no filter
}

const (
	batResampleHz  = 240000
	birdResampleHz = 24000
)

// BirdProfile returns the default frequency profile for bird detections.
func BirdProfile() FrequencyProfile {
	return FrequencyProfile{
		ResampleRate: birdResampleHz,
		HighPassHz:   0,
	}
}

// BatProfile returns the frequency profile for bat detections. Resampling to
// 240 kHz makes the spectrogram frequency axis span 0-120 kHz.
func BatProfile() FrequencyProfile {
	return FrequencyProfile{
		ResampleRate: batResampleHz,
		HighPassHz:   0,
	}
}

// ProfileForModelType selects the appropriate frequency profile based on
// the AI model's type string (as stored in ai_models.model_type).
func ProfileForModelType(modelType string) FrequencyProfile {
	if modelType == "bat" {
		return BatProfile()
	}
	return BirdProfile()
}
