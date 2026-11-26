package conf

// ModelIDBatSpot identifies the BatSpot m09 classifier in configuration.
const ModelIDBatSpot = "batspot"

// BatSpotSampleRate is the native sample rate in hertz of BatSpot m09.
const BatSpotSampleRate = 250_000

// BatSpotConfig holds the local model files and confidence threshold for
// BatSpot m09. The nighttime schedule is shared with the other bat models.
type BatSpotConfig struct {
	// ModelPath points to the BatSpot m09 ONNX file.
	ModelPath string `yaml:"modelpath,omitempty" json:"modelPath,omitempty"`
	// LabelPath points to the 15 labels in checkpoint order.
	LabelPath string `yaml:"labelpath,omitempty" json:"labelPath,omitempty"`
	// Threshold is the per-species confidence threshold (0 to 1, default 0.5).
	Threshold float64 `yaml:"threshold" json:"threshold"`
}
