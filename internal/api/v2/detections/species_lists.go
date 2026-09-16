// internal/api/v2/detections/species_lists.go
//
// Managed species-list endpoints (always-include and confirmed) for the
// analytics "Manage" view. These mirror the ignore-list toggle but act on the
// Realtime.Species.Include / Realtime.Species.Confirmed lists. The confirmed
// list is analytics-only and does not affect detection processing, so its toggle
// does not trigger settings side-effects.
package detections

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// registerSpeciesListRoutes registers the species workspace list endpoints on
// the protected detections group.
func (c *Handler) registerSpeciesListRoutes(detectionGroup *echo.Group) {
	detectionGroup.POST("/include", c.IncludeSpecies)
	detectionGroup.GET("/included", c.GetIncludedSpecies)
	detectionGroup.POST("/confirm", c.ConfirmSpecies)
	detectionGroup.GET("/confirmed", c.GetConfirmedSpecies)
	detectionGroup.POST("/species/delete", c.DeleteSpeciesDetections)
}

// Species-list toggle actions returned to clients.
const (
	speciesActionAdded   = "added"
	speciesActionRemoved = "removed"
)

// SpeciesListRequest represents the request body for toggling a species in a
// managed species list (always-include or confirmed).
type SpeciesListRequest struct {
	CommonName string `json:"common_name"`
}

// SpeciesListToggleResponse is returned by the include/confirm toggle endpoints.
type SpeciesListToggleResponse struct {
	CommonName string `json:"common_name"`
	Action     string `json:"action"` // "added" or "removed"
	Present    bool   `json:"present"`
}

// SpeciesListResponse is returned by the included/confirmed list endpoints.
type SpeciesListResponse struct {
	Species []string `json:"species"`
	Count   int      `json:"count"`
}

// IncludeSpecies toggles a species in the always-include list (adds if absent, removes if present).
func (c *Handler) IncludeSpecies(ctx echo.Context) error {
	return c.toggleManagedSpecies(ctx,
		func(s *conf.Settings) []string { return s.Realtime.Species.Include },
		func(s *conf.Settings, list []string) { s.Realtime.Species.Include = list },
		true, "include")
}

// GetIncludedSpecies returns the always-include species list.
func (c *Handler) GetIncludedSpecies(ctx echo.Context) error {
	species := nonNilSpeciesList(c.getSettingsOrFallback().Realtime.Species.Include)

	return ctx.JSON(http.StatusOK, SpeciesListResponse{
		Species: species,
		Count:   len(species),
	})
}

// ConfirmSpecies toggles a species in the confirmed list (adds if absent, removes if present).
// The confirmed list is analytics-only and does not affect detection processing, so no
// settings side-effects are triggered.
func (c *Handler) ConfirmSpecies(ctx echo.Context) error {
	return c.toggleManagedSpecies(ctx,
		func(s *conf.Settings) []string { return s.Realtime.Species.Confirmed },
		func(s *conf.Settings, list []string) { s.Realtime.Species.Confirmed = list },
		false, "confirm")
}

// GetConfirmedSpecies returns the confirmed species list.
func (c *Handler) GetConfirmedSpecies(ctx echo.Context) error {
	species := nonNilSpeciesList(c.getSettingsOrFallback().Realtime.Species.Confirmed)

	return ctx.JSON(http.StatusOK, SpeciesListResponse{
		Species: species,
		Count:   len(species),
	})
}

// nonNilSpeciesList clones list, substituting a non-nil empty slice when list
// is nil. slices.Clone preserves nilness, which would otherwise encode an
// unset list as JSON null instead of [], inconsistent with GetExcludedSpecies
// (which builds its response via make([]string, len(...))).
func nonNilSpeciesList(list []string) []string {
	if list == nil {
		return []string{}
	}
	return slices.Clone(list)
}

// toggleManagedSpecies binds and validates the request, toggles the species in
// the list selected by listOf/setList, and returns the standard toggle response.
func (c *Handler) toggleManagedSpecies(ctx echo.Context, listOf func(*conf.Settings) []string, setList func(*conf.Settings, []string), triggerSideEffects bool, opLabel string) error {
	req := &SpeciesListRequest{}
	if err := ctx.Bind(req); err != nil {
		return c.HandleError(ctx, err, "Invalid request format", http.StatusBadRequest)
	}
	req.CommonName = strings.TrimSpace(req.CommonName)
	if req.CommonName == "" {
		return c.HandleError(ctx, nil, "Missing species name", http.StatusBadRequest)
	}

	action, present, err := c.toggleSpeciesInList(req.CommonName, listOf, setList, triggerSideEffects)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to update species list", http.StatusInternalServerError)
	}

	c.LogInfoIfEnabled("Species "+opLabel+" toggled",
		logger.String("species", req.CommonName),
		logger.String("action", action),
		logger.Bool("present", present),
		logger.String("ip", ctx.RealIP()),
	)

	return ctx.JSON(http.StatusOK, SpeciesListToggleResponse{
		CommonName: req.CommonName,
		Action:     action,
		Present:    present,
	})
}

