// detections_batch_test.go: Package api provides tests for API v2 batch detection endpoints.

package detections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/mocks"
)

// TestBatchDeleteDetections tests the BatchDeleteDetections endpoint.
func TestBatchDeleteDetections(t *testing.T) {
	e, mockDS, controller := setupTestEnvironment(t)

	testCases := []struct {
		name           string
		body           any
		mockSetup      func(*mock.Mock)
		expectedStatus int
		checkResult    func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "deletes unlocked and skips locked",
			body: BatchIDsRequest{IDs: []string{"1", "2"}},
			mockSetup: func(m *mock.Mock) {
				m.On("Get", "1").Return(datastore.Note{ID: 1, Locked: false, ClipName: ""}, nil)
				m.On("Delete", "1").Return(nil)
				m.On("Get", "2").Return(datastore.Note{ID: 2, Locked: true}, nil)
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 1, result.Processed)
				assert.Equal(t, 1, result.Skipped)
			},
		},
		{
			name: "skips not-found IDs",
			body: BatchIDsRequest{IDs: []string{"999"}},
			mockSetup: func(m *mock.Mock) {
				m.On("Get", "999").Return(datastore.Note{}, errors.New("record not found"))
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 0, result.Processed)
				assert.Equal(t, 1, result.Skipped)
			},
		},
		{
			name:           "empty IDs returns 400",
			body:           BatchIDsRequest{IDs: []string{}},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "exceeding max batch size returns 400",
			body:           BatchIDsRequest{IDs: make([]string, maxBatchSize+1)},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockDS.ExpectedCalls = nil
			mockDS.Calls = nil
			tc.mockSetup(&mockDS.Mock)

			bodyBytes, err := json.Marshal(tc.body)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/batch/delete", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err = controller.BatchDeleteDetections(c)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedStatus, rec.Code)

			if tc.checkResult != nil {
				tc.checkResult(t, rec)
			}

			mockDS.AssertExpectations(t)
		})
	}
}

// bulkDeleteMockDatastore embeds the standard mock datastore and adds the
// optional DeleteNotesByIDs bulk-delete capability (batchDeleteNotesDatastore),
// letting tests exercise the batched delete path in deleteNotesByIDs without
// hand-implementing the full datastore.Interface.
type bulkDeleteMockDatastore struct {
	*mocks.MockInterface
	deletedCount int
	clipNames    []string
	skipped      int
	err          error
	calledWith   []string
}

func (m *bulkDeleteMockDatastore) DeleteNotesByIDs(_ context.Context, ids []string) (int, []string, int, error) {
	m.calledWith = ids
	return m.deletedCount, m.clipNames, m.skipped, m.err
}

