// Package api curation endpoints back the "All Species" view: include / confirm
// membership toggles and a per-species review-statistics aggregate. Exclude
// (ignore) toggles and per-species delete already exist (IgnoreSpecies /
// GetExcludedSpecies and the batch endpoints), so the frontend reuses those
// rather than duplicating them here.
//
// This file is intentionally self-contained: the only edit to an existing file
// is a single route-init entry in api.go. The curation "confirmed" list is
// persisted in a small JSON sidecar next to config.yaml (see confirmedStore)
// rather than in the core Settings struct, so the feature touches no shared
// config or settings code. Include/Exclude reuse the existing SpeciesSettings
// lists and their endpoints.
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// Membership toggle action labels, returned in SpeciesMembershipResponse.Action
// to describe the outcome of a read-modify-write. Kept local to this file so the
// curation feature stays self-contained.
const (
	membershipActionAdded   = "added"
	membershipActionRemoved = "removed"
)

// reviewStatsPageSize is the page size used to sweep reviewed detections.
const reviewStatsPageSize = 500

// reviewStatsMaxPages caps the sweep so a pathological dataset cannot make the
// handler run unbounded (500 * 400 = 200k reviewed detections).
const reviewStatsMaxPages = 400

// confirmedSpeciesFile is the JSON sidecar (co-located with config.yaml) that
// persists the curation "confirmed" list.
const confirmedSpeciesFile = "confirmed_species.json"

// confirmedStore persists the curation "confirmed" species list in a small JSON
// sidecar next to config.yaml. The confirmed flag is curation-only (it does not
// affect detection), so it lives outside the core Settings struct and is owned
// entirely by this file. All access goes through the mutex; the on-disk file is
// the source of truth and the in-memory slice is a lazily-loaded cache.
type confirmedStore struct {
	mu     sync.Mutex
	loaded bool
	names  []string
}

// confirmedSpecies is the process-wide store. A single sidecar file backs the
// list, so one shared instance keeps the cache and file consistent across
// concurrent requests.
var confirmedSpecies = &confirmedStore{}

// path resolves the sidecar location next to the active config.yaml.
func (s *confirmedStore) path() (string, error) {
	dir, err := conf.ResolveConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, confirmedSpeciesFile), nil
}

// load reads the sidecar into the cache once. A missing file is not an error -
// it simply means nothing has been confirmed yet. Caller must hold s.mu.
func (s *confirmedStore) load() error {
	if s.loaded {
		return nil
	}
	path, err := s.path()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		s.names = nil
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return err
	}
	s.names = names
	s.loaded = true
	return nil
}

// save writes the cache to disk atomically (temp file + rename). Caller must
// hold s.mu.
func (s *confirmedStore) save() error {
	path, err := s.path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.names, "", "  ")
	if err != nil {
		return err
	}
	// The config dir normally exists (config.yaml lives there), but guard against
	// a fresh setup whose directory has not been created yet.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // don't leave the temp file behind on a failed rename
		return err
	}
	return nil
}

// list returns a copy of the confirmed species names.
func (s *confirmedStore) list() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return slices.Clone(s.names), nil
}

// toggle adds the species when absent and removes it when present, persisting
// the change. It returns the resulting action label and membership state.
func (s *confirmedStore) toggle(name string) (action string, isMember bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if loadErr := s.load(); loadErr != nil {
		return "", false, loadErr
	}
	if slices.Contains(s.names, name) {
		s.names = slices.DeleteFunc(s.names, func(n string) bool { return n == name })
		action, isMember = membershipActionRemoved, false
	} else {
		s.names = append(s.names, name)
		action, isMember = membershipActionAdded, true
	}
	if saveErr := s.save(); saveErr != nil {
		// Force a reload so the cache re-syncs with disk after a failed write.
		s.loaded = false
		return "", false, saveErr
	}
	return action, isMember, nil
}

// SpeciesNameRequest is the body for the include / confirm toggle endpoints.
type SpeciesNameRequest struct {
	CommonName string `json:"common_name"`
}

// SpeciesMembershipResponse reports the outcome of a membership toggle.
type SpeciesMembershipResponse struct {
	CommonName string `json:"common_name"`
	Action     string `json:"action"` // "added" or "removed"
	IsMember   bool   `json:"is_member"`
}

