package detections

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tphakala/birdnet-go/internal/analysis/processor"
	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/audiocore/ultrasonic"
	"github.com/tphakala/birdnet-go/internal/audiocore/wav"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
	datastoreV2 "github.com/tphakala/birdnet-go/internal/datastore/v2"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository"
	"github.com/tphakala/birdnet-go/internal/datastore/v2only"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/logger"
)

const (
	rerunTestClip      = "saved.wav"
	rerunTestRate      = 192000
	rerunTestFrames    = 8192
	rerunTestFFTSize   = 512
	rerunTestThreshold = 0.15
	rerunTestLocale    = "fr"
	rerunTestUserEntry = "A reviewer comment"

	// rerunTestBSGBatStepFrames is a valid non-default bsgbat.stepframes.
	rerunTestBSGBatStepFrames = 150
)

func rerunTestSettings(s *conf.Settings) {
	s.Bat.UltrasonicFilter = conf.UltrasonicFilterConfig{
		Enabled: true, CVThreshold: rerunTestThreshold, FFTSize: rerunTestFFTSize,
		HopSize: rerunTestFFTSize / 2, FrequencySplitHz: 20000,
	}
	s.Realtime.Dashboard.Locale = rerunTestLocale
}

func writeRerunWAV(t *testing.T, h *Handler, rate, frames int, burst bool) {
	t.Helper()
	const bytesPerSample = 2
	pcm := make([]byte, frames*bytesPerSample)
	if burst {
		const amplitude, frequency = 16000, 40000
		for i := frames / 2; i < frames*3/4; i++ {
			sample := int16(amplitude * math.Sin(2*math.Pi*frequency*float64(i)/float64(rate)))
			binary.LittleEndian.PutUint16(pcm[i*bytesPerSample:], uint16(sample))
		}
	}
	require.NoError(t, wav.EncodePCM(t.Context(), &wav.Options{
		PCMData: pcm, SampleRate: rate, BitDepth: 16,
		OutputPath: filepath.Join(h.CurrentSettings().Realtime.Audio.Export.Path, rerunTestClip),
	}))
}

type rerunFixture struct {
	echo *echo.Echo
	h    *Handler
	db   *gorm.DB
	path string
	id   uint
}

func newRerunFixture(t *testing.T, model detection.ModelInfo, clip string, change func(*conf.Settings)) rerunFixture {
	t.Helper()
	f := newContribFixture(t)
	db := f.mgr.DB()
	repo := repository.NewDetectionRepository(db, nil, false, false)
	store, err := v2only.New(&v2only.Config{
		Manager: f.mgr, Detection: repo,
		Label:  repository.NewLabelRepository(db, nil, false, false),
		Model:  repository.NewModelRepository(db, nil, false, false),
		Logger: logger.Global().Module("rerun_test"),
	})
	require.NoError(t, err)

	row := entities.AIModel{Name: model.Name, Version: model.Version, Variant: model.Variant, ModelType: entities.ModelType(model.ModelType)}
	require.NoError(t, db.Where("name = ? AND version = ? AND variant = ?", row.Name, row.Version, row.Variant).FirstOrCreate(&row).Error)
	require.NoError(t, db.Model(&entities.Detection{}).Where("id = ?", f.detectionID).
		Updates(map[string]any{"model_id": row.ID, "clip_name": clip}).Error)

	e, _, h := setupTestEnvironment(t, apitest.WithSettingsFunc(func(s *conf.Settings) {
		rerunTestSettings(s)
		if change != nil {
			change(s)
		}
	}))
	h.Repo = v2only.NewDetectionRepository(store)
	h.V2Manager = f.mgr
	h.AuthMiddleware = func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	datastoreV2.SetEnhancedDatabaseMode()
	t.Cleanup(datastoreV2.ResetDatabaseMode)
	h.RegisterDetectionRoutes(h.Group)
	path := "/api/v2/detections/" + strconv.FormatUint(uint64(f.detectionID), 10) + "/rerun-unlikely"
	return rerunFixture{echo: e, h: h, db: db, path: path, id: f.detectionID}
}

func (f rerunFixture) post(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, f.path, nil)
	rec := httptest.NewRecorder()
	f.echo.ServeHTTP(rec, req)
	return rec
}

