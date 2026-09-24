// Package reanalyze is the api/v2 reanalysis domain handler. It owns the two
// endpoints that let an operator ask "what does every classifier I have loaded
// think about this saved clip?" and then act on the answer:
//
//	POST /api/v2/detections/:id/reanalyze        (read-only second opinion)
//	POST /api/v2/detections/:id/correct-species  (apply the operator's choice)
//
// The package is deliberately self-contained. Everything it needs is either
// promoted from the embedded *apicore.Core or reached through already-exported
// leaf-package APIs, so adding the feature costs the shared codebase exactly one
// facade wiring block (import, field, constructor, initRoutes entry) and nothing
// else. In particular the correction write does NOT extend datastore.Interface or
// datastore.DetectionRepository: it composes the existing exported primitives
// (datastore.Interface.Transaction for legacy installs, the exported v2
// repositories for v2 installs) from inside this package. See correct.go.
package reanalyze

import (
	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
)

// Handler serves the api/v2 reanalysis endpoints. It embeds the shared
// *apicore.Core by pointer (never by value — Core holds atomics and mutexes)
// so DS, V2Manager, SFS, DetectionCache, AuthMiddleware and the
// HandleError/logging helpers promote onto it.
type Handler struct {
	*apicore.Core
}

// New constructs the reanalysis domain handler around the shared core.
func New(core *apicore.Core) *Handler {
	return &Handler{Core: core}
}

// RegisterRoutes registers the reanalysis endpoints on the supplied group.
//
// Both routes mutate or spend real CPU on behalf of an identified operator, so
// both sit behind c.AuthMiddleware in the same protected /detections group the
// review/lock/delete endpoints use. They are registered as a separate group from
// the detections domain's own protected group; Echo merges same-prefix groups by
// path, so the resulting route table is the union.
func (c *Handler) RegisterRoutes(g *echo.Group) {
	// Both handlers dereference c.DS. Honour the constructor's "datastore
	// disabled" mode (NewWithOptions permits a nil datastore) by not registering
	// handlers that would panic, mirroring RegisterDetectionRoutes.
	if c.DS == nil {
		c.LogWarnIfEnabled("Skipping reanalyze routes: datastore is not available")
		return
	}

	detectionGroup := g.Group("/detections", c.AuthMiddleware)
	detectionGroup.POST("/:id/reanalyze", c.ReanalyzeDetection)
	detectionGroup.POST("/:id/correct-species", c.CorrectDetectionSpecies)
}
