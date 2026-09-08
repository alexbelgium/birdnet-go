package reanalyze

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository"
	bnerrors "github.com/tphakala/birdnet-go/internal/errors"
)

// spec3s is a BirdNET-v2.4-shaped model spec: 48 kHz, 3-second windows.
var spec3s = classifier.ModelSpec{SampleRate: 48000, ClipLength: 3 * time.Second}

// stubPredict returns a predict function that answers every window with the
// supplied per-window result sets, in order, and records how many windows it saw.
func stubPredict(perWindow [][]datastore.Results, calls *int) predictModelFn {
	return func(_ context.Context, _ string, _ [][]float32) ([]datastore.Results, error) {
		i := *calls
		*calls++
		if i < len(perWindow) {
			return perWindow[i], nil
		}
		return nil, nil
	}
}

func TestReanalyzeSamples_KeepsMaxConfidenceAcrossWindows(t *testing.T) {
	t.Parallel()

	// 3 s windows at 50% overlap over 6 s of audio => offsets 0, 1.5, 3 s.
	samples := make([]float32, 48000*6)
	calls := 0
	predict := stubPredict([][]datastore.Results{
		{{Species: "Ficedula hypoleuca_Pied Flycatcher", Confidence: 0.40}},
		{{Species: "Ficedula hypoleuca_Pied Flycatcher", Confidence: 0.91}},
		{{Species: "Ficedula hypoleuca_Pied Flycatcher", Confidence: 0.55}},
	}, &calls)

	scores, windows, err := reanalyzeSamples(t.Context(), predict, "BirdNET_V2.4", spec3s, samples)
	require.NoError(t, err)
	assert.Equal(t, 3, windows, "6s of audio at 3s/50%% overlap yields three whole windows")
	assert.Equal(t, 3, calls)
	// The peak, not the first or the last, survives aggregation.
	assert.InDelta(t, 0.91, float64(scores["Ficedula hypoleuca_Pied Flycatcher"]), 1e-6)
}

func TestReanalyzeSamples_PadsShortClipToOneWindow(t *testing.T) {
	t.Parallel()

	// One second of audio is shorter than a 3 s window: without padding the loop
	// body never runs and the endpoint silently reports zero predictions.
	samples := make([]float32, 48000)
	calls := 0
	predict := stubPredict([][]datastore.Results{
		{{Species: "Parus major_Great Tit", Confidence: 0.7}},
	}, &calls)

	scores, windows, err := reanalyzeSamples(t.Context(), predict, "BirdNET_V2.4", spec3s, samples)
	require.NoError(t, err)
	assert.Equal(t, 1, windows)
	assert.InDelta(t, 0.7, float64(scores["Parus major_Great Tit"]), 1e-6)
}

func TestReanalyzeSamples_PropagatesPredictError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("model closed")
	predict := func(_ context.Context, _ string, _ [][]float32) ([]datastore.Results, error) {
		return nil, wantErr
	}

	_, windows, err := reanalyzeSamples(t.Context(), predict, "BirdNET_V2.4", spec3s, make([]float32, 48000*6))
	require.ErrorIs(t, err, wantErr, "an inference failure must fail the request, not yield partial results")
	assert.Equal(t, 0, windows)
}

func TestReanalyzeSamples_RejectsEmptySamples(t *testing.T) {
	t.Parallel()

	predict := func(_ context.Context, _ string, _ [][]float32) ([]datastore.Results, error) {
		t.Fatal("predict must not be called for an empty sample stream")
		return nil, nil
	}
	_, _, err := reanalyzeSamples(t.Context(), predict, "BirdNET_V2.4", spec3s, nil)
	require.Error(t, err)
}

func TestReanalyzeSamples_RejectsZeroClipLength(t *testing.T) {
	t.Parallel()

	predict := func(_ context.Context, _ string, _ [][]float32) ([]datastore.Results, error) {
		t.Fatal("predict must not be called for a degenerate model spec")
		return nil, nil
	}
	_, _, err := reanalyzeSamples(t.Context(), predict, "Broken",
		classifier.ModelSpec{SampleRate: 48000, ClipLength: 0}, make([]float32, 48000))
	require.Error(t, err)
}

