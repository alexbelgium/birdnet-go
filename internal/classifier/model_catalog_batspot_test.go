package classifier

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/conf"
)

func TestBatSpotCatalogMatchesManifest(t *testing.T) {
	t.Parallel()

	manifestData, err := os.ReadFile("gen/manifests/documents/BatSpot.models.json")
	require.NoError(t, err)
	var manifest struct {
		Repo       string `json:"repo"`
		FamilyMeta struct {
			Author       string `json:"author"`
			License      string `json:"license"`
			SpeciesCount int    `json:"species_count"`
		} `json:"family_meta"`
		Models []struct {
			Path            string `json:"path"`
			SHA256          string `json:"sha256"`
			SizeBytes       int64  `json:"size_bytes"`
			Labels          string `json:"labels"`
			LabelsSHA256    string `json:"labels_sha256"`
			LabelsSizeBytes int64  `json:"labels_size_bytes"`
			UpstreamVersion string `json:"upstream_version"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(manifestData, &manifest))
	require.Len(t, manifest.Models, 1)

	entry, ok := GetCatalogEntry(conf.ModelIDBatSpot)
	require.True(t, ok)
	require.False(t, entry.Hidden)
	assert.Equal(t, RegistryIDBatSpot, entry.RegistryID)
	assert.Equal(t, CategoryBat, entry.Category)
	assert.True(t, entry.RequiresONNX)
	assert.Equal(t, manifest.Repo, entry.HuggingFaceRepo)
	assert.Equal(t, manifest.FamilyMeta.Author, entry.Author)
	assert.Equal(t, manifest.FamilyMeta.License, entry.License)
	assert.Equal(t, manifest.FamilyMeta.SpeciesCount, entry.SpeciesCount)
	model := &manifest.Models[0]
	assert.Equal(t, model.UpstreamVersion, entry.Version)
	assert.ElementsMatch(t, []CatalogFile{
		{Role: RoleModel, RemotePath: model.Path, LocalName: model.Path, SHA256: model.SHA256, SizeBytes: model.SizeBytes},
		{Role: RoleLabels, RemotePath: model.Labels, LocalName: model.Labels, SHA256: model.LabelsSHA256, SizeBytes: model.LabelsSizeBytes},
	}, entry.Files)
	require.Len(t, EntryDocuments(&entry), 1)
}

func TestBatSpotGalleryPaths(t *testing.T) {
	t.Parallel()

	entry, ok := GetCatalogEntry(conf.ModelIDBatSpot)
	require.True(t, ok)
	modelsDir := t.TempDir()
	installedModel := writeVariantModelFile(t, modelsDir, &entry, "fp32")
	installedLabels := writeVariantLabelsFile(t, modelsDir, &entry, "fp32")

	o := &Orchestrator{}
	o.SetModelsDir(modelsDir)
	cfg := &conf.Settings{}
	o.mu.RLock()
	resolved := o.batSpotPathResolution(cfg)
	o.mu.RUnlock()
	assert.Equal(t, modelFileSet{model: installedModel, labels: installedLabels}, resolved.resolved)

	fields, ok := familyFields(cfg, RegistryIDBatSpot)
	require.True(t, ok)
	require.Same(t, &cfg.BatSpot.ModelPath, fields.Model)
	require.Same(t, &cfg.BatSpot.LabelPath, fields.Labels)
	require.Same(t, &cfg.BatSpot.Threshold, fields.Threshold)
}
