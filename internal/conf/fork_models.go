package conf

import (
	"math"
	"strings"
)

const (
	// ModelIDBatDetect2 selects the BatDetect2 UK classifier.
	ModelIDBatDetect2 = "batdetect2"
	// ModelIDBSGBat selects the BSG-BAT European classifier.
	ModelIDBSGBat = "bsgbat"
)

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
	// BSGBat configures the optional BSG-BAT European classifier files.
	BSGBat BatONNXPaths `yaml:"bsgbat" json:"bsgBat"`
}

func init() { ValidAudioModels[ModelIDBatDetect2] = true; ValidAudioModels[ModelIDBSGBat] = true }

// BatModelThreshold returns the current threshold for a fork bat classifier.
// Zero overrides inherit the shared bat threshold.
func BatModelThreshold(settings *Settings, modelID string) float64 {
	threshold := 0.0
	switch strings.ToLower(modelID) {
	case ModelIDBatDetect2:
		threshold = settings.ForkModels.BatDetect2.Threshold
	case ModelIDBSGBat:
		threshold = settings.ForkModels.BSGBat.Threshold
	}
	if threshold > 0 {
		return threshold
	}
	return settings.Bat.Threshold
}

func (s *Settings) normalizeForkBatThresholds() {
	for name, value := range map[string]*float64{
		"forkmodels.batdetect2": &s.ForkModels.BatDetect2.Threshold,
		"forkmodels.bsgbat":     &s.ForkModels.BSGBat.Threshold,
	} {
		if math.IsNaN(*value) || math.IsInf(*value, 0) || *value < minModelThreshold || *value > maxModelThreshold {
			s.recordValidationWarning(warnComponentModels, "%s threshold %v is outside [0, 1]; using the shared bat threshold", name, *value)
			*value = 0
		}
	}
}
