package detections

import (
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/errors"
)

const (
	// maxBestRecordingSpecies bounds one best-recordings request.
	maxBestRecordingSpecies = 25
	// bestRecordingCandidates is how many candidates are checked on disk per species.
	bestRecordingCandidates = 10
	// defaultRecordingsPerPage and maxRecordingsPerPage bound the recordings listing.
	defaultRecordingsPerPage = 25
	maxRecordingsPerPage     = 100
)

// WorkspaceBestRecording is the recording shown for a species.
type WorkspaceBestRecording struct {
	ID         uint    `json:"id"`
	Confidence float64 `json:"confidence"`
	Locked     bool    `json:"locked"`
}

// WorkspaceRecordingResponse is a detection with its model name. The embedded
// DetectionResponse keeps the shape shared components already understand;
// Timestamp is replaced by the store's stored instant.
type WorkspaceRecordingResponse struct {
	DetectionResponse
	ModelName *string `json:"modelName"`
}

// WorkspaceRecordingsResponse is one page of a species' recordings.
type WorkspaceRecordingsResponse struct {
	Data       []WorkspaceRecordingResponse `json:"data"`
	Total      int64                        `json:"total"`
	Page       int                          `json:"page"`
	PerPage    int                          `json:"perPage"`
	TotalPages int                          `json:"totalPages"`
}

// clipAvailable reports whether a clip exists as a non-empty regular file.
func (c *Handler) clipAvailable(clipName string) (bool, error) {
	if c.SFS == nil {
		return false, nil
	}
	clipPath, valid := apicore.NormalizeClipPathStrict(clipName, c.CurrentSettings().Realtime.Audio.Export.Path)
	if !valid || clipPath == "" {
		return false, nil
	}
	info, err := c.SFS.StatRel(clipPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular() && info.Size() > 0, nil
}

// GetWorkspaceBestRecordings handles GET /api/v2/species-workspace/best-recordings?species=a&species=b.
// Each species maps to its best available recording or null: the highest
// confidence locked detection with an existing clip, else the highest confidence one.
func (c *Handler) GetWorkspaceBestRecordings(ctx echo.Context) error {
	store, err := c.workspaceStore(ctx)
	if store == nil {
		return err
	}
	names := slices.Compact(slices.Sorted(slices.Values(ctx.QueryParams()["species"])))
	if len(names) == 0 || len(names) > maxBestRecordingSpecies {
		return c.HandleError(ctx, nil, "Expected 1 to 25 species", http.StatusBadRequest)
	}
	queryCtx, cancel := workspaceContext(ctx)
	defer cancel()
	result := make(map[string]*WorkspaceBestRecording, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return c.HandleError(ctx, nil, "Empty species name", http.StatusBadRequest)
		}
		candidates, err := store.SpeciesWorkspaceCandidates(queryCtx, name, bestRecordingCandidates)
		if err != nil {
			return c.HandleError(ctx, err, "Failed to find recordings", http.StatusInternalServerError)
		}
		result[name] = nil
		for _, cand := range candidates {
			ok, err := c.clipAvailable(cand.ClipName)
			if err != nil {
				return c.HandleError(ctx, err, "Failed to check recording availability", http.StatusInternalServerError)
			}
			if ok {
				result[name] = &WorkspaceBestRecording{ID: cand.ID, Confidence: cand.Confidence, Locked: cand.Locked}
				break
			}
		}
	}
	return ctx.JSON(http.StatusOK, result)
}

// parseRecordingsQuery validates the recordings listing parameters.
func parseRecordingsQuery(ctx echo.Context) (datastore.SpeciesRecordingQuery, int, error) {
	q := datastore.SpeciesRecordingQuery{
		ScientificName: strings.TrimSpace(ctx.QueryParam("species")),
		SortBy:         ctx.QueryParam("sort"),
		LockedOnly:     ctx.QueryParam("locked") == "true",
		Limit:          defaultRecordingsPerPage,
	}
	if q.ScientificName == "" {
		return q, 0, errors.NewStd("species is required")
	}
	switch q.SortBy {
	case "":
		q.SortBy = datastore.SpeciesSortConfidenceDesc
	case datastore.SpeciesSortConfidenceDesc, datastore.SpeciesSortConfidenceAsc,
		datastore.SpeciesSortDateDesc, datastore.SpeciesSortDateAsc:
	default:
		return q, 0, errors.NewStd("invalid sort")
	}
	page := 1
	if raw := ctx.QueryParam("page"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 {
			return q, 0, errors.NewStd("invalid page")
		}
		page = p
	}
	if raw := ctx.QueryParam("perPage"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxRecordingsPerPage {
			return q, 0, errors.NewStd("invalid perPage")
		}
		q.Limit = n
	}
	q.Offset = (page - 1) * q.Limit
	return q, page, nil
}

// GetWorkspaceRecordings handles GET /api/v2/species-workspace/recordings.
func (c *Handler) GetWorkspaceRecordings(ctx echo.Context) error {
	store, err := c.workspaceStore(ctx)
	if store == nil {
		return err
	}
	q, page, err := parseRecordingsQuery(ctx)
	if err != nil {
		return c.HandleError(ctx, err, err.Error(), http.StatusBadRequest)
	}
	queryCtx, cancel := workspaceContext(ctx)
	defer cancel()
	recs, total, err := store.SpeciesWorkspaceRecordings(queryCtx, q)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to load recordings", http.StatusInternalServerError)
	}
	weatherCache := make(map[string][]datastore.HourlyWeather)
	out := WorkspaceRecordingsResponse{
		Data:    make([]WorkspaceRecordingResponse, 0, len(recs)),
		Total:   total,
		Page:    page,
		PerPage: q.Limit,
	}
	out.TotalPages = int((total + int64(q.Limit) - 1) / int64(q.Limit))
	for i := range recs {
		rec := &recs[i]
		resp := WorkspaceRecordingResponse{DetectionResponse: c.noteToDetectionResponse(&rec.Note, true, weatherCache)}
		if !rec.DetectedAt.IsZero() {
			resp.Timestamp = rec.DetectedAt.Format(time.RFC3339)
		}
		if rec.ModelName != "" {
			resp.ModelName = &rec.ModelName
		}
		out.Data = append(out.Data, resp)
	}
	return ctx.JSON(http.StatusOK, out)
}