// toggleSpeciesInList toggles a species in one of the realtime species lists
// (e.g. include or confirmed) under the settings mutex, persisting the change
// through the standard publish/save path. listOf reads the current slice and
// setList writes the updated slice back onto a cloned Settings. When
// triggerSideEffects is true, settings change side-effects (e.g. range filter
// rebuild) are triggered after saving. Returns the action ("added"/"removed")
// and the resulting membership state.
func (c *Handler) toggleSpeciesInList(species string, listOf func(*conf.Settings) []string, setList func(*conf.Settings, []string), triggerSideEffects bool) (action string, present bool, err error) {
	if species == "" {
		return "", false, nil
	}

	// Serialise this read-modify-write against concurrent settings saves so an
	// out-of-band StoreSettings cannot interleave between read and publish.
	c.settingsMutex.Lock()
	defer c.settingsMutex.Unlock()

	current := c.getSettingsOrFallback()

	// Resolve the incoming name to its scientific form for MATCHING purposes
	// only (mirrors the Exclude list's resolveExcludeName/excludeEntryMatches
	// convention: canonicalize the target once, then compare each stored entry
	// against it, which also resolves the entry's own alias). This catches an
	// entry stored as the scientific name (e.g. typed directly into the
	// Settings editor) against an incoming current-locale common name, or vice
	// versa - not just a case difference. It is comparison-only: the value
	// actually stored (below, on add) is still the caller's verbatim `species`
	// string, not this resolved form, so Include's range-filter override
	// (internal/classifier/range_filter.go resolveOverrideLabels) keeps
	// operating on unchanged stored strings; Confirmed has no other consumer
	// so this is purely a UX improvement there. Known residual limitation: the
	// common-to-scientific map reflects only the currently active BirdNET
	// locale (name_maps.go buildNameMaps), so an entry stored as a common name
	// under a locale that is no longer active still won't resolve - only a
	// same-locale common/scientific mismatch is fixed here.
	canonicalTarget := c.resolveExcludeName(species)
	matchesTarget := func(s string) bool { return c.excludeEntryMatches(s, canonicalTarget) }

	wasPresent := slices.ContainsFunc(listOf(current), matchesTarget)

	updated := conf.CloneSettings(current)
	if wasPresent {
		setList(updated, slices.DeleteFunc(listOf(updated), matchesTarget))
		action = speciesActionRemoved
		present = false
	} else {
		setList(updated, append(listOf(updated), species))
		action = speciesActionAdded
		present = true
	}

	if err := c.publishAndSaveSettings(current, updated); err != nil {
		return "", wasPresent, err
	}

	if triggerSideEffects {
		if handleErr := c.handleSettingsChanges(current, updated); handleErr != nil {
			apicore.GetLogger().Warn("Failed to trigger settings side-effects after species list change",
				logger.Error(handleErr),
				logger.String("species", species),
				logger.String("action", action))
		}
	}

	return action, present, nil
}

// SpeciesDeleteRequest is the request body for deleting all detections of a species.
// ExcludeIDs are IDs the caller already knows are permanently un-deletable
// (reported as SkippedIDs by an earlier call in the same delete operation);
// see SpeciesDeleteResult for why the caller should accumulate and resend them.
type SpeciesDeleteRequest struct {
	ScientificName string   `json:"scientific_name"`
	ExcludeIDs     []string `json:"exclude_ids,omitempty"`
}

