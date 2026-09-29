package reanalyze_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/api/v2/reanalyze"
)

// registeredRoutes returns "METHOD PATH" for every route on e.
func registeredRoutes(e *echo.Echo) []string {
	routes := e.Routes()
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

func TestRegisterRoutes_RegistersBothEndpoints(t *testing.T) {
	core := apitest.NewCore(t)
	h := reanalyze.New(core)
	h.RegisterRoutes(core.Group)

	got := registeredRoutes(core.Echo)
	assert.Contains(t, got, "POST /api/v2/detections/:id/reanalyze")
	assert.Contains(t, got, "POST /api/v2/detections/:id/correct-species")
}

func TestRegisterRoutes_SkipsWhenDatastoreDisabled(t *testing.T) {
	// NewWithOptions permits a nil datastore ("datastore disabled" mode). Both
	// handlers dereference c.DS, so registering them there would turn a disabled
	// datastore into a panic on the first request instead of a 404.
	core := apitest.NewCore(t, apitest.WithDatastore(nil))
	h := reanalyze.New(core)
	h.RegisterRoutes(core.Group)

	for _, r := range registeredRoutes(core.Echo) {
		assert.NotContains(t, r, "/reanalyze")
		assert.NotContains(t, r, "/correct-species")
	}
}

// TestHandlers_RejectNonNumericIDBeforeTouchingDatastore pins the guard that
// keeps a wildcard route from reaching the datastore with garbage. apitest's core
// carries a strict mock (mockery asserts expectations on cleanup), so any call
// into c.DS here would fail the test on its own — the assertion is the 400 plus
// the absence of a datastore round trip.
func TestHandlers_RejectNonNumericIDBeforeTouchingDatastore(t *testing.T) {
	core := apitest.NewCore(t)
	h := reanalyze.New(core)

	for name, handler := range map[string]echo.HandlerFunc{
		"reanalyze":       h.ReanalyzeDetection,
		"correct-species": h.CorrectDetectionSpecies,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx := core.Echo.NewContext(
				httptest.NewRequest(http.MethodPost, "/", http.NoBody), rec)
			ctx.SetParamNames("id")
			ctx.SetParamValues("not-a-number")

			require.NoError(t, handler(ctx))
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}
