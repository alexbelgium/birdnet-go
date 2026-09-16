package detections

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// WorkspaceSpeciesResponse is one inventory row. Total includes false positives:
// it is the number a species delete acts on, of which Locked are kept.
type WorkspaceSpeciesResponse struct {
	ScientificName string `json:"scientificName"`
	CommonName     string `json:"commonName"`
	SpeciesCode    string `json:"speciesCode"`
	Total          int64  `json:"total"`
	Locked         int64  `json:"locked"`
	FirstSeen      string `json:"firstSeen"`
	LastSeen       string `json:"lastSeen"`
}

// WorkspaceSpeciesStatsResponse holds review counts and the max confidence of
// non-false-positive detections that have a recording.
type WorkspaceSpeciesStatsResponse struct {
	ScientificName string   `json:"scientificName"`
	Correct        int64    `json:"correct"`
	FalsePositive  int64    `json:"falsePositive"`
	MaxConfidence  *float64 `json:"maxConfidence"`
}

// formatWorkspaceTime renders a timestamp as RFC3339, or "" when unknown.
func formatWorkspaceTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// GetWorkspaceSpecies handles GET /api/v2/species-workspace/species[?species=].
func (c *Handler) GetWorkspaceSpecies(ctx echo.Context) error {
	store, err := c.workspaceStore(ctx)
	if store == nil {
		return err
	}
	queryCtx, cancel := workspaceContext(ctx)
	defer cancel()
	rows, err := store.SpeciesWorkspaceInventory(queryCtx, strings.TrimSpace(ctx.QueryParam("species")))
	if err != nil {
		return c.HandleError(ctx, err, "Failed to load species", http.StatusInternalServerError)
	}
	out := make([]WorkspaceSpeciesResponse, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		out = append(out, WorkspaceSpeciesResponse{
			ScientificName: r.ScientificName,
			CommonName:     r.CommonName,
			SpeciesCode:    r.SpeciesCode,
			Total:          r.Total,
			Locked:         r.Locked,
			FirstSeen:      formatWorkspaceTime(r.FirstSeen),
			LastSeen:       formatWorkspaceTime(r.LastSeen),
		})
	}
	return ctx.JSON(http.StatusOK, out)
}

// GetWorkspaceSpeciesStats handles GET /api/v2/species-workspace/species/stats.
func (c *Handler) GetWorkspaceSpeciesStats(ctx echo.Context) error {
	store, err := c.workspaceStore(ctx)
	if store == nil {
		return err
	}
	queryCtx, cancel := workspaceContext(ctx)
	defer cancel()
	stats, err := store.SpeciesWorkspaceStats(queryCtx)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to load species statistics", http.StatusInternalServerError)
	}
	out := make([]WorkspaceSpeciesStatsResponse, 0, len(stats))
	for _, s := range stats {
		out = append(out, WorkspaceSpeciesStatsResponse(s))
	}
	return ctx.JSON(http.StatusOK, out)
}
