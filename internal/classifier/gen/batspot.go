package main

import (
	"fmt"
	"net/http"
)

const batSpotManifest = "gen/manifests/documents/BatSpot.models.json"

// batSpotLabelSpec reads the one fixed label artifact pinned by the manifest.
func batSpotLabelSpec() (repo, path string, want labelChecksum, err error) {
	m, err := loadManifest(batSpotManifest)
	if err != nil {
		return "", "", labelChecksum{}, err
	}
	if m.Repo == "" || len(m.Models) != 1 {
		return "", "", labelChecksum{}, fmt.Errorf("%s: expected a repository and one model", batSpotManifest)
	}
	model := &m.Models[0]
	if !validHostedPath(model.Labels) || !sha256Pattern.MatchString(model.LabelsSHA256) ||
		model.LabelsSizeBytes <= 0 || model.Classes == nil || *model.Classes <= 0 {
		return "", "", labelChecksum{}, fmt.Errorf("%s: invalid label artifact metadata", batSpotManifest)
	}
	return m.Repo, model.Labels, labelChecksum{
		SHA256: model.LabelsSHA256, SizeBytes: model.LabelsSizeBytes, Lines: *model.Classes,
	}, nil
}

// validateBatSpotLabels checks the pinned fixed label file during offline generation.
func validateBatSpotLabels(labels map[string]labelChecksum) error {
	repo, path, want, err := batSpotLabelSpec()
	if err != nil {
		return err
	}
	key := repo + "|" + path
	if labels[key] != want {
		return fmt.Errorf("%s: label metadata for %q differs from the checksum sidecar", batSpotManifest, key)
	}
	return nil
}

// updateBatSpotLabels includes the fixed label file when refreshing the sidecar.
func updateBatSpotLabels(client *http.Client, labels map[string]labelChecksum) error {
	repo, path, want, err := batSpotLabelSpec()
	if err != nil {
		return err
	}
	got, err := fetchLabelChecksum(client, repo, path)
	if err != nil {
		return fmt.Errorf("fetch BatSpot labels: %w", err)
	}
	if got != want {
		return fmt.Errorf("%s: published label metadata differs from the manifest", batSpotManifest)
	}
	labels[repo+"|"+path] = got
	return nil
}
