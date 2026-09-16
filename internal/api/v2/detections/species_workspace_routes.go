// species_workspace_routes.go: routes and datastore capability for the analytics
// species workspace (/ui/analytics/species-workspace).
//
// Every endpoint is protected. The datastore methods are an optional capability
// discovered by type assertion so the datastore Interface and its mocks are
// unchanged; a store without them answers 501.
package detections

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/datastore"
)

// speciesWorkspaceTimeout bounds each workspace request's datastore work.
const speciesWorkspaceTimeout = 30 * time.Second

// speciesWorkspaceStore is the datastore capability behind the workspace. Both
// datastore.DataStore (legacy) and v2only.Datastore implement it.
type speciesWorkspaceStore interface {
	SpeciesWorkspaceInventory(ctx context.Context, scientificName string) ([]datastore.SpeciesWorkspaceRow, error)
	SpeciesWorkspaceStats(ctx context.Context) ([]datastore.SpeciesWorkspaceStats, error)
	SpeciesWorkspaceCandidates(ctx context.Context, names []string, limit int) (map[string][]datastore.SpeciesRecordingCandidate, error)
	SpeciesWorkspaceRecordings(ctx context.Context, q datastore.SpeciesRecordingQuery) ([]datastore.SpeciesWorkspaceRecording, int64, error)
	SpeciesWorkspaceDeleteChunk(ctx context.Context, scientificName string, limit int) (datastore.SpeciesDeleteChunk, error)
}

// RegisterSpeciesWorkspaceRoutes registers /species-workspace/* on the API group.
func (c *Handler) RegisterSpeciesWorkspaceRoutes(g *echo.Group) {
	if c.DS == nil {
		return
	}
	ws := g.Group("/species-workspace", c.AuthMiddleware)
	ws.GET("/species", c.GetWorkspaceSpecies)
	ws.GET("/species/stats", c.GetWorkspaceSpeciesStats)
	ws.POST("/species/delete", c.DeleteWorkspaceSpeciesChunk)
	ws.GET("/memberships", c.GetWorkspaceMemberships)
	ws.PUT("/memberships/:kind", c.SetWorkspaceMembership)
	ws.GET("/best-recordings", c.GetWorkspaceBestRecordings)
	ws.GET("/recordings", c.GetWorkspaceRecordings)
	ws.GET("/layout", c.GetWorkspaceLayout)
	ws.PUT("/layout", c.PutWorkspaceLayout)
}

// workspaceStore returns the capability or writes a 501 response.
func (c *Handler) workspaceStore(ctx echo.Context) (speciesWorkspaceStore, error) {
	store, ok := c.DS.(speciesWorkspaceStore)
	if !ok {
		return nil, c.HandleError(ctx, nil, "Species workspace is not supported by the active datastore", http.StatusNotImplemented)
	}
	return store, nil
}

// workspaceContext derives a bounded context from the request.
func workspaceContext(ctx echo.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx.Request().Context(), speciesWorkspaceTimeout)
}
