package detections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/mocks"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/securefs"
)

// fakeWorkspaceStore is a datastore mock that also implements the workspace capability.
type fakeWorkspaceStore struct {
	*mocks.MockInterface
	inventory   []datastore.SpeciesWorkspaceRow
	stats       []datastore.SpeciesWorkspaceStats
	candidates  map[string][]datastore.SpeciesRecordingCandidate
	recordings  []datastore.SpeciesWorkspaceRecording
	recTotal    int64
	lastQuery   datastore.SpeciesRecordingQuery
	deletable   []uint
	remaining   []int64 // returned by successive Deletable calls
	outcomes    map[uint]datastore.SpeciesDeleteOutcome
	deleteFails map[uint]bool
}

func (f *fakeWorkspaceStore) SpeciesWorkspaceInventory(_ context.Context, name string) ([]datastore.SpeciesWorkspaceRow, error) {
	if name == "" {
		return f.inventory, nil
	}
	var out []datastore.SpeciesWorkspaceRow
	for _, r := range f.inventory {
		if r.ScientificName == name {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeWorkspaceStore) SpeciesWorkspaceStats(context.Context) ([]datastore.SpeciesWorkspaceStats, error) {
	return f.stats, nil
}

func (f *fakeWorkspaceStore) SpeciesWorkspaceCandidates(_ context.Context, name string, _ int) ([]datastore.SpeciesRecordingCandidate, error) {
	return f.candidates[name], nil
}

func (f *fakeWorkspaceStore) SpeciesWorkspaceRecordings(_ context.Context, q datastore.SpeciesRecordingQuery) ([]datastore.SpeciesWorkspaceRecording, int64, error) {
	f.lastQuery = q
	return f.recordings, f.recTotal, nil
}

func (f *fakeWorkspaceStore) SpeciesWorkspaceDeletable(_ context.Context, _ string, _ int) (ids []uint, remaining int64, err error) {
	if len(f.remaining) > 0 {
		remaining, f.remaining = f.remaining[0], f.remaining[1:]
	}
	return f.deletable, remaining, nil
}

func (f *fakeWorkspaceStore) SpeciesWorkspaceDeleteDetection(_ context.Context, _ string, id uint) (datastore.SpeciesDeleteOutcome, string, error) {
	if f.deleteFails[id] {
		return 0, "", errors.NewStd("database is locked")
	}
	return f.outcomes[id], "", nil
}

// newWorkspaceTest wires a handler around store with the workspace routes registered.
func newWorkspaceTest(t *testing.T, store datastore.Interface, opts ...apitest.CoreOption) (*echo.Echo, *Handler) {
	t.Helper()
	e := echo.New()
	core := apitest.NewCore(t, append([]apitest.CoreOption{apitest.WithEcho(e), apitest.WithDatastore(store)}, opts...)...)
	core.AuthMiddleware = func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	h := buildTestHandler(t, core,
		map[string]string{"blackbird": "Turdus merula"},
		map[string]string{"Turdus merula": "Blackbird"})
	h.RegisterSpeciesWorkspaceRoutes(core.Group)
	return e, h
}

func doWorkspace(t *testing.T, e *echo.Echo, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, http.NoBody)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestSpeciesWorkspaceRoutesRequireAuth(t *testing.T) {
	t.Parallel()
	e := echo.New()
	core := apitest.NewCore(t, apitest.WithEcho(e))
	core.AuthMiddleware = func(echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error { return ctx.NoContent(http.StatusUnauthorized) }
	}
	h := buildTestHandler(t, core, nil, nil)
	h.RegisterSpeciesWorkspaceRoutes(core.Group)

	count := 0
	for _, r := range e.Routes() {
		// Echo adds a catch-all route for groups with middleware; skip it.
		if !strings.HasPrefix(r.Path, "/api/v2/species-workspace/") || strings.HasSuffix(r.Path, "*") {
			continue
		}
		count++
		path := strings.ReplaceAll(r.Path, ":kind", "confirmed")
		rec := doWorkspace(t, e, r.Method, path, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s must be protected", r.Method, r.Path)
	}
	assert.Equal(t, 9, count)
}

func TestSpeciesWorkspaceNotImplementedWithoutCapability(t *testing.T) {
	t.Parallel()
	e, _ := newWorkspaceTest(t, mocks.NewMockInterface(t))
	for _, target := range []string{"/api/v2/species-workspace/species", "/api/v2/species-workspace/species/stats",
		"/api/v2/species-workspace/best-recordings?species=A", "/api/v2/species-workspace/recordings?species=A"} {
		assert.Equal(t, http.StatusNotImplemented, doWorkspace(t, e, http.MethodGet, target, "").Code, target)
	}
}

func TestGetWorkspaceSpecies(t *testing.T) {
	t.Parallel()
	seen := time.Date(2025, 5, 1, 6, 30, 0, 0, time.UTC)
	store := &fakeWorkspaceStore{MockInterface: mocks.NewMockInterface(t), inventory: []datastore.SpeciesWorkspaceRow{
		{ScientificName: "Turdus merula", CommonName: "Blackbird", SpeciesCode: "eurbla", Total: 12, Locked: 2, FirstSeen: seen, LastSeen: seen},
		{ScientificName: "Strix aluco", Total: 1},
	}}
	e, _ := newWorkspaceTest(t, store)

	rec := doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/species", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var rows []WorkspaceSpeciesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	require.Len(t, rows, 2)
	assert.Equal(t, WorkspaceSpeciesResponse{ScientificName: "Turdus merula", CommonName: "Blackbird", SpeciesCode: "eurbla",
		Total: 12, Locked: 2, FirstSeen: "2025-05-01T06:30:00Z", LastSeen: "2025-05-01T06:30:00Z"}, rows[0])
	assert.Empty(t, rows[1].FirstSeen)

	rec = doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/species?species=Strix%20aluco", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	assert.Len(t, rows, 1)
}

func TestGetWorkspaceBestRecordings(t *testing.T) {
	t.Parallel()
	store := &fakeWorkspaceStore{MockInterface: mocks.NewMockInterface(t), candidates: map[string][]datastore.SpeciesRecordingCandidate{
		"Turdus merula": {
			{ID: 1, Confidence: 0.7, ClipName: "missing.wav", Locked: true},
			{ID: 2, Confidence: 0.9, ClipName: "present.wav"},
		},
		"Strix aluco": {{ID: 3, Confidence: 0.5, ClipName: "gone.wav"}},
	}}
	e, h := newWorkspaceTest(t, store)
	exportDir := h.CurrentSettings().Realtime.Audio.Export.Path
	require.NoError(t, os.WriteFile(filepath.Join(exportDir, "present.wav"), []byte("RIFF"), 0o600))
	sfs, err := securefs.New(exportDir)
	require.NoError(t, err)
	h.SFS = sfs

	rec := doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/best-recordings?species=Turdus%20merula&species=Strix%20aluco&species=Nobody", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got map[string]*WorkspaceBestRecording
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotNil(t, got["Turdus merula"])
	assert.Equal(t, uint(2), got["Turdus merula"].ID, "a missing locked clip falls through to the next available one")
	assert.Nil(t, got["Strix aluco"])
	assert.Contains(t, got, "Nobody")
	assert.Nil(t, got["Nobody"])

	assert.Equal(t, http.StatusBadRequest, doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/best-recordings", "").Code)
	params := make([]string, 0, maxBestRecordingSpecies+1)
	for i := range maxBestRecordingSpecies + 1 {
		params = append(params, "species=S"+strconv.Itoa(i))
	}
	tooMany := "/api/v2/species-workspace/best-recordings?" + strings.Join(params, "&")
	assert.Equal(t, http.StatusBadRequest, doWorkspace(t, e, http.MethodGet, tooMany, "").Code)
}

func TestGetWorkspaceRecordings(t *testing.T) {
	t.Parallel()
	at := time.Date(2025, 5, 2, 5, 14, 0, 0, time.UTC)
	store := &fakeWorkspaceStore{MockInterface: mocks.NewMockInterface(t), recTotal: 51, recordings: []datastore.SpeciesWorkspaceRecording{
		{Note: datastore.Note{ID: 7, ScientificName: "Turdus merula", Date: "2025-05-02", Time: "05:14:00", Confidence: 0.9}, DetectedAt: at, ModelName: "BirdNET"},
		{Note: datastore.Note{ID: 8, ScientificName: "Turdus merula", Date: "2025-05-01", Time: "05:00:00", Confidence: 0.8}},
	}}
	store.On("GetHourlyWeather", mock.Anything).Return([]datastore.HourlyWeather{}, nil).Maybe()
	e, _ := newWorkspaceTest(t, store)

	rec := doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/recordings?species=Turdus%20merula&page=3&perPage=25&sort=date_asc&locked=true", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, datastore.SpeciesRecordingQuery{ScientificName: "Turdus merula", SortBy: "date_asc", LockedOnly: true, Limit: 25, Offset: 50}, store.lastQuery)

	var got struct {
		Data []struct {
			ID        uint    `json:"id"`
			Timestamp string  `json:"timestamp"`
			ModelName *string `json:"modelName"`
		} `json:"data"`
		Total      int64 `json:"total"`
		Page       int   `json:"page"`
		TotalPages int   `json:"totalPages"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, int64(51), got.Total)
	assert.Equal(t, 3, got.Page)
	assert.Equal(t, 3, got.TotalPages)
	require.Len(t, got.Data, 2)
	assert.Equal(t, "2025-05-02T05:14:00Z", got.Data[0].Timestamp)
	require.NotNil(t, got.Data[0].ModelName)
	assert.Equal(t, "BirdNET", *got.Data[0].ModelName)
	assert.Nil(t, got.Data[1].ModelName, "an unknown model is null, not a guessed name")

	for _, bad := range []string{"", "?species=A&sort=random", "?species=A&page=0", "?species=A&perPage=500"} {
		assert.Equal(t, http.StatusBadRequest, doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/recordings"+bad, "").Code, bad)
	}

	rec = doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/recordings?species=A", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, datastore.SpeciesSortConfidenceDesc, store.lastQuery.SortBy, "default sort is confidence descending")
}

func TestDeleteWorkspaceSpeciesChunk(t *testing.T) {
	t.Parallel()
	store := &fakeWorkspaceStore{
		MockInterface: mocks.NewMockInterface(t),
		deletable:     []uint{1, 2, 3, 4, 5},
		remaining:     []int64{5, 2},
		outcomes: map[uint]datastore.SpeciesDeleteOutcome{
			1: datastore.SpeciesDeleteDeleted, 2: datastore.SpeciesDeleteLocked,
			3: datastore.SpeciesDeleteReassigned, 4: datastore.SpeciesDeleteDeleted,
		},
		deleteFails: map[uint]bool{5: true},
	}
	e, _ := newWorkspaceTest(t, store)

	rec := doWorkspace(t, e, http.MethodPost, "/api/v2/species-workspace/species/delete", `{"scientificName":"Turdus merula"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got WorkspaceDeleteResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, WorkspaceDeleteResponse{Deleted: 2, Locked: 1, Reassigned: 1, FailedIDs: []string{"5"}, Remaining: 2}, got,
		"only operational failures are reported as failed, and they are not excluded from later chunks")

	assert.Equal(t, http.StatusBadRequest, doWorkspace(t, e, http.MethodPost, "/api/v2/species-workspace/species/delete", `{"scientificName":"  "}`).Code)
}

func TestWorkspaceMemberships(t *testing.T) {
	t.Parallel()
	e, h := newWorkspaceTest(t, mocks.NewMockInterface(t), apitest.WithSettingsFunc(func(s *conf.Settings) {
		s.Realtime.Species.Include = []string{"Blackbird"}
		s.Realtime.Species.Confirmed = nil
	}))

	rec := doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/memberships", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var lists WorkspaceMembershipsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &lists))
	assert.Equal(t, []string{"Turdus merula"}, lists.Included, "stored common names are reported by scientific name")
	assert.Empty(t, lists.Confirmed)

	put := func(kind, body string) *httptest.ResponseRecorder {
		return doWorkspace(t, e, http.MethodPut, "/api/v2/species-workspace/memberships/"+kind, body)
	}
	require.Equal(t, http.StatusOK, put("confirmed", `{"scientificName":"Turdus merula","present":true}`).Code)
	require.Equal(t, http.StatusOK, put("confirmed", `{"scientificName":"Turdus merula","present":true}`).Code)
	assert.Equal(t, []string{"Turdus merula"}, h.getSettingsOrFallback().Realtime.Species.Confirmed, "setting the same state twice is idempotent")

	require.Equal(t, http.StatusOK, put("included", `{"scientificName":"Turdus merula","present":false}`).Code)
	assert.Empty(t, h.getSettingsOrFallback().Realtime.Species.Include, "removal matches the stored common-name alias")

	assert.Equal(t, http.StatusBadRequest, put("favourites", `{"scientificName":"Turdus merula","present":true}`).Code)
	assert.Equal(t, http.StatusBadRequest, put("confirmed", `{"scientificName":"","present":true}`).Code)
}

func TestWorkspaceLayout(t *testing.T) {
	t.Parallel()
	e, h := newWorkspaceTest(t, mocks.NewMockInterface(t))

	rec := doWorkspace(t, e, http.MethodGet, "/api/v2/species-workspace/layout", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var layout conf.SpeciesWorkspaceLayout
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &layout))
	assert.Equal(t, conf.DefaultSpeciesWorkspaceLayout(), layout)

	rec = doWorkspace(t, e, http.MethodPut, "/api/v2/species-workspace/layout",
		`{"columns":[{"id":"lastSeen","visible":true},{"id":"species","visible":false},{"id":"nope","visible":true}],"sort":{"column":"lastSeen","direction":"asc"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	saved := h.getSettingsOrFallback().Realtime.Species.SpeciesWorkspace
	assert.Equal(t, conf.SpeciesWorkspaceColumn{ID: "lastSeen", Visible: true}, saved.Columns[0])
	assert.Equal(t, conf.SpeciesWorkspaceColumn{ID: "species", Visible: true}, saved.Columns[1])
	assert.Equal(t, conf.SpeciesWorkspaceSort{Column: "lastSeen", Direction: "asc"}, saved.Sort)
}