func TestReanalyzeSamples_StopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	// Cancel after the first window; the walk must stop rather than burn
	// inference on the remaining windows of an abandoned request.
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	predict := func(_ context.Context, _ string, _ [][]float32) ([]datastore.Results, error) {
		calls++
		cancel()
		return []datastore.Results{{Species: "Parus major", Confidence: 0.5}}, nil
	}

	_, windows, err := reanalyzeSamples(ctx, predict, "BirdNET_V2.4", spec3s, make([]float32, 48000*30))
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls, "only the in-flight window should run after cancellation")
	assert.Equal(t, 1, windows)
}

func TestReanalyzeSamples_DoesNotTruncatePerModel(t *testing.T) {
	t.Parallel()

	// Top-N truncation is the multi-model aggregator's job. A single model's
	// result set must come back whole, or a species that only one model sees
	// would be dropped before the grid is assembled.
	many := make([]datastore.Results, 0, 25)
	for i := range 25 {
		many = append(many, datastore.Results{
			Species:    string(rune('A'+i)) + "_species",
			Confidence: float32(i) / 100,
		})
	}
	calls := 0
	predict := stubPredict([][]datastore.Results{many}, &calls)

	scores, _, err := reanalyzeSamples(t.Context(), predict, "BirdNET_V2.4", spec3s, make([]float32, 48000*3))
	require.NoError(t, err)
	assert.Len(t, scores, 25)
}

func TestSelectModelsForReanalysis_DefaultSkipsUltrasonicModels(t *testing.T) {
	t.Parallel()

	infos := []classifier.ModelInfo{
		{ID: "BirdNET_V2.4", Name: "BirdNET v2.4", Spec: spec3s},
		{ID: "Perch_V2", Name: "Perch v2", Spec: classifier.ModelSpec{SampleRate: 32000, ClipLength: 5 * time.Second}},
		// Bat: expects raw 256 kHz audio that saved detection clips never carry.
		{ID: "Bat_V1", Name: "Bat", Spec: classifier.ModelSpec{SampleRate: 256000, ClipLength: time.Second, RawSampleRate: 256000}},
		// Degenerate spec: no usable sample rate to decode at.
		{ID: "Broken", Name: "Broken", Spec: classifier.ModelSpec{SampleRate: 0, ClipLength: time.Second}},
	}

	got, err := selectModelsForReanalysis(infos, nil)
	require.NoError(t, err)

	ids := make([]string, 0, len(got))
	for _, m := range got {
		ids = append(ids, m.id)
	}
	assert.ElementsMatch(t, []string{"BirdNET_V2.4", "Perch_V2"}, ids)
}