// TestDeleteNotesByIDs_UsesBulkPathWhenAvailable verifies that when the datastore
// implements batchDeleteNotesDatastore, BatchDeleteDetections resolves entirely
// through the single bulk call: the mock's per-ID Get/Delete are never invoked,
// and the response reflects the bulk call's counts.
func TestDeleteNotesByIDs_UsesBulkPathWhenAvailable(t *testing.T) {
	e := echo.New()
	bulkDS := &bulkDeleteMockDatastore{
		MockInterface: mocks.NewMockInterface(t),
		deletedCount:  2,
		clipNames:     []string{"2025/01/15/a.wav", "2025/01/15/b.wav"},
		skipped:       1,
	}
	core := apitest.NewCore(t, apitest.WithEcho(e), apitest.WithDatastore(bulkDS))
	h := buildTestHandler(t, core, map[string]string{}, map[string]string{})

	body, err := json.Marshal(BatchIDsRequest{IDs: []string{"1", "2", "3"}})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/batch/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.BatchDeleteDetections(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var result BatchResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Equal(t, 2, result.Processed)
	assert.Equal(t, 1, result.Skipped)

	assert.ElementsMatch(t, []string{"1", "2", "3"}, bulkDS.calledWith)
	// The per-ID fallback path must not run at all when the bulk call succeeds.
	bulkDS.MockInterface.AssertNotCalled(t, "Get", mock.Anything)
	bulkDS.MockInterface.AssertNotCalled(t, "Delete", mock.Anything)
}

// TestDeleteNotesByIDs_FallsBackWhenBulkPathErrors verifies that if the datastore
// implements batchDeleteNotesDatastore but the bulk call itself fails, deletion
// falls back to the per-ID loop rather than surfacing the bulk error.
func TestDeleteNotesByIDs_FallsBackWhenBulkPathErrors(t *testing.T) {
	e := echo.New()
	underlying := mocks.NewMockInterface(t)
	underlying.On("Get", "1").Return(datastore.Note{ID: 1, Locked: false, ClipName: ""}, nil)
	underlying.On("Delete", "1").Return(nil)

	bulkDS := &bulkDeleteMockDatastore{
		MockInterface: underlying,
		err:           errors.New("bulk delete unavailable"),
	}
	core := apitest.NewCore(t, apitest.WithEcho(e), apitest.WithDatastore(bulkDS))
	h := buildTestHandler(t, core, map[string]string{}, map[string]string{})

	body, err := json.Marshal(BatchIDsRequest{IDs: []string{"1"}})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/batch/delete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	require.NoError(t, h.BatchDeleteDetections(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	var result BatchResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 0, result.Skipped)

	underlying.AssertExpectations(t)
}

// TestBatchReviewDetections tests the BatchReviewDetections endpoint.
func TestBatchReviewDetections(t *testing.T) {
	e, mockDS, controller := setupTestEnvironment(t)

	testCases := []struct {
		name           string
		body           any
		mockSetup      func(*mock.Mock)
		expectedStatus int
		checkResult    func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "marks unlocked as false_positive and skips locked",
			body: BatchReviewRequest{IDs: []string{"1", "2"}, Verified: "false_positive"},
			mockSetup: func(m *mock.Mock) {
				m.On("Get", "1").Return(datastore.Note{ID: 1, Locked: false}, nil)
				m.On("SaveNoteReview", mock.AnythingOfType("*datastore.NoteReview")).Return(nil)
				m.On("Get", "2").Return(datastore.Note{ID: 2, Locked: true}, nil)
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 1, result.Processed)
				assert.Equal(t, 1, result.Skipped)
			},
		},
		{
			name: "marks as correct",
			body: BatchReviewRequest{IDs: []string{"1"}, Verified: "correct"},
			mockSetup: func(m *mock.Mock) {
				m.On("Get", "1").Return(datastore.Note{ID: 1, Locked: false}, nil)
				m.On("SaveNoteReview", mock.AnythingOfType("*datastore.NoteReview")).Return(nil)
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 1, result.Processed)
				assert.Equal(t, 0, result.Skipped)
			},
		},
		{
			name:           "invalid verification status returns 400",
			body:           BatchReviewRequest{IDs: []string{"1"}, Verified: "bad_status"},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing verified field returns 400",
			body:           BatchReviewRequest{IDs: []string{"1"}, Verified: ""},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty IDs returns 400",
			body:           BatchReviewRequest{IDs: []string{}, Verified: "correct"},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "exceeding max batch size returns 400",
			body:           BatchReviewRequest{IDs: make([]string, maxBatchSize+1), Verified: "correct"},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockDS.ExpectedCalls = nil
			mockDS.Calls = nil
			tc.mockSetup(&mockDS.Mock)

			bodyBytes, err := json.Marshal(tc.body)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/batch/review", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err = controller.BatchReviewDetections(c)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedStatus, rec.Code)

			if tc.checkResult != nil {
				tc.checkResult(t, rec)
			}

			mockDS.AssertExpectations(t)
		})
	}
}

// TestBatchLockDetections tests the BatchLockDetections endpoint.
func TestBatchLockDetections(t *testing.T) {
	e, mockDS, controller := setupTestEnvironment(t)

	testCases := []struct {
		name           string
		body           any
		mockSetup      func(*mock.Mock)
		expectedStatus int
		checkResult    func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "locks unlocked detection and skips already-locked",
			body: BatchLockRequest{IDs: []string{"1", "2"}, Locked: true},
			mockSetup: func(m *mock.Mock) {
				m.On("Get", "1").Return(datastore.Note{ID: 1, Locked: false}, nil)
				m.On("LockNote", "1").Return(nil)
				m.On("Get", "2").Return(datastore.Note{ID: 2, Locked: true}, nil)
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 1, result.Processed)
				assert.Equal(t, 1, result.Skipped)
			},
		},
		{
			name: "unlocks all including currently locked",
			body: BatchLockRequest{IDs: []string{"1", "2"}, Locked: false},
			mockSetup: func(m *mock.Mock) {
				m.On("Get", "1").Return(datastore.Note{ID: 1, Locked: false}, nil)
				m.On("UnlockNote", "1").Return(nil)
				m.On("Get", "2").Return(datastore.Note{ID: 2, Locked: true}, nil)
				m.On("UnlockNote", "2").Return(nil)
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 2, result.Processed)
				assert.Equal(t, 0, result.Skipped)
			},
		},
		{
			name:           "empty IDs returns 400",
			body:           BatchLockRequest{IDs: []string{}, Locked: true},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "exceeding max batch size returns 400",
			body:           BatchLockRequest{IDs: make([]string, maxBatchSize+1), Locked: true},
			mockSetup:      func(m *mock.Mock) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockDS.ExpectedCalls = nil
			mockDS.Calls = nil
			tc.mockSetup(&mockDS.Mock)

			bodyBytes, err := json.Marshal(tc.body)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/batch/lock", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err = controller.BatchLockDetections(c)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedStatus, rec.Code)

			if tc.checkResult != nil {
				tc.checkResult(t, rec)
			}

			mockDS.AssertExpectations(t)
		})
	}
}

