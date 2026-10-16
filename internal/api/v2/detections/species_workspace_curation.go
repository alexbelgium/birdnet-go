package detections

import (
	"net/http"
	"slices"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// Membership kinds managed by the species workspace.
const (
	membershipConfirmed = "confirmed"
	membershipIncluded  = "included"
	membershipExcluded  = "excluded"
)

// speciesDeleteChunk bounds how many detections one delete request removes, so
// each request stays well under client timeouts (the batch endpoints use the same bound).
const speciesDeleteChunk = maxBatchSize

// WorkspaceMembershipsResponse lists every membership by scientific name.
type WorkspaceMembershipsResponse struct {
	Confirmed []string `json:"confirmed"`
	Included  []string `json:"included"`
	Excluded  []string `json:"excluded"`
}

// WorkspaceMembershipRequest sets whether a species belongs to a list.
type WorkspaceMembershipRequest struct {
	ScientificName string `json:"scientificName"`
	Present        bool   `json:"present"`
}

// WorkspaceMembershipResponse is the authoritative state after an update.
type WorkspaceMembershipResponse struct {
	Kind           string `json:"kind"`
	ScientificName string `json:"scientificName"`
	Present        bool   `json:"present"`
}

// WorkspaceDeleteRequest deletes the next chunk of a species' unlocked detections.
type WorkspaceDeleteRequest struct {
	ScientificName string `json:"scientificName"`
}

// WorkspaceDeleteResponse reports one chunk. Locked and Reassigned count
// detections kept because they were locked or moved to another species while the
// chunk ran. Remaining counts unlocked detections of the species still stored; a
// client repeats while it is above zero. A datastore failure fails the request
// (nothing is marked skipped), so the client can simply retry.
type WorkspaceDeleteResponse struct {
	Deleted    int   `json:"deleted"`
	Locked     int   `json:"locked"`
	Reassigned int   `json:"reassigned"`
	Remaining  int64 `json:"remaining"`
}

// membershipList returns accessors for one managed list; ok is false for an unknown kind.
func membershipList(kind string) (get func(*conf.Settings) []string, set func(*conf.Settings, []string), sideEffects, ok bool) {
	switch kind {
	case membershipConfirmed:
		return func(s *conf.Settings) []string { return s.Realtime.Species.Confirmed },
			func(s *conf.Settings, l []string) { s.Realtime.Species.Confirmed = l }, false, true
	case membershipIncluded:
		return func(s *conf.Settings) []string { return s.Realtime.Species.Include },
			func(s *conf.Settings, l []string) { s.Realtime.Species.Include = l }, true, true
	case membershipExcluded:
		return func(s *conf.Settings) []string { return s.Realtime.Species.Exclude },
			func(s *conf.Settings, l []string) { s.Realtime.Species.Exclude = l }, true, true
	}
	return nil, nil, false, false
}

// canonicalMembers resolves stored entries (scientific or common names in any
// case) to scientific names, de-duplicated in first-seen order.
func (c *Handler) canonicalMembers(list []string) []string {
	out := make([]string, 0, len(list))
	for _, entry := range list {
		name := c.resolveExcludeName(strings.TrimSpace(entry))
		if name != "" && !slices.ContainsFunc(out, func(s string) bool { return strings.EqualFold(s, name) }) {
			out = append(out, name)
		}
	}
	return out
}

// GetWorkspaceMemberships handles GET /api/v2/species-workspace/memberships.
func (c *Handler) GetWorkspaceMemberships(ctx echo.Context) error {
	s := c.getSettingsOrFallback()
	return ctx.JSON(http.StatusOK, WorkspaceMembershipsResponse{
		Confirmed: c.canonicalMembers(s.Realtime.Species.Confirmed),
		Included:  c.canonicalMembers(s.Realtime.Species.Include),
		Excluded:  c.canonicalMembers(s.Realtime.Species.Exclude),
	})
}

// SetWorkspaceMembership handles PUT /api/v2/species-workspace/memberships/:kind.
// It is idempotent: setting the current state again changes nothing.
func (c *Handler) SetWorkspaceMembership(ctx echo.Context) error {
	kind := ctx.Param("kind")
	get, set, sideEffects, ok := membershipList(kind)
	if !ok {
		return c.HandleError(ctx, nil, "Unknown membership list", http.StatusBadRequest)
	}
	var req WorkspaceMembershipRequest
	if err := ctx.Bind(&req); err != nil {
		return c.HandleError(ctx, err, "Invalid request format", http.StatusBadRequest)
	}
	name := c.resolveExcludeName(strings.TrimSpace(req.ScientificName))
	if name == "" {
		return c.HandleError(ctx, nil, "Missing species name", http.StatusBadRequest)
	}

	c.settingsMutex.Lock()
	defer c.settingsMutex.Unlock()
	current := c.getSettingsOrFallback()
	matches := func(entry string) bool { return c.excludeEntryMatches(entry, name) }
	if slices.ContainsFunc(get(current), matches) == req.Present {
		return ctx.JSON(http.StatusOK, WorkspaceMembershipResponse{Kind: kind, ScientificName: name, Present: req.Present})
	}
	updated := conf.CloneSettings(current)
	if req.Present {
		set(updated, append(get(updated), name))
	} else {
		set(updated, slices.DeleteFunc(get(updated), matches))
	}
	if err := c.publishAndSaveSettings(current, updated); err != nil {
		return c.HandleError(ctx, err, "Failed to update species list", http.StatusInternalServerError)
	}
	if sideEffects {
		if err := c.handleSettingsChanges(current, updated); err != nil {
			apicore.GetLogger().Warn("Failed to apply settings side-effects after species list change",
				logger.Error(err), logger.String("list", kind), logger.String("species", name))
		}
	}
	c.LogInfoIfEnabled("Species workspace membership updated",
		logger.String("list", kind), logger.String("species", name),
		logger.Bool("present", req.Present), logger.String("ip", ctx.RealIP()))
	return ctx.JSON(http.StatusOK, WorkspaceMembershipResponse{Kind: kind, ScientificName: name, Present: req.Present})
}

// DeleteWorkspaceSpeciesChunk handles POST /api/v2/species-workspace/species/delete.
// Each detection is deleted with a single conditional statement (still this
// species, not locked), so a concurrent lock or correction is never overridden.
func (c *Handler) DeleteWorkspaceSpeciesChunk(ctx echo.Context) error {
	store, err := c.workspaceStore(ctx)
	if store == nil {
		return err
	}
	var req WorkspaceDeleteRequest
	if err := ctx.Bind(&req); err != nil {
		return c.HandleError(ctx, err, "Invalid request format", http.StatusBadRequest)
	}
	req.ScientificName = strings.TrimSpace(req.ScientificName)
	if req.ScientificName == "" {
		return c.HandleError(ctx, nil, "Scientific name is required", http.StatusBadRequest)
	}
	queryCtx, cancel := workspaceContext(ctx)
	defer cancel()
	chunk, err := store.SpeciesWorkspaceDeleteChunk(queryCtx, req.ScientificName, speciesDeleteChunk)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to delete detections", http.StatusInternalServerError)
	}
	resp := WorkspaceDeleteResponse{
		Deleted:    len(chunk.Deleted),
		Locked:     chunk.Locked,
		Reassigned: chunk.Reassigned,
		Remaining:  chunk.Remaining,
	}
	if resp.Deleted > 0 {
		c.invalidateDetectionCache()
	}
	clipNames := make([]string, 0, len(chunk.Deleted))
	for _, d := range chunk.Deleted {
		if d.ClipName != "" {
			clipNames = append(clipNames, d.ClipName)
		}
	}
	c.removeClipFiles(clipNames)
	c.LogInfoIfEnabled("Species workspace delete chunk",
		logger.String("species", req.ScientificName), logger.Int("deleted", resp.Deleted),
		logger.Int("locked", resp.Locked), logger.Int("reassigned", resp.Reassigned),
		logger.Int64("remaining", resp.Remaining),
		logger.String("ip", ctx.RealIP()))
	return ctx.JSON(http.StatusOK, resp)
}

