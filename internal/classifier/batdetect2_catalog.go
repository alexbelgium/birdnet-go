package classifier

func init() {
	EmbeddedCatalog = append(EmbeddedCatalog, CatalogEntry{
		ID: "batdetect2-uk", Name: "BatDetect2 (UK)", Description: "BatDetect2 2.0.0b3 classifier for United Kingdom bat species",
		Author: "Mac Aodha et al. (BatDetect2); ONNX conversion by legojoey17", License: "CC-BY-NC-4.0", CommercialUse: false,
		Category: CategoryBat, Region: "United Kingdom", SpeciesCount: 17, Version: "2.0.0b3", RegistryID: RegistryIDBatDetect2,
		RequiresONNX: true, UpstreamURL: "https://github.com/macaodha/batdetect2", HuggingFaceRepo: "legojoey17/batdetect2-uk-same-onnx",
		Files: []CatalogFile{
			{RemotePath: "batdetect2-uk-same.onnx", LocalName: "batdetect2-uk-same.onnx", Role: RoleModel, SHA256: "9eddc08f1a22695ffdb1cdb52d947752d83777cd8596554f108bcf701481d340", SizeBytes: 7603041},
			{RemotePath: "labels.json", LocalName: "batdetect2-uk-labels.json", Role: RoleLabels, SHA256: "0a82dcd7a2a5d86a32ca3dcf22b9ced20fdb8c04bd2a78589cfaebd412e32122", SizeBytes: 203},
		},
	})
}
