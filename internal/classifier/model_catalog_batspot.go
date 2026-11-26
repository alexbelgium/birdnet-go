package classifier

import (
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/detection"
)

const (
	batSpotRepo            = "alexbelgium/BatSpot"
	batSpotModelFile       = "batspot_m09.onnx"
	batSpotModelSHA256     = "65caf3931d32b7d2804c76202b9445a2614c90d20640eec5ee8278c403bf45c4"
	batSpotModelSizeBytes  = 44705602
	batSpotLabelsFile      = "batspot_labels.txt"
	batSpotLabelsSHA256    = "6cb46191a3275af74f94df9b3b7b9eb904a930345356a851292e2a164e1addb6"
	batSpotLabelsSizeBytes = 468
	// batSpotMinRAMMB is the RAM floor for one session: measured ORT session
	// RSS of about 173 MB plus 15%, rounded up (the BSG-BAT manifest rule).
	batSpotMinRAMMB = 200
)

func batSpotCatalogEntry() CatalogEntry {
	return CatalogEntry{
		ID:              conf.ModelIDBatSpot,
		Name:            detection.ModelNameBatSpot,
		Description:     "Search-phase call classifier for European bats. Requires ultrasonic capture at 250 kHz or higher.",
		Author:          "Smeele et al. (BatSpot), ONNX conversion by alexbelgium",
		License:         "CC-BY-4.0",
		CommercialUse:   true,
		Category:        CategoryBat,
		Region:          "Europe",
		Channel:         ChannelExperimental,
		SpeciesCount:    batSpotSpeciesCount,
		Version:         batSpotVersion,
		RegistryID:      RegistryIDBatSpot,
		RequiresONNX:    true,
		UpstreamURL:     "https://doi.org/10.5281/zenodo.18977979",
		HuggingFaceRepo: batSpotRepo,
		Variants: []CatalogVariant{{
			ID:           "fp32",
			Precision:    "fp32",
			SpeciesCount: batSpotSpeciesCount,
			Default:      true,
			Requirements: VariantRequirements{MinRAMMB: batSpotMinRAMMB},
			Backends: map[string]BackendSupport{
				"onnxruntime-cpu": {Supported: true, Recommended: true},
			},
			Files: []CatalogFile{
				{RemotePath: batSpotModelFile, LocalName: batSpotModelFile, Role: RoleModel, SHA256: batSpotModelSHA256, SizeBytes: batSpotModelSizeBytes},
				{RemotePath: batSpotLabelsFile, LocalName: batSpotLabelsFile, Role: RoleLabels, SHA256: batSpotLabelsSHA256, SizeBytes: batSpotLabelsSizeBytes},
			},
		}},
	}
}
