package detections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/mocks"
)

// speciesIDsDS adds the optional species-ID lookup to the generated mock.
type speciesIDsDS struct {
	*mocks.MockInterface
	ids []string
}

func (d *speciesIDsDS) GetSpeciesNoteIDs(context.Context, string) ([]string, error) {
	return d.ids, nil
}

func TestDeleteSpeciesDetections(t *testing.T) {
	e, mockDS, controller := setupTestEnvironment(t)
	controller.DS = &speciesIDsDS{MockInterface: mockDS, ids: []string{"1", "2", "3", "4", "5"}}

	mockDS.On("Get", "1").Return(datastore.Note{ID: 1, ScientificName: "Turdus merula"}, nil)
	mockDS.On("Delete", "1").Return(nil)
	mockDS.On("Get", "2").Return(datastore.Note{ID: 2, ScientificName: "Turdus merula", Locked: true}, nil)
	// Corrected to another species after the ID lookup: must not be deleted.
	mockDS.On("Get", "3").Return(datastore.Note{ID: 3, ScientificName: "Parus major"}, nil)
	// Transient failures are reported, not excluded, so they can be retried.
	mockDS.On("Get", "4").Return(datastore.Note{}, errors.New("database is locked"))
	mockDS.On("Get", "5").Return(datastore.Note{ID: 5, ScientificName: "Turdus merula"}, nil)
	mockDS.On("Delete", "5").Return(errors.New("database is locked"))

	body, err := json.Marshal(SpeciesDeleteRequest{ScientificName: "Turdus merula"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/species/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	require.NoError(t, controller.DeleteSpeciesDetections(e.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	var result SpeciesDeleteResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, 2, result.Skipped)
	assert.Equal(t, 2, result.Failed)
	assert.Equal(t, 0, result.Remaining)
	assert.ElementsMatch(t, []string{"2", "3"}, result.SkippedIDs)
	mockDS.AssertNotCalled(t, "Delete", "3")
}
