package analytics

import (
	"context"
	"io/fs"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/errors"
)

// registerSpeciesToolsRoutes registers the protected species workspace endpoints.
func (c *Handler) registerSpeciesToolsRoutes(speciesGroup *echo.Group) {
	speciesGroup.GET("/tools", c.GetSpeciesTools, c.AuthMiddleware)
	speciesGroup.GET("/tools/recordings", c.GetSpeciesToolRecordings, c.AuthMiddleware)
	speciesGroup.GET("/tools/observation-link", c.GetObservationLink, c.AuthMiddleware)
	speciesGroup.GET("/review-stats", c.GetSpeciesReviewStats, c.AuthMiddleware)
}

type speciesToolsStore interface {
	GetSpeciesTools(context.Context, []string) ([]datastore.SpeciesToolRow, error)
	GetSpeciesToolRecordings(context.Context, []string, int) ([]datastore.SpeciesRecordingCandidate, error)
}

// GetSpeciesTools returns a complete inventory with an optional metric projection.
func (c *Handler) GetSpeciesTools(ctx echo.Context) error {
	store, ok := c.DS.(speciesToolsStore)
	if !ok {
		return c.HandleError(ctx, nil, "Species tools unavailable", http.StatusNotImplemented)
	}
	fields := []string{}
	if raw := ctx.QueryParam("fields"); raw != "" {
		fields = strings.Split(raw, ",")
	}
	if _, err := datastore.SelectSpeciesToolFields(fields, "", "", ""); err != nil {
		return c.HandleError(ctx, err, "Invalid species tools fields", http.StatusBadRequest)
	}
	queryCtx, cancel := withAnalyticsTimeout(ctx)
	defer cancel()
	rows, err := store.GetSpeciesTools(queryCtx, fields)
	if err != nil {
		return c.handleAnalyticsQueryError(ctx, err, "Species tools", "Failed to load species")
	}
	return ctx.JSON(http.StatusOK, rows)
}

// GetSpeciesToolRecordings returns the preferred available recording for a bounded group of species.
func (c *Handler) GetSpeciesToolRecordings(ctx echo.Context) error {
	const maxSpecies = 25
	const pageSize = 200
	store, ok := c.DS.(speciesToolsStore)
	if !ok || c.SFS == nil {
		return c.HandleError(ctx, nil, "Recording lookup unavailable", http.StatusServiceUnavailable)
	}
	names := ctx.QueryParams()["species"]
	if len(names) == 0 || len(names) > maxSpecies {
		return c.HandleError(ctx, nil, "Expected 1 to 25 species", http.StatusBadRequest)
	}
	result := make(map[string]*datastore.SpeciesRecordingCandidate, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return c.HandleError(ctx, nil, "Empty species", http.StatusBadRequest)
		}
		result[name] = nil
	}
	queryCtx, cancel := withAnalyticsTimeout(ctx)
	defer cancel()
	remaining := len(result)
	for offset := 0; remaining > 0; {
		before := remaining
		candidates, err := store.GetSpeciesToolRecordings(queryCtx, names, offset)
		if err != nil {
			return c.handleAnalyticsQueryError(ctx, err, "Species recordings", "Failed to find recordings")
		}
		for _, candidate := range candidates {
			if err := queryCtx.Err(); err != nil {
				return c.handleAnalyticsQueryError(ctx, err, "Species recordings", "Recording lookup timed out")
			}
			if selected, requested := result[candidate.ScientificName]; !requested || selected != nil {
				continue
			}
			clipPath, valid := apicore.NormalizeClipPathStrict(candidate.ClipName, c.CurrentSettings().Realtime.Audio.Export.Path)
			if !valid || clipPath == "" {
				continue
			}
			info, err := c.SFS.StatRel(clipPath)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return c.HandleError(ctx, err, "Failed to check recording availability", http.StatusInternalServerError)
			}
			if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
				result[candidate.ScientificName] = &candidate
				remaining--
			}
		}
		if len(candidates) < pageSize {
			break
		}
		if remaining < before {
			// A new filter changes page boundaries. Restart only the unresolved
			// species so a common species with a chosen clip cannot dominate later pages.
			names = names[:0]
			for name, candidate := range result {
				if candidate == nil {
					names = append(names, name)
				}
			}
			offset = 0
		} else {
			offset += pageSize
		}
	}
	return ctx.JSON(http.StatusOK, result)
}