// SpeciesListResponse returns a simple list of species names with a count.
type SpeciesListResponse struct {
	Species []string `json:"species"`
	Count   int      `json:"count"`
}

// SpeciesReviewStat holds the verification tally for a single species.
type SpeciesReviewStat struct {
	ScientificName string  `json:"scientific_name"`
	Correct        int     `json:"correct"`
	FalsePositive  int     `json:"false_positive"`
	Total          int     `json:"total"`
	CorrectRate    float64 `json:"correct_rate"` // Correct / Total; 0 when Total == 0
}

// SpeciesReviewStatsResponse wraps the per-species tally with a truncation flag.
// Truncated is true when the sweep stopped at reviewStatsMaxPages before reaching
// the end, so the client can warn that the numbers may be incomplete instead of
// silently presenting a partial tally.
type SpeciesReviewStatsResponse struct {
	Stats     map[string]*SpeciesReviewStat `json:"stats"`
	Truncated bool                          `json:"truncated"`
}

// initSpeciesCurationRoutes registers the curation endpoints. The mutating and
// list endpoints are auth-protected (they expose and change curation state),
// mirroring the existing /detections/ignore* endpoints.
func (c *Controller) initSpeciesCurationRoutes() {
	group := c.Group.Group("/detections", c.AuthMiddleware)
	group.GET("/included", c.GetIncludedSpecies)
	group.POST("/include", c.IncludeSpecies)
	group.GET("/confirmed", c.GetConfirmedSpecies)
	group.POST("/confirm", c.ConfirmSpecies)

	c.Group.GET("/analytics/species/review-stats", c.GetSpeciesReviewStats, c.AuthMiddleware)
}

// GetIncludedSpecies returns the always-include list.
func (c *Controller) GetIncludedSpecies(ctx echo.Context) error {
	species := slices.Clone(c.getSettingsOrFallback().Realtime.Species.Include)
	return ctx.JSON(http.StatusOK, SpeciesListResponse{Species: species, Count: len(species)})
}

// GetConfirmedSpecies returns the curation "confirmed" list from the sidecar.
func (c *Controller) GetConfirmedSpecies(ctx echo.Context) error {
	species, err := confirmedSpecies.list()
	if err != nil {
		return c.HandleError(ctx, err, "Failed to load confirmed list", http.StatusInternalServerError)
	}
	return ctx.JSON(http.StatusOK, SpeciesListResponse{Species: species, Count: len(species)})
}

// IncludeSpecies toggles a species in the always-include list. Because the
// include list affects detection, the change triggers settings side-effects
// (range-filter rebuild) just like the exclude toggle.
func (c *Controller) IncludeSpecies(ctx echo.Context) error {
	req := &SpeciesNameRequest{}
	if err := ctx.Bind(req); err != nil {
		return c.HandleError(ctx, err, "Invalid request format", http.StatusBadRequest)
	}
	name := strings.TrimSpace(req.CommonName)
	if name == "" {
		return c.HandleError(ctx, nil, "Missing species name", http.StatusBadRequest)
	}

	action, isMember, err := c.toggleSpeciesMembership(name,
		func(s *conf.SpeciesSettings) []string { return s.Include },
		func(s *conf.SpeciesSettings, v []string) { s.Include = v },
		true)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to update include list", http.StatusInternalServerError)
	}

	c.LogInfoIfEnabled("Species include toggled",
		logger.String("species", name),
		logger.String("action", action),
		logger.Bool("is_included", isMember),
	)
	return ctx.JSON(http.StatusOK, SpeciesMembershipResponse{CommonName: name, Action: action, IsMember: isMember})
}

// ConfirmSpecies toggles a species in the curation "confirmed" list. This flag
// is curation-only and does not affect detection, so it is persisted in the JSON
// sidecar (confirmedStore) rather than in the core Settings struct, and no
// detection side-effects are run.
func (c *Controller) ConfirmSpecies(ctx echo.Context) error {
	req := &SpeciesNameRequest{}
	if err := ctx.Bind(req); err != nil {
		return c.HandleError(ctx, err, "Invalid request format", http.StatusBadRequest)
	}
	name := strings.TrimSpace(req.CommonName)
	if name == "" {
		return c.HandleError(ctx, nil, "Missing species name", http.StatusBadRequest)
	}

	action, isMember, err := confirmedSpecies.toggle(name)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to update confirmed list", http.StatusInternalServerError)
	}

	c.LogInfoIfEnabled("Species confirmation toggled",
		logger.String("species", name),
		logger.String("action", action),
		logger.Bool("is_confirmed", isMember),
	)
	return ctx.JSON(http.StatusOK, SpeciesMembershipResponse{CommonName: name, Action: action, IsMember: isMember})
}

