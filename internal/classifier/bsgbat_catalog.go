package classifier

func init() {
	EmbeddedCatalog = append(EmbeddedCatalog, CatalogEntry{
		ID: "bsg-bat-eu", Name: "BSG-BAT (Europe)", Description: "BSG-BAT 0.21 classifier for European bat species",
		Author: "Meramo et al. (BSG-BATS, University of Helsinki); ONNX conversion by tphakala", License: "CC-BY-4.0", CommercialUse: true,
		Category: CategoryBat, Region: "Europe", SpeciesCount: 21, Version: "0.21", RegistryID: RegistryIDBSGBat,
		RequiresONNX: true, UpstreamURL: "https://doi.org/10.5281/zenodo.15495676", HuggingFaceRepo: "tphakala/BSG-BAT",
		Files: []CatalogFile{
			{RemotePath: "bsgbat_v0.21_r3.onnx", LocalName: "bsgbat_v0.21_r3.onnx", Role: RoleModel, SHA256: "6f03a2a52f8ada8b01e5232ca8b8550d71fd85441955a7183f5a0fec2cd90249", SizeBytes: 83101506},
			{RemotePath: "bsgbat_labels.txt", LocalName: "bsgbat_labels.txt", Role: RoleLabels, SHA256: "10690573f98a7088d26beb238b1e1a63e58c2a360bf12cabd2b0b494d2b43517", SizeBytes: 438},
		},
	})
}
