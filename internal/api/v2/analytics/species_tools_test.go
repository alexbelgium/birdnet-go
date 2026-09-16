package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/securefs"
)

type speciesToolsTestStore struct {
	datastore.Interface
	candidates []datastore.SpeciesRecordingCandidate
	calls      [][]string
}

func (s *speciesToolsTestStore) GetSpeciesTools(context.Context, []string) ([]datastore.SpeciesToolRow, error) {
	return nil, nil
}
func (s *speciesToolsTestStore) GetSpeciesToolRecordings(_ context.Context, names []string, offset int) ([]datastore.SpeciesRecordingCandidate, error) {
	s.calls = append(s.calls, append([]string(nil), names...))
	var matching []datastore.SpeciesRecordingCandidate
	for _, candidate := range s.candidates {
		for _, name := range names {
			if candidate.ScientificName == name {
				matching = append(matching, candidate)
				break
			}
		}
	}
	if offset >= len(matching) {
		return nil, nil
	}
	end := min(offset+200, len(matching))
	return matching[offset:end], nil
}

func TestSpeciesToolsAvailableRecordingFallback(t *testing.T) {
	e, _, handler := setupAnalyticsTestEnvironment(t)
	dir := t.TempDir()
	for _, name := range []string{"locked.wav", "highest.wav", "other.wav"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("audio"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.wav"), nil, 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "directory.wav"), 0o700))
	sfs, err := securefs.New(dir)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sfs.Close()) })
	handler.SFS = sfs
	store := &speciesToolsTestStore{candidates: []datastore.SpeciesRecordingCandidate{
		{ID: 1, ScientificName: "Turdus merula", Locked: true, Confidence: 0.95, ClipName: "missing.wav"},
		{ID: 2, ScientificName: "Turdus merula", Locked: true, Confidence: 0.8, ClipName: "clips/locked.wav"},
		{ID: 3, ScientificName: "Turdus merula", Confidence: 0.99, ClipName: "highest.wav"},
		{ID: 6, ScientificName: "Tyto alba", Locked: true, Confidence: 1, ClipName: "missing.wav"},
		{ID: 7, ScientificName: "Tyto alba", Locked: true, Confidence: 0.99, ClipName: "empty.wav"},
		{ID: 8, ScientificName: "Tyto alba", Locked: true, Confidence: 0.98, ClipName: "directory.wav"},
		{ID: 4, ScientificName: "Tyto alba", Confidence: 0.9, ClipName: "other.wav"},
		{ID: 5, ScientificName: "Unknown", Locked: true, Confidence: 1, ClipName: "../unsafe.wav"},
	}}
	handler.DS = store
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/?species=Turdus%20merula&species=Tyto%20alba&species=Unknown", http.NoBody), recorder)
	require.NoError(t, handler.GetSpeciesToolRecordings(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	var result map[string]*datastore.SpeciesRecordingCandidate
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.NotNil(t, result["Turdus merula"])
	assert.Equal(t, uint(2), result["Turdus merula"].ID)
	require.NotNil(t, result["Tyto alba"])
	assert.Equal(t, uint(4), result["Tyto alba"].ID)
	assert.Nil(t, result["Unknown"])
	// Choosing one species must not alias the pointer subsequently assigned for another.
	assert.NotSame(t, result["Turdus merula"], result["Tyto alba"])
}

func TestSpeciesToolsRecordingPaginationDropsResolvedSpecies(t *testing.T) {
	e, _, handler := setupAnalyticsTestEnvironment(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "valid.wav"), []byte("audio"), 0o600))
	sfs, err := securefs.New(dir)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, sfs.Close()) })
	handler.SFS = sfs
	store := &speciesToolsTestStore{}
	for i := range 201 {
		store.candidates = append(store.candidates, datastore.SpeciesRecordingCandidate{ID: uint(i + 1), ScientificName: "Turdus merula", ClipName: "valid.wav"})
	}
	store.candidates = append(store.candidates, datastore.SpeciesRecordingCandidate{ID: 202, ScientificName: "Tyto alba", ClipName: "valid.wav"})
	handler.DS = store
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/?species=Turdus%20merula&species=Tyto%20alba", http.NoBody), recorder)
	require.NoError(t, handler.GetSpeciesToolRecordings(ctx))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, store.calls, 2)
	assert.Equal(t, []string{"Tyto alba"}, store.calls[1])
	assert.Contains(t, recorder.Body.String(), `"id":202`)
}