// toggleSpeciesMembership adds or removes a species from a SpeciesSettings name
// list (currently the Include list) under the settings mutex, persisting the
// result. get/set select the target list on a SpeciesSettings snapshot. The
// confirmed list does not use this path - it is curation-only and persisted in
// the sidecar (see confirmedStore).
func (c *Controller) toggleSpeciesMembership(
	species string,
	get func(s *conf.SpeciesSettings) []string,
	set func(s *conf.SpeciesSettings, v []string),
	affectsDetection bool,
) (action string, isMember bool, err error) {
	if species == "" {
		return "", false, nil
	}

	// Serialise read-modify-write against concurrent settings saves so an
	// out-of-band StoreSettings cannot interleave between read and publish.
	c.settingsMutex.Lock()
	defer c.settingsMutex.Unlock()

	current := c.getSettingsOrFallback()
	was := slices.Contains(get(&current.Realtime.Species), species)

	updated := conf.CloneSettings(current)
	list := get(&updated.Realtime.Species)
	if was {
		list = slices.DeleteFunc(list, func(s string) bool { return s == species })
		action, isMember = membershipActionRemoved, false
	} else {
		list = append(list, species)
		action, isMember = membershipActionAdded, true
	}
	set(&updated.Realtime.Species, list)

	if saveErr := c.publishAndSaveSettings(current, updated); saveErr != nil {
		return "", was, saveErr
	}

	if affectsDetection {
		if handleErr := c.handleSettingsChanges(current, updated); handleErr != nil {
			GetLogger().Warn("Failed to trigger settings side-effects after species membership change",
				logger.Error(handleErr),
				logger.String("species", species),
				logger.String("action", action))
		}
	}

	return action, isMember, nil
}

// GetSpeciesReviewStats aggregates human review outcomes per species across all
// dates and returns a map keyed by scientific name. It sweeps only reviewed
// detections (the verified filter), which are typically a small, user-curated
// subset, so a paginated scan is inexpensive. No new datastore method is
// required - it builds on the existing advanced search.
func (c *Controller) GetSpeciesReviewStats(ctx echo.Context) error {
	verified := true
	stats := make(map[string]*SpeciesReviewStat)
	// Assume truncated until a short page proves we reached the end of the set.
	truncated := true

	for page := range reviewStatsMaxPages {
		filters := datastore.AdvancedSearchFilters{
			Verified: &verified,
			// Empty SortBy with SortAscending resolves to "date ASC, time ASC" in
			// the datastore - a deterministic order for stable offset paging,
			// without repeating the sort-key literal used elsewhere in the package.
			SortAscending: true,
			Limit:         reviewStatsPageSize,
			Offset:        page * reviewStatsPageSize,
		}

		notes, _, searchErr := c.DS.SearchNotesAdvanced(&filters)
		if searchErr != nil {
			return c.HandleError(ctx, searchErr, "Failed to load review statistics", http.StatusInternalServerError)
		}

		for i := range notes {
			sci := notes[i].ScientificName
			if sci == "" {
				continue
			}
			stat := stats[sci]
			if stat == nil {
				stat = &SpeciesReviewStat{ScientificName: sci}
				stats[sci] = stat
			}
			switch notes[i].Verified {
			case VerificationStatusCorrect:
				stat.Correct++
			case VerificationStatusFalsePositive:
				stat.FalsePositive++
			}
		}

		if len(notes) < reviewStatsPageSize {
			truncated = false
			break
		}
	}

	for _, stat := range stats {
		stat.Total = stat.Correct + stat.FalsePositive
		if stat.Total > 0 {
			stat.CorrectRate = float64(stat.Correct) / float64(stat.Total)
		}
	}

	return ctx.JSON(http.StatusOK, SpeciesReviewStatsResponse{Stats: stats, Truncated: truncated})
}
