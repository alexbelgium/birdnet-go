package conf

import (
	"math"
)

// ModelIDBatDetect2 selects the BatDetect2 UK classifier.
const ModelIDBatDetect2 = "batdetect2"

// BatONNXPaths holds an ONNX bat model and label file pair.
type BatONNXPaths struct {
	// Threshold overrides the shared bat threshold; zero inherits it.
	Threshold float64 `yaml:"threshold" json:"threshold"`
	// ModelPath is the path to the ONNX classifier model.
	ModelPath string `yaml:"modelPath" json:"modelPath"`
	// LabelPath is the path to the classifier species labels.
	LabelPath string `yaml:"labelPath" json:"labelPath"`
}

// BatModelsConfig holds the optional fork-specific bat classifiers.
type BatModelsConfig struct {
	// BatDetect2 configures the optional BatDetect2 UK classifier files.
	BatDetect2 BatONNXPaths `yaml:"batdetect2" json:"batDetect2"`
}

func init() { ValidAudioModels[ModelIDBatDetect2] = true }

// BatModelThreshold returns the current threshold for a fork bat classifier.
// Zero overrides inherit the shared bat threshold.
func BatModelThreshold(settings *Settings, modelID string) float64 {
	threshold := settings.ForkModels.BatDetect2.Threshold
	if threshold > 0 {
		return threshold
	}
	return settings.Bat.Threshold
}

func (s *Settings) normalizeForkBatThresholds() {
	value := &s.ForkModels.BatDetect2.Threshold
	if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < minModelThreshold || *value > maxModelThreshold {
		s.recordValidationWarning(warnComponentModels, "forkmodels.batdetect2 threshold %v is outside [0, 1]; using the shared bat threshold", *value)
		*value = 0
	}
}
