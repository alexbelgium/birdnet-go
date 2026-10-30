package conf

import (
	"math"
	"strings"

	"github.com/tphakala/birdnet-go/internal/errors"
)

const (
	// ModelIDBatDetect2 selects the BatDetect2 UK classifier.
	ModelIDBatDetect2 = "batdetect2"
	// ModelIDBSGBat selects the BSG-BAT European classifier.
	ModelIDBSGBat = "bsgbat"

	minBSGBatBackgroundMax = 0.5
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

// BSGBatModelsConfig holds the BSG-BAT files and prediction filters.
type BSGBatModelsConfig struct {
	// Threshold overrides the shared bat threshold; zero inherits it.
	Threshold float64 `yaml:"threshold" json:"threshold"`
	// ModelPath is the path to the ONNX classifier model.
	ModelPath string `yaml:"modelPath" json:"modelPath"`
	// LabelPath is the path to the classifier species labels.
	LabelPath string `yaml:"labelPath" json:"labelPath"`
	// BackgroundMax rejects windows whose Background sigmoid exceeds this value. Zero disables rejection (default); otherwise use 0.5 to 1.0. Suggested: 0.99.
	BackgroundMax float64 `yaml:"backgroundmax" json:"backgroundMax"`
}

// BatModelsConfig holds the optional fork-specific bat classifiers.
type BatModelsConfig struct {
	// BatDetect2 configures the optional BatDetect2 UK classifier files.
	BatDetect2 BatONNXPaths `yaml:"batdetect2" json:"batDetect2"`
	// BSGBat configures the optional BSG-BAT European classifier files.
	BSGBat BSGBatModelsConfig `yaml:"bsgbat" json:"bsgBat"`
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
	if err := validateBSGBatBackgroundMax(s.ForkModels.BSGBat.BackgroundMax); err != nil {
		s.recordValidationWarning(warnComponentModels, "%v; disabling the BSG-BAT background gate", err)
		s.ForkModels.BSGBat.BackgroundMax = 0
	}
}

func validateBSGBatBackgroundMax(value float64) error {
	if value == 0 || (!math.IsNaN(value) && value >= minBSGBatBackgroundMax && value <= maxModelThreshold) {
		return nil
	}
	return errors.Newf("forkmodels.bsgbat.backgroundmax must be 0 (disabled) or between %.2f and %.2f, got %v", minBSGBatBackgroundMax, maxModelThreshold, value).
		Component("conf").Category(errors.CategoryValidation).
		Context("operation", "validate_bsgbat_background_max").Build()
}