// TestBatchResolveDetections tests the BatchResolveDetections endpoint.
func TestBatchResolveDetections(t *testing.T) {
	e, mockDS, controller := setupTestEnvironment(t)

	mockNotes := []datastore.Note{
		{ID: 1, ScientificName: "Turdus merula"},
		{ID: 2, ScientificName: "Turdus merula"},
	}

	testCases := []struct {
		name           string
		body           any
		mockSetup      func(*mock.Mock)
		expectedStatus int
		checkResult    func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "resolves species query to IDs",
			body: BatchResolveRequest{QueryType: "species", Species: "Turdus merula"},
			mockSetup: func(m *mock.Mock) {
				m.On("SpeciesDetections",
					mock.AnythingOfType("string"),
					mock.AnythingOfType("string"),
					mock.AnythingOfType("string"),
					mock.AnythingOfType("int"),
					mock.AnythingOfType("bool"),
					mock.AnythingOfType("int"),
					mock.AnythingOfType("int"),
				).Return(mockNotes, nil).Maybe()
				m.On("CountSpeciesDetections",
					mock.AnythingOfType("string"),
					mock.AnythingOfType("string"),
					mock.AnythingOfType("string"),
					mock.AnythingOfType("int"),
				).Return(int64(2), nil).Maybe()
				m.On("SearchNotes", mock.AnythingOfType("string"), mock.AnythingOfType("bool"), mock.AnythingOfType("int"), mock.AnythingOfType("int")).
					Return(mockNotes, int64(2), nil).Maybe()
				m.On("SearchNotesAdvanced", mock.AnythingOfType("*datastore.AdvancedSearchFilters")).
					Return(mockNotes, int64(2), nil).Maybe()
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResolveResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 2, result.Count)
				assert.Len(t, result.IDs, 2)
			},
		},
		{
			name: "resolves all query to IDs",
			body: BatchResolveRequest{QueryType: "all"},
			mockSetup: func(m *mock.Mock) {
				m.On("SearchNotes", mock.AnythingOfType("string"), mock.AnythingOfType("bool"), mock.AnythingOfType("int"), mock.AnythingOfType("int")).
					Return(mockNotes, int64(2), nil).Maybe()
				m.On("SearchNotesAdvanced", mock.AnythingOfType("*datastore.AdvancedSearchFilters")).
					Return(mockNotes, int64(2), nil).Maybe()
			},
			expectedStatus: http.StatusOK,
			checkResult: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var result BatchResolveResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				assert.Equal(t, 2, result.Count)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockDS.ExpectedCalls = nil
			mockDS.Calls = nil
			tc.mockSetup(&mockDS.Mock)

			bodyBytes, err := json.Marshal(tc.body)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v2/detections/batch/resolve", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err = controller.BatchResolveDetections(c)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedStatus, rec.Code)

			if tc.checkResult != nil {
				tc.checkResult(t, rec)
			}

			mockDS.AssertExpectations(t)
		})
	}
}

// TestBatchRoutes verifies that all batch endpoints are correctly registered.
func TestBatchRoutes(t *testing.T) {
	e, _, controller := setupTestEnvironment(t)
	controller.RegisterDetectionRoutes(controller.Group)

	apitest.AssertRoutesRegistered(t, e, []string{
		"POST /api/v2/detections/batch/delete",
		"POST /api/v2/detections/batch/review",
		"POST /api/v2/detections/batch/lock",
		"POST /api/v2/detections/batch/resolve",
	})
}