func (f rerunFixture) outcome(t *testing.T) rerunUnlikelyResponse {
	t.Helper()
	rec := f.post(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp rerunUnlikelyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// rerunRow is the stored state the endpoint may change: the flag and the comments.
type rerunRow struct {
	Unlikely bool
	Comments []entities.DetectionComment
}

func (f rerunFixture) read(t *testing.T) rerunRow {
	t.Helper()
	var det entities.Detection
	require.NoError(t, f.db.First(&det, f.id).Error)
	row := rerunRow{Unlikely: det.Unlikely}
	require.NoError(t, f.db.Where("detection_id = ?", f.id).Order("id").Find(&row.Comments).Error)
	return row
}

func TestRerunUnlikely_SavedClipVerdicts(t *testing.T) {
	model := classifier.DetectionModelInfoForID(classifier.RegistryIDBat)
	f := newRerunFixture(t, model, rerunTestClip, nil)
	userComment := entities.DetectionComment{DetectionID: f.id, Entry: rerunTestUserEntry}
	require.NoError(t, f.db.Create(&userComment).Error)
	writeRerunWAV(t, f.h, rerunTestRate, rerunTestFrames, false)

	resp := f.outcome(t)
	assert.Equal(t, rerunUnlikelyOutcome, resp.Outcome)
	assert.True(t, resp.Unlikely)
	assert.Zero(t, resp.UltrasonicCV)
	assert.InDelta(t, rerunTestThreshold, resp.UltrasonicCVThreshold, 1e-12)
	row := f.read(t)
	assert.True(t, row.Unlikely)
	require.Len(t, row.Comments, 2)
	auto := row.Comments[1]
	assert.Equal(t, processor.FormatUltrasonicReviewComment(rerunTestLocale, resp.UltrasonicCV, rerunTestThreshold), auto.Entry)
	assert.Equal(t, rerunUnchangedOutcome, f.outcome(t).Outcome)
	assert.Equal(t, row.Comments, f.read(t).Comments)

	// A new settings snapshot takes effect without re-registering the handler.
	settings := conf.CloneSettings(f.h.CurrentSettings())
	const newThreshold = 0.25
	settings.Bat.UltrasonicFilter.CVThreshold = newThreshold
	apitest.PublishTestSettings(t, settings)
	assert.InDelta(t, newThreshold, f.outcome(t).UltrasonicCVThreshold, 1e-12)
	row = f.read(t)
	require.Len(t, row.Comments, 2)
	assert.Equal(t, auto.ID, row.Comments[1].ID)
	assert.NotEqual(t, auto.Entry, row.Comments[1].Entry)

	writeRerunWAV(t, f.h, rerunTestRate, rerunTestFrames, true)
	resp = f.outcome(t)
	assert.Equal(t, rerunProbableOutcome, resp.Outcome)
	assert.False(t, resp.Unlikely)
	assert.Greater(t, resp.UltrasonicCV, resp.UltrasonicCVThreshold)
	row = f.read(t)
	assert.False(t, row.Unlikely)
	require.Len(t, row.Comments, 1)
	assert.Equal(t, userComment.ID, row.Comments[0].ID)
}

func TestRerunUnlikely_ReusesLiveCommentAndRemovesDuplicates(t *testing.T) {
	f := newRerunFixture(t, classifier.DetectionModelInfoForID(classifier.RegistryIDBat), rerunTestClip, nil)
	require.NoError(t, f.db.Model(&entities.Detection{}).Where("id = ?", f.id).Update("unlikely", true).Error)
	const liveCV = 0.025
	// The comment the live pipeline added (another locale), a duplicate, and a user comment.
	liveComment := entities.DetectionComment{DetectionID: f.id,
		Entry: processor.FormatUltrasonicReviewComment("de", liveCV, rerunTestThreshold)}
	require.NoError(t, f.db.Create(&liveComment).Error)
	require.NoError(t, f.db.Create(&entities.DetectionComment{DetectionID: f.id,
		Entry: processor.FormatUltrasonicReviewComment("en", liveCV, rerunTestThreshold)}).Error)
	userComment := entities.DetectionComment{DetectionID: f.id, Entry: rerunTestUserEntry}
	require.NoError(t, f.db.Create(&userComment).Error)

	writeRerunWAV(t, f.h, rerunTestRate, rerunTestFrames, false)
	resp := f.outcome(t)
	assert.Equal(t, rerunUnlikelyOutcome, resp.Outcome, "rewriting the comment is a change")
	row := f.read(t)
	assert.True(t, row.Unlikely)
	require.Len(t, row.Comments, 2)
	assert.Equal(t, liveComment.ID, row.Comments[0].ID, "the live comment is kept and rewritten")
	assert.Equal(t, processor.FormatUltrasonicReviewComment(rerunTestLocale, resp.UltrasonicCV, rerunTestThreshold), row.Comments[0].Entry)
	assert.Equal(t, userComment.ID, row.Comments[1].ID)

	writeRerunWAV(t, f.h, rerunTestRate, rerunTestFrames, true)
	assert.Equal(t, rerunProbableOutcome, f.outcome(t).Outcome)
	row = f.read(t)
	assert.False(t, row.Unlikely)
	require.Len(t, row.Comments, 1)
	assert.Equal(t, userComment.ID, row.Comments[0].ID)
}

func TestRerunUnlikely_LockedDetectionRefused(t *testing.T) {
	f := newRerunFixture(t, classifier.DetectionModelInfoForID(classifier.RegistryIDBat), rerunTestClip, nil)
	require.NoError(t, f.db.Create(&entities.DetectionLock{DetectionID: f.id}).Error)
	writeRerunWAV(t, f.h, rerunTestRate, rerunTestFrames, false)
	before := f.read(t)
	rec := f.post(t)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Equal(t, before, f.read(t))
}

func TestRerunUnlikely_RefusedWithoutChanges(t *testing.T) {
	bat := classifier.DetectionModelInfoForID(classifier.RegistryIDBSGBat)
	for _, tc := range []struct {
		name, reason, clip string
		model              detection.ModelInfo
		rate, frames       int
		settings           func(*conf.Settings)
	}{
		{name: "disabled", reason: rerunDisabledMessage, model: bat, settings: func(s *conf.Settings) { s.Bat.UltrasonicFilter.Enabled = false }},
		{name: "invalid STFT", reason: rerunSTFTMessage, model: bat, settings: func(s *conf.Settings) { s.Bat.UltrasonicFilter.HopSize = 0 }},
		{name: "missing clip", reason: rerunNoClipMessage, model: bat},
		{name: "missing clip file", reason: rerunNoClipMessage, model: bat, clip: rerunTestClip},
		{name: "low clip rate", reason: rerunClipBandMessage, model: bat, clip: rerunTestClip, rate: 40000, frames: rerunTestFrames},
		{name: "too short", reason: rerunTooShortMessage, model: bat, clip: rerunTestClip, rate: rerunTestRate, frames: 32},
		{name: "bird model", reason: rerunNonBatMessage, model: detection.DefaultModelInfo()},
		{name: "unknown model", reason: rerunUnknownModelMessage, model: detection.ModelInfo{Name: "Unknown bat model", Version: "1", ModelType: string(entities.ModelTypeBat)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRerunFixture(t, tc.model, tc.clip, tc.settings)
			require.NoError(t, f.db.Model(&entities.Detection{}).Where("id = ?", f.id).Update("unlikely", true).Error)
			require.NoError(t, f.db.Create(&entities.DetectionComment{DetectionID: f.id, Entry: rerunTestUserEntry}).Error)
			if tc.frames > 0 {
				writeRerunWAV(t, f.h, tc.rate, tc.frames, false)
			}
			before := f.read(t)
			resp := f.post(t)
			assert.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
			assert.Contains(t, resp.Body.String(), tc.reason)
			assert.Equal(t, before, f.read(t))
		})
	}
}

func TestBatFilterRate(t *testing.T) {
	for _, id := range []string{classifier.RegistryIDBat, classifier.RegistryIDBSGBat} {
		spec, found := classifier.GetModelSpec(id)
		require.True(t, found)
		model := classifier.DetectionModelInfoForID(id)
		rate, reason := batFilterRate(model, 20000)
		assert.Empty(t, reason)
		assert.Equal(t, spec.EffectiveSampleRate(), rate)
		_, reason = batFilterRate(model, rate/2)
		assert.Equal(t, rerunModelBandMessage, reason)
	}
	_, reason := batFilterRate(detection.DefaultModelInfo(), 20000)
	assert.Equal(t, rerunNonBatMessage, reason)
}

func TestHighestWindowCV_JudgesEachWindowLikeTheLivePipeline(t *testing.T) {
	cfg := conf.UltrasonicFilterConfig{Enabled: true, CVThreshold: rerunTestThreshold,
		FFTSize: rerunTestFFTSize, HopSize: rerunTestFFTSize / 2, FrequencySplitHz: 20000}
	const window = rerunTestFrames
	flat := make([]float32, window)
	burst := make([]float32, window)
	for i := window / 2; i < window*3/4; i++ {
		burst[i] = float32(0.5 * math.Sin(2*math.Pi*40000*float64(i)/float64(rerunTestRate)))
	}
	judge := func(w []float32) float64 {
		frameCV, ok := ultrasonic.ComputeUSFrameCV(w, rerunTestRate, cfg)
		require.True(t, ok)
		return ultrasonic.BroadbandAdjustedCV(w, rerunTestRate, cfg, frameCV)
	}
	join := func(parts ...[]float32) []float32 {
		var out []float32
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	flatCV, burstCV := judge(flat), judge(burst)
	require.Less(t, flatCV, rerunTestThreshold)
	require.Greater(t, burstCV, rerunTestThreshold)

	run := func(samples []float32, window, step int) float64 {
		t.Helper()
		cv, ok, err := highestWindowCV(t.Context(), samples, rerunTestRate, window, step, cfg)
		require.NoError(t, err)
		require.True(t, ok)
		return cv
	}

	// Every window flat: unlikely, as live when every hit window is flat.
	assert.InDelta(t, flatCV, run(join(flat, flat, flat), window, window/2), 1e-9)

	// One window with a call, 50% overlap as live: probable, and the CV reported
	// is the highest window CV.
	samples := join(flat, burst, flat)
	want := 0.0
	for start := 0; start+window <= len(samples); start += window / 2 {
		want = max(want, judge(samples[start:start+window]))
	}
	got := run(samples, window, window/2)
	assert.InDelta(t, want, got, 1e-9)
	assert.False(t, ultrasonic.IsUnlikely(got, cfg))

	// The tail past the last full step is judged too (overlapping step that
	// does not divide the clip).
	tailed := join(flat, flat[:window/3], burst)
	assert.InDelta(t, burstCV, run(tailed, window, window*2/3), 1e-9)

	// A clip shorter than a window is judged whole.
	assert.InDelta(t, burstCV, run(burst, 4*window, 2*window), 1e-9)

	// A request that ends stops the analysis.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, ok, err := highestWindowCV(ctx, join(flat, burst), rerunTestRate, window, window/2, cfg)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, ok)
}

func TestBatWindow_MatchesLiveAnalysisWindow(t *testing.T) {
	settings := &conf.Settings{} // bsgbat.stepframes unset: the default step

	// Bat: the 48 kHz x 3 s window (144,000 samples) of the 256 kHz PCM, 50% overlap.
	bat, found := classifier.GetModelSpec(classifier.RegistryIDBat)
	require.True(t, found)
	window, step := batWindow(classifier.DetectionModelInfoForID(classifier.RegistryIDBat), settings)
	assert.Equal(t, 144000, window)
	assert.Equal(t, bat.ClipSampleCount(), window)
	assert.Equal(t, window/2, step)

	// BSG-BAT: its own window and the configured step.
	bsg, found := classifier.GetModelSpec(classifier.RegistryIDBSGBat)
	require.True(t, found)
	window, step = batWindow(classifier.DetectionModelInfoForID(classifier.RegistryIDBSGBat), settings)
	assert.Equal(t, bsg.ClipSampleCount(), window)
	assert.Equal(t, classifier.BSGBatStepSamples(settings), step)

	custom := conf.CloneSettings(settings)
	custom.BSGBat.StepFrames = rerunTestBSGBatStepFrames
	require.True(t, conf.ValidBSGBatStepFrames(custom.BSGBat.StepFrames))
	_, step = batWindow(classifier.DetectionModelInfoForID(classifier.RegistryIDBSGBat), custom)
	assert.Equal(t, classifier.BSGBatStepSamples(custom), step)
	assert.NotEqual(t, classifier.BSGBatStepSamples(settings), step)

	window, step = batWindow(detection.ModelInfo{Name: "unknown"}, settings)
	assert.Zero(t, window)
	assert.Zero(t, step)
}