// GetWorkspaceLayout handles GET /api/v2/species-workspace/layout.
func (c *Handler) GetWorkspaceLayout(ctx echo.Context) error {
	return ctx.JSON(http.StatusOK, conf.NormalizeSpeciesWorkspaceLayout(c.getSettingsOrFallback().Realtime.Species.SpeciesWorkspace))
}

// PutWorkspaceLayout handles PUT /api/v2/species-workspace/layout and returns the saved (normalized) layout.
func (c *Handler) PutWorkspaceLayout(ctx echo.Context) error {
	var layout conf.SpeciesWorkspaceLayout
	if err := ctx.Bind(&layout); err != nil {
		return c.HandleError(ctx, err, "Invalid request format", http.StatusBadRequest)
	}
	layout = conf.NormalizeSpeciesWorkspaceLayout(layout)
	c.settingsMutex.Lock()
	defer c.settingsMutex.Unlock()
	current := c.getSettingsOrFallback()
	updated := conf.CloneSettings(current)
	updated.Realtime.Species.SpeciesWorkspace = layout
	if err := c.publishAndSaveSettings(current, updated); err != nil {
		return c.HandleError(ctx, err, "Failed to save layout", http.StatusInternalServerError)
	}
	return ctx.JSON(http.StatusOK, layout)
}
