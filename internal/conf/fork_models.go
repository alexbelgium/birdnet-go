package conf

const (
	// ModelIDBatDetect2 selects the BatDetect2 UK classifier.
	ModelIDBatDetect2 = "batdetect2"
	// ModelIDBSGBat selects the BSG-BAT European classifier.
	ModelIDBSGBat = "bsgbat"
)

// BatONNXPaths holds an ONNX bat model and label file pair.
type BatONNXPaths struct {
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
