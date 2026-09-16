package detection

import "github.com/tphakala/birdnet-go/internal/datastore/v2/entities"

// Default model constants.
const (
	DefaultModelName    = "BirdNET"
	DefaultModelVersion = "2.4"
	DefaultModelVariant = "default"

	// CustomModelVariant marks a user-supplied classifier. It is the only variant
	// that means "custom": classifier.ToDetectionModelInfo sets it for a non-stock
	// CustomPath, while other non-default variants describe provenance rather than
	// authorship (imports.go stores "import" for BirdNET-Pi imports).
	CustomModelVariant = "custom"
)

// ModelInfo describes the AI model used for detection.
type ModelInfo struct {
	Name           string  // e.g., "BirdNET"
	Version        string  // e.g., "2.4"
	Variant        string  // e.g., "default", "finland_birds"
	ClassifierPath *string // path to custom classifier file, nil for default
	ModelType      string  // e.g., "bird", "bat", "multi"; empty when unknown
}

// WithDefaults returns a copy of the ModelInfo with empty fields replaced by defaults.
func (m ModelInfo) WithDefaults() ModelInfo {
	if m.Name == "" {
		m.Name = DefaultModelName
	}
	if m.Version == "" {
		m.Version = DefaultModelVersion
	}
	if m.Variant == "" {
		m.Variant = DefaultModelVariant
	}
	return m
}

// IsCustom reports whether the detection came from a user-supplied classifier
// rather than a stock model. Callers must not infer this from "variant is not
// default": imported and other provenance variants are stock models too.
func (m ModelInfo) IsCustom() bool {
	return m.Variant == CustomModelVariant
}

// DefaultModelInfo returns the default BirdNET model info.
func DefaultModelInfo() ModelInfo {
	return ModelInfo{
		Name:           DefaultModelName,
		Version:        DefaultModelVersion,
		Variant:        DefaultModelVariant,
		ClassifierPath: nil,
	}
}

// ResolveModelType determines the entity ModelType from a model's name and version.
// BattyBirdNET models are bat, BirdNET v3.0+ and Perch are multi-taxa wildlife,
// and everything else (BirdNET v2.4, BSG, unknown) is bird.
func ResolveModelType(name, version string) entities.ModelType {
	switch {
	case name == "BattyBirdNET":
		return entities.ModelTypeBat
	case name == "Perch":
		return entities.ModelTypeMulti
	case name == "BirdNET" && version != "" && version != "2.4":
		return entities.ModelTypeMulti
	default:
		return entities.ModelTypeBird
	}
}
