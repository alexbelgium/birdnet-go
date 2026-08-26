// species_management_test.go: Tests for the species management endpoints
// (review-stats aggregation and whole-species deletion).

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/detections"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/mocks"
)

// speciesManagerMock adds the optional datastore.SpeciesManager capability on top of the
// generated MockInterface so the controller's type assertion succeeds in tests. Get/Delete
// continue to be driven by the embedded mock's expectations.
type speciesManagerMock struct {
	*mocks.MockInterface
	noteIDs    []string
	noteIDsErr error
	stats      []datastore.SpeciesReviewStats
	statsErr   error
}

func (m *speciesManagerMock) GetSpeciesNoteIDs(_ string) ([]string, error) {
	return m.noteIDs, m.noteIDsErr
}

func (m *speciesManagerMock) GetSpeciesReviewStats(_ context.Context) ([]datastore.SpeciesReviewStats, error) {
	return m.stats, m.statsErr
}

// TestDeleteSpeciesDetections covers the whole-species delete handler.
func TestDeleteSpeciesDetections(t *testing.T) {
	t.Run("deletes all unlocked detections and skips locked", func(t *testing.T) {
		e, mockDS, controller := setupTestEnvironment(t)
		// "1" deletes, "2" is locked (skipped).
		mockDS.On("Get", "1").Return(datastore.Note{ID: 1, Locked: false, ClipName: ""}, nil)
		mockDS.On("Delete", "1").Return(nil)
		mockDS.On("Get", "2").Return(datastore.Note{ID: 2, Locked: true}, nil)
		controller.DS = &speciesManagerMock{MockInterface: mockDS, noteIDs: []string{"1", "2"}}

		rec := doDeleteSpecies(t, e, controller, detections.DeleteSpeciesRequest{ScientificName: "Turdus migratorius"})
		assert.Equal(t, http.StatusOK, rec.Code)

		var result detections.DeleteSpeciesResult
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		assert.Equal(t, "Turdus migratorius", result.ScientificName)
		assert.Equal(t, 1, result.Deleted)
		assert.Equal(t, 1, result.Skipped)
	})

	t.Run("missing scientific name returns 400", func(t *testing.T) {
		e, mockDS, controller := setupTestEnvironment(t)
		controller.DS = &speciesManagerMock{MockInterface: mockDS}

		rec := doDeleteSpecies(t, e, controller, detections.DeleteSpeciesRequest{ScientificName: "   "})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("species with no detections returns 404", func(t *testing.T) {
		e, mockDS, controller := setupTestEnvironment(t)
		controller.DS = &speciesManagerMock{MockInterface: mockDS, noteIDs: []string{}}

		rec := doDeleteSpecies(t, e, controller, detections.DeleteSpeciesRequest{ScientificName: "Ghost bird"})
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("datastore without capability returns 501", func(t *testing.T) {
		e, _, controller := setupTestEnvironment(t)
		// controller.DS is the plain MockInterface, which does not implement SpeciesManager.

		rec := doDeleteSpecies(t, e, controller, detections.DeleteSpeciesRequest{ScientificName: "Turdus migratorius"})
		assert.Equal(t, http.StatusNotImplemented, rec.Code)
	})
}

// TestGetSpeciesReviewStats covers the review-stats handler.
func TestGetSpeciesReviewStats(t *testing.T) {
	t.Run("returns per-species review stats", func(t *testing.T) {
		e, mockDS, controller := setupTestEnvironment(t)
		controller.DS = &speciesManagerMock{
			MockInterface: mockDS,
			stats: []datastore.SpeciesReviewStats{
				{ScientificName: "Turdus migratorius", CommonName: "American Robin", Total: 2, Verified: 1, Rejected: 1},
			},
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v2/analytics/species/review-stats", http.NoBody)
		rec := httptest.NewRecorder()
		require.NoError(t, controller.analytics.GetSpeciesReviewStats(e.NewContext(req, rec)))
		assert.Equal(t, http.StatusOK, rec.Code)

		var stats []datastore.SpeciesReviewStats
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &stats))
		require.Len(t, stats, 1)
		assert.Equal(t, "Turdus migratorius", stats[0].ScientificName)
		assert.Equal(t, 2, stats[0].Total)
		assert.Equal(t, 1, stats[0].Verified)
		assert.Equal(t, 1, stats[0].Rejected)
	})

	t.Run("datastore without capability returns 501", func(t *testing.T) {
		e, _, controller := setupTestEnvironment(t)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/analytics/species/review-stats", http.NoBody)
		rec := httptest.NewRecorder()
		require.NoError(t, controller.analytics.GetSpeciesReviewStats(e.NewContext(req, rec)))
		assert.Equal(t, http.StatusNotImplemented, rec.Code)
	})
}

// doDeleteSpecies issues a species-delete request and returns the recorder.
func doDeleteSpecies(t *testing.T, e *echo.Echo, controller *Controller, body detections.DeleteSpeciesRequest) *httptest.ResponseRecorder {
	t.Helper()
	bodyBytes, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/species/delete", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	require.NoError(t, controller.detections.DeleteSpeciesDetections(e.NewContext(req, rec)))
	return rec
}
