package conf

const (
	ModelIDBatDetect2 = "batdetect2"
	ModelIDBSGBat     = "bsgbat"
)

// BatONNXPaths holds an ONNX bat model and label file pair.
type BatONNXPaths struct {
	ModelPath string `yaml:"modelPath" json:"modelPath"`
	LabelPath string `yaml:"labelPath" json:"labelPath"`
}

// ForkModelsConfig holds the optional fork-specific bat classifiers.
type ForkModelsConfig struct {
	BatDetect2 BatONNXPaths `yaml:"batdetect2" json:"batDetect2"`
	BSGBat     BatONNXPaths `yaml:"bsgbat" json:"bsgBat"`
}

func init() { ValidAudioModels[ModelIDBatDetect2] = true; ValidAudioModels[ModelIDBSGBat] = true }