// SpeciesDeleteResult reports the outcome of a species-wide delete. A single
// call processes at most maxBatchSize detections (the deleteNotesByIDs loop is
// a per-row Get+Delete, so an unbounded species-wide delete on a common species
// with tens of thousands of detections could hold the request - and, on
// SQLite, its single-writer lock - for minutes).
//
// Remaining reports how many matching detections (after ExcludeIDs filtering)
// were not attempted this call; a non-zero Remaining means the caller should
// invoke the endpoint again to delete the rest. Locked detections are never
// removed, so GetSpeciesNoteIDs keeps returning them on every call - the
// caller MUST accumulate SkippedIDs across calls and resend the full
// accumulated set as ExcludeIDs on the next call. Without that, chunk
// selection (always the front of the current ID list) would repeatedly
// re-examine the same locked entries: Remaining would never reach 0 (an
// infinite loop) if not accounted for, and even a caller that stopped after
// one such all-skipped chunk would never reach later, genuinely deletable
// detections. Because both the deleted and the excluded portion of a chunk
// are permanently removed from consideration (deleted rows physically, locked
// rows via the accumulated exclusion), the set of not-yet-considered IDs
// shrinks by up to maxBatchSize on every call regardless of how many of them
// were locked, so a conforming caller reaches Remaining == 0 in a bounded
// number of calls (ceil(total IDs / maxBatchSize)) with every ID examined
// exactly once. The species workspace's remove() implements this accumulation.
type SpeciesDeleteResult struct {
	Deleted    int      `json:"deleted"`
	Skipped    int      `json:"skipped"`
	Remaining  int      `json:"remaining"`
	SkippedIDs []string `json:"skipped_ids,omitempty"`
}

// speciesNoteIDsDatastore is the optional datastore capability required to resolve
// every detection ID for a species. Datastores that do not implement it cause
// DeleteSpeciesDetections to return HTTP 501.
type speciesNoteIDsDatastore interface {
	GetSpeciesNoteIDs(ctx context.Context, scientificName string) ([]string, error)
}

// DeleteSpeciesDetections deletes up to maxBatchSize (unlocked) detections for
// the given scientific name, excluding any IDs the caller reports via
// ExcludeIDs. Locked detections are skipped and counted, mirroring the batch
// delete semantics. Callers must repeat the request - accumulating each
// response's SkippedIDs into the next call's ExcludeIDs - while the response's
// Remaining is non-zero, to delete the rest and reach every deletable
// detection regardless of how locked ones are distributed. Returns HTTP 501
// when the active datastore cannot resolve a species' detection IDs.
func (c *Handler) DeleteSpeciesDetections(ctx echo.Context) error {
	var req SpeciesDeleteRequest
	if err := ctx.Bind(&req); err != nil {
		return c.HandleError(ctx, err, "Invalid request format", http.StatusBadRequest)
	}
	req.ScientificName = strings.TrimSpace(req.ScientificName)
	if req.ScientificName == "" {
		return c.HandleError(ctx, fmt.Errorf("no scientific name provided"),
			"Scientific name is required", http.StatusBadRequest)
	}

	ds, ok := c.DS.(speciesNoteIDsDatastore)
	if !ok {
		return c.HandleError(ctx, fmt.Errorf("datastore does not support species note lookup"),
			"Species deletion is not supported by the active datastore", http.StatusNotImplemented)
	}

	ids, err := ds.GetSpeciesNoteIDs(ctx.Request().Context(), req.ScientificName)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to look up detections for species", http.StatusInternalServerError)
	}

	unique := deduplicateIDs(ids)
	if len(req.ExcludeIDs) > 0 {
		exclude := make(map[string]struct{}, len(req.ExcludeIDs))
		for _, id := range req.ExcludeIDs {
			exclude[id] = struct{}{}
		}
		unique = slices.DeleteFunc(unique, func(id string) bool {
			_, excluded := exclude[id]
			return excluded
		})
	}

	chunk, remaining := unique, 0
	if len(unique) > maxBatchSize {
		chunk, remaining = unique[:maxBatchSize], len(unique)-maxBatchSize
	}

	deleted, skipped, skippedIDs := c.deleteNotesByIDs(chunk)

	c.invalidateDetectionCache()

	c.LogInfoIfEnabled("Species detections deleted",
		logger.String("scientific_name", req.ScientificName),
		logger.Int("deleted", deleted),
		logger.Int("skipped", skipped),
		logger.Int("remaining", remaining),
		logger.String("ip", ctx.RealIP()))

	return ctx.JSON(http.StatusOK, SpeciesDeleteResult{
		Deleted:    deleted,
		Skipped:    skipped,
		Remaining:  remaining,
		SkippedIDs: skippedIDs,
	})
}

// annotateAudioAvailability sets AudioAvailable on each detection by checking
// its clip on disk. Opt-in (includeAudioAvailability=true) because it performs
// one stat per row; detections and notes must be index-aligned.
func (c *Handler) annotateAudioAvailability(notes []datastore.Note, detections []DetectionResponse) error {
	exportPath := c.CurrentSettings().Realtime.Audio.Export.Path
	for i := range detections {
		available := false
		clipPath, valid := apicore.NormalizeClipPathStrict(notes[i].ClipName, exportPath)
		if valid && clipPath != "" {
			info, err := c.SFS.StatRel(clipPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			available = err == nil && info.Mode().IsRegular() && info.Size() > 0
		}
		detections[i].AudioAvailable = &available
	}
	return nil
}