func TestSelectModelsForReanalysis_ExplicitListDedupesAndErrorsOnUnloaded(t *testing.T) {
	t.Parallel()

	infos := []classifier.ModelInfo{{ID: "BirdNET_V2.4", Name: "BirdNET v2.4", Spec: spec3s}}

	// "birdnet" is the config alias for "BirdNET_V2.4"; asking for both forms must
	// schedule the model once, not twice.
	got, err := selectModelsForReanalysis(infos, []string{"birdnet", "BirdNET_V2.4"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "BirdNET_V2.4", got[0].id)

	// An unloaded model is an error, not a silent drop: the caller asked for
	// something specific and must learn it was not honoured.
	_, err = selectModelsForReanalysis(infos, []string{"Perch_V2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Perch_V2")
}

func TestSelectModelsForReanalysis_ExplicitListAcceptsUltrasonicWhenAsked(t *testing.T) {
	t.Parallel()

	// The RawSampleRate filter is a DEFAULT, not a hard ban: an explicit request
	// for a loaded model is honoured. (The correction endpoint refuses to
	// attribute a correction to one; reanalysis merely shows its scores.)
	infos := []classifier.ModelInfo{
		{ID: "Bat_V1", Name: "Bat", Spec: classifier.ModelSpec{SampleRate: 256000, ClipLength: time.Second, RawSampleRate: 256000}},
	}
	got, err := selectModelsForReanalysis(infos, []string{"Bat_V1"})
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestReanalyzePrediction_MaxConfidence(t *testing.T) {
	t.Parallel()

	p := ReanalyzePrediction{ByModel: map[string]float32{"BirdNET_V2.4": 0.30, "Perch_V2": 0.86}}
	assert.InDelta(t, 0.86, float64(p.MaxConfidence()), 1e-6)

	empty := ReanalyzePrediction{ByModel: map[string]float32{}}
	assert.Zero(t, empty.MaxConfidence(), "a prediction no model scored ranks last, not panics")
}

func TestIsDetectionNotFoundErr_CoversAllThreeDatastoreShapes(t *testing.T) {
	t.Parallel()

	// The legacy DataStore.Get path returns a CategoryNotFound enhanced error
	// that does NOT wrap gorm.ErrRecordNotFound. Matching only the sentinels
	// turns a legacy 404 into a 500, which tells the operator their detection is
	// gone when the real answer is "that id does not exist".
	legacyNotFound := bnerrors.Newf("note not found: 42").
		Component("datastore").
		Category(bnerrors.CategoryNotFound).
		Build()

	assert.True(t, isDetectionNotFoundErr(legacyNotFound), "legacy enhanced not-found")
	assert.True(t, isDetectionNotFoundErr(repository.ErrDetectionNotFound), "v2 repository sentinel")
	assert.True(t, isDetectionNotFoundErr(gorm.ErrRecordNotFound), "bare GORM miss")
	assert.True(t, isDetectionNotFoundErr(fmt.Errorf("wrapped: %w", gorm.ErrRecordNotFound)), "wrapped")

	// A transient database failure is emphatically NOT a 404.
	assert.False(t, isDetectionNotFoundErr(errors.New("database is locked")))
	assert.False(t, isDetectionNotFoundErr(nil))
}

func TestIsClipNotFoundErr_IncludesDetectionAndClipCases(t *testing.T) {
	t.Parallel()

	// A missing clip and a missing parent detection both mean "there is nothing
	// to reanalyze", so both are 404 — but only the second is "Detection not found".
	assert.True(t, isClipNotFoundErr(os.ErrNotExist))
	assert.True(t, isClipNotFoundErr(repository.ErrNoClipPath))
	assert.True(t, isClipNotFoundErr(repository.ErrDetectionNotFound))
	assert.False(t, isClipNotFoundErr(errors.New("permission denied")))
}

func TestAddSpeciesScore_MergesDifferentLabelSpellingsForOneSpecies(t *testing.T) {
	t.Parallel()

	// This is the whole point of the grid. BirdNET emits
	// "Ficedula hypoleuca_Pied Flycatcher"; Perch emits bare "Ficedula hypoleuca".
	// Keyed on the raw label they become two rows and never line up, so the user
	// can never see that both classifiers agree.
	agg := make(map[string]*speciesAggregate)
	addSpeciesScore(agg, "Ficedula hypoleuca_Pied Flycatcher", "BirdNET_V2.4", 0.998, 2)
	addSpeciesScore(agg, "Ficedula hypoleuca", "Perch_V2", 0.869, 2)

	require.Len(t, agg, 1, "both spellings must collapse into one species row")
	a := agg["Ficedula hypoleuca"]
	require.NotNil(t, a)
	assert.Equal(t, "Ficedula hypoleuca", a.scientific)
	// The model that supplied a common name wins; the bare-scientific model must
	// not blank it out.
	assert.Equal(t, "Pied Flycatcher", a.common)
	assert.InDelta(t, 0.998, float64(a.byModel["BirdNET_V2.4"]), 1e-6)
	assert.InDelta(t, 0.869, float64(a.byModel["Perch_V2"]), 1e-6)
}

func TestAddSpeciesScore_KeepsBestPerModelAndUnparseableLabels(t *testing.T) {
	t.Parallel()

	agg := make(map[string]*speciesAggregate)
	// Same model, two windows: the peak survives.
	addSpeciesScore(agg, "Parus major_Great Tit", "BirdNET_V2.4", 0.40, 1)
	addSpeciesScore(agg, "Parus major_Great Tit", "BirdNET_V2.4", 0.91, 1)
	assert.InDelta(t, 0.91, float64(agg["Parus major"].byModel["BirdNET_V2.4"]), 1e-6)

	// An underscore-separated Perch sound class splits the same way it does
	// everywhere else in the UI (SplitSpeciesName is the shared splitter), so the
	// grid stays consistent with the detection views rather than inventing its own
	// rendering for these labels.
	addSpeciesScore(agg, "power_tool", "Perch_V2", 0.5, 1)
	addSpeciesScore(agg, "power_tool", "Perch_V2", 0.7, 1)
	require.Contains(t, agg, "power")
	assert.InDelta(t, 0.7, float64(agg["power"].byModel["Perch_V2"]), 1e-6)

	// A multi-word non-binomial label has no scientific half at all. It must key
	// on its common name, not on "" — otherwise every such label in the clip
	// collapses onto one row and their scores overwrite each other.
	addSpeciesScore(agg, "engine idling nearby", "Perch_V2", 0.3, 1)
	addSpeciesScore(agg, "distant human speech", "Perch_V2", 0.4, 1)
	require.Contains(t, agg, "engine idling nearby")
	require.Contains(t, agg, "distant human speech")
	assert.NotContains(t, agg, "")
	assert.Empty(t, agg["engine idling nearby"].scientific)
	assert.Equal(t, "engine idling nearby", agg["engine idling nearby"].common)
}

func TestReanalyzeSamples_AnalyzesTheTrailingPartialWindow(t *testing.T) {
	t.Parallel()

	// 4 s of audio through a 3 s model at 1.5 s stride: the strided walk runs only
	// offset 0 (offset 1.5 would need 4.5 s), so seconds 3-4 go unanalyzed. A bird
	// calling only in that tail is exactly the case a second opinion is for.
	samples := make([]float32, 48000*4)
	var offsetsSeen int
	predict := func(_ context.Context, _ string, window [][]float32) ([]datastore.Results, error) {
		offsetsSeen++
		if offsetsSeen == 1 {
			return []datastore.Results{{Species: "Parus major_Great Tit", Confidence: 0.2}}, nil
		}
		// The end-anchored window is where the interesting call lives.
		return []datastore.Results{{Species: "Ficedula hypoleuca_Pied Flycatcher", Confidence: 0.95}}, nil
	}

	scores, windows, err := reanalyzeSamples(t.Context(), predict, "BirdNET_V2.4", spec3s, samples)
	require.NoError(t, err)
	assert.Equal(t, 2, windows, "one strided window plus one anchored at the clip's end")
	assert.Contains(t, scores, "Ficedula hypoleuca_Pied Flycatcher")
	assert.InDelta(t, 0.95, float64(scores["Ficedula hypoleuca_Pied Flycatcher"]), 1e-6)
}

func TestReanalyzeSamples_NoExtraWindowWhenTheClipAlignsToTheStride(t *testing.T) {
	t.Parallel()

	// 6 s at 3 s/1.5 s stride ends exactly on a window boundary, so the tail fix
	// must not add a redundant fourth pass — that would be a third of the
	// inference cost for nothing.
	calls := 0
	predict := stubPredict(nil, &calls)
	_, windows, err := reanalyzeSamples(t.Context(), predict, "BirdNET_V2.4", spec3s, make([]float32, 48000*6))
	require.NoError(t, err)
	assert.Equal(t, 3, windows)
	assert.Equal(t, 3, calls)
}

func TestReanalyzeDetection_RefusesWhenAnotherReanalysisHoldsTheSlot(t *testing.T) {
	// Not parallel: it takes the process-wide admission slot.
	core := apitest.NewCore(t)
	h := New(core)

	// Simulate a reanalysis already in progress.
	reanalysisSlot <- struct{}{}
	defer func() { <-reanalysisSlot }()

	rec := httptest.NewRecorder()
	ctx := core.Echo.NewContext(httptest.NewRequest(http.MethodPost, "/", http.NoBody), rec)
	ctx.SetParamNames("id")
	ctx.SetParamValues("1")

	require.NoError(t, h.ReanalyzeDetection(ctx))
	// 429, not a queued request: inference is globally serialised anyway, so a
	// second concurrent run would only sit on a multi-MiB PCM buffer while it
	// waits. The strict datastore mock also asserts no DB call happened.
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestReanalyzeDetection_ReleasesTheSlotOnAnErrorPath(t *testing.T) {
	// Not parallel: it takes the process-wide admission slot.
	core := apitest.NewCore(t)
	h := New(core)

	rec := httptest.NewRecorder()
	ctx := core.Echo.NewContext(httptest.NewRequest(http.MethodPost, "/", http.NoBody), rec)
	ctx.SetParamNames("id")
	ctx.SetParamValues("not-a-number")
	require.NoError(t, h.ReanalyzeDetection(ctx))

	// The slot must be free again: a request that failed validation must not
	// wedge the endpoint shut for the lifetime of the process.
	select {
	case reanalysisSlot <- struct{}{}:
		<-reanalysisSlot
	default:
		t.Fatal("reanalysis slot was not released after an early-return error path")
	}
}
