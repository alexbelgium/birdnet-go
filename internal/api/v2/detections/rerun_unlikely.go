package detections

import (
	"context"
	"io"
	"math"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/tphakala/birdnet-go/internal/analysis/processor"
	"github.com/tphakala/birdnet-go/internal/audiocore/transcode"
	"github.com/tphakala/birdnet-go/internal/audiocore/ultrasonic"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
	datastoreV2 "github.com/tphakala/birdnet-go/internal/datastore/v2"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/errors"
)

const (
	rerunDisabledMessage     = "The ultrasonic validation filter is disabled"
	rerunNonBatMessage       = "The detection was not made by a bat model"
	rerunUnknownModelMessage = "The detection model is unknown"
	rerunModelBandMessage    = "The bat model sample rate has no ultrasonic band above the configured frequency split"
	rerunClipBandMessage     = "The saved clip sample rate has no ultrasonic band above the configured frequency split"
	rerunNoClipMessage       = "No audio clip is available for this detection"
	rerunDecodeMessage       = "The saved audio clip could not be decoded for ultrasonic validation"
	rerunTooLongMessage      = "The saved clip is too long for ultrasonic validation"
	rerunSTFTMessage         = "The ultrasonic filter has an invalid STFT configuration"
	rerunThresholdMessage    = "The ultrasonic filter has an invalid CV threshold"
	rerunTooShortMessage     = "The saved clip has too little audio for the configured STFT"
	rerunLockedMessage       = "Detection is locked and cannot be changed"
	rerunCanceledMessage     = "The unlikely filter re-run was canceled"
	rerunBusyMessage         = "Another unlikely filter re-run is in progress; try again when it finishes"
	// rerunReasonKey is the i18n key the frontend shows a refusal with ({reason}).
	rerunReasonKey        = "detections.detail.review.rerunUnlikelyRefused"
	rerunRequestEndedLog  = "Ultrasonic validation ended before completion"
	rerunUnchangedOutcome = "unchanged"
	rerunUnlikelyOutcome  = "unlikely"
	rerunProbableOutcome  = "probable"

	// Apply the existing 16-bit PCM sample limit to the resampled mono clip too.
	maxRerunSamples = transcode.MaxDecodedPCMBytes / 2
)

// ultrasonicVerdictWriter is implemented by the v2 detection repository
// (repository.detectionRepository.SetUltrasonicVerdict).
type ultrasonicVerdictWriter interface {
	SetUltrasonicVerdict(ctx context.Context, id uint, unlikely bool, comment string, isAuto func(string) bool) (changed bool, err error)
}

// rerunSlot allows one re-run at a time (see RerunUnlikely).
var rerunSlot = make(chan struct{}, 1)

// rerunRefused answers a re-run that did not run, with the public reason both as
// the message and as the {reason} parameter of rerunReasonKey for the frontend.
func (c *Handler) rerunRefused(ctx echo.Context, err error, reason string, code int) error {
	return c.HandleErrorWithKey(ctx, err, reason, code, rerunReasonKey, map[string]any{"reason": reason})
}

// ultrasonicVerdict is the result of the ultrasonic filter on a saved clip.
type ultrasonicVerdict struct {
	Unlikely      bool
	CV, Threshold float64
	Comment       string
}

// verdictWriter returns the v2 repository as an ultrasonicVerdictWriter, or nil
// when the enhanced database is not active.
func (c *Handler) verdictWriter() ultrasonicVerdictWriter {
	if c.V2Manager == nil || !datastoreV2.IsEnhancedDatabase() {
		return nil
	}
	w, _ := repository.NewDetectionRepository(c.V2Manager.DB(), nil, c.V2Manager.TablePrefix() != "", c.V2Manager.IsMySQL()).(ultrasonicVerdictWriter)
	return w
}

type rerunUnlikelyResponse struct {
	Outcome               string  `json:"outcome"`
	Unlikely              bool    `json:"unlikely"`
	UltrasonicCV          float64 `json:"ultrasonicCV"`
	UltrasonicCVThreshold float64 `json:"ultrasonicCVThreshold"`
}

// RerunUnlikely evaluates a saved bat clip with the current ultrasonic filter settings.
func (c *Handler) RerunUnlikely(ctx echo.Context) error {
	writer := c.verdictWriter()
	if writer == nil {
		return c.respondEnhancedDatabaseRequired(ctx, "Re-running ultrasonic validation requires the enhanced (v2) database",
			"ultrasonic validation requested without the enhanced database")
	}
	id, err := parseDetectionID(ctx)
	if err != nil {
		return c.HandleError(ctx, err, invalidDetectionIDMessage, http.StatusBadRequest)
	}
	reqCtx := ctx.Request().Context()
	result, err := c.Repo.Get(reqCtx, ctx.Param("id"))
	if err != nil {
		if handled, responseErr := c.respondIfRequestEnded(ctx, err, rerunRequestEndedLog); handled {
			return responseErr
		}
		if errors.Is(err, repository.ErrDetectionNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			return c.HandleError(ctx, err, detectionNotFoundMessage, http.StatusNotFound)
		}
		return c.HandleError(ctx, err, "Could not load the detection for ultrasonic validation", http.StatusInternalServerError)
	}
	if result.Locked {
		return c.rerunRefused(ctx, nil, rerunLockedMessage, http.StatusConflict)
	}

	// One re-run at a time: decoding and analyzing a long ultrasonic clip can take
	// hundreds of MiB, so concurrent clicks must not multiply it.
	select {
	case rerunSlot <- struct{}{}:
		defer func() { <-rerunSlot }()
	default:
		return c.rerunRefused(ctx, nil, rerunBusyMessage, http.StatusTooManyRequests)
	}

	verdict, reason, err := c.evaluateSavedClip(reqCtx, result, c.CurrentSettings())
	if handled, responseErr := c.respondIfRequestEnded(ctx, err, rerunRequestEndedLog); handled {
		return responseErr
	}
	if reason != "" {
		// Decoder errors can carry internal file details. Return the specific public reason.
		return c.rerunRefused(ctx, nil, reason, http.StatusBadRequest)
	}

	changed, err := writer.SetUltrasonicVerdict(reqCtx, uint(id), verdict.Unlikely, verdict.Comment, processor.MatchesUltrasonicReviewComment)
	if err != nil {
		if handled, responseErr := c.respondIfRequestEnded(ctx, err, rerunRequestEndedLog); handled {
			return responseErr
		}
		switch {
		case errors.Is(err, repository.ErrDetectionNotFound):
			return c.HandleError(ctx, err, detectionNotFoundMessage, http.StatusNotFound)
		case errors.Is(err, repository.ErrDetectionLocked):
			return c.rerunRefused(ctx, err, rerunLockedMessage, http.StatusConflict)
		default:
			return c.HandleError(ctx, err, "Could not save the ultrasonic validation result", http.StatusInternalServerError)
		}
	}

	outcome := rerunUnchangedOutcome
	if changed {
		c.invalidateDetectionCache()
		outcome = rerunProbableOutcome
		if verdict.Unlikely {
			outcome = rerunUnlikelyOutcome
		}
	}
	return ctx.JSON(http.StatusOK, rerunUnlikelyResponse{
		Outcome: outcome, Unlikely: verdict.Unlikely,
		UltrasonicCV: verdict.CV, UltrasonicCVThreshold: verdict.Threshold,
	})
}

// registryIDOf returns the registry ID of the model a detection names, matched
// on its detection name and version, and whether one was found.
func registryIDOf(model detection.ModelInfo) (string, bool) {
	for id := range classifier.ModelRegistry {
		info := classifier.ModelRegistry[id]
		if info.DetectionName == model.Name && info.DetectionVersion == model.Version {
			return id, true
		}
	}
	return "", false
}

// batFilterRate returns the effective sample rate the live ultrasonic filter
// judges the model's audio at, or the public reason the filter cannot run on
// the model's detections.
func batFilterRate(model detection.ModelInfo, splitHz int) (rate int, reason string) {
	if id, found := registryIDOf(model); found {
		if classifier.TaxonGroupFor(id) != classifier.TaxonGroupBat {
			return 0, rerunNonBatMessage
		}
		if spec, ok := classifier.GetModelSpec(id); ok {
			rate = spec.EffectiveSampleRate()
			if splitHz <= 0 || splitHz >= rate || !ultrasonic.HasUltrasonicBand(rate, splitHz) {
				return 0, rerunModelBandMessage
			}
			return rate, ""
		}
	}
	return 0, rerunUnknownModelMessage
}

func (c *Handler) canRerunUnlikely(result *detection.Result) bool {
	if c.verdictWriter() == nil {
		return false
	}
	_, reason := batFilterRate(result.Model, c.CurrentSettings().Bat.UltrasonicFilter.FrequencySplitHz)
	return reason == ""
}

func (c *Handler) evaluateSavedClip(ctx context.Context, result *detection.Result, settings *conf.Settings) (verdict ultrasonicVerdict, reason string, err error) {
	cfg := settings.Bat.UltrasonicFilter
	if !cfg.Enabled {
		return verdict, rerunDisabledMessage, nil
	}
	if err := cfg.ValidateSTFT(); err != nil {
		return verdict, rerunSTFTMessage, err
	}
	if math.IsNaN(cfg.CVThreshold) || math.IsInf(cfg.CVThreshold, 0) {
		return verdict, rerunThresholdMessage, nil
	}
	rate, reason := batFilterRate(result.Model, cfg.FrequencySplitHz)
	if reason != "" {
		return verdict, reason, nil
	}
	samples, reason, err := c.savedClipSamples(ctx, result.ClipName, rate, cfg.FrequencySplitHz)
	if reason != "" {
		return verdict, reason, err
	}
	window, step := batWindow(result.Model, settings)
	cv, ok, err := highestWindowCV(ctx, samples, rate, window, step, cfg)
	if err != nil {
		return verdict, rerunCanceledMessage, err
	}
	if !ok {
		return verdict, rerunTooShortMessage, nil
	}
	verdict = ultrasonicVerdict{Unlikely: ultrasonic.IsUnlikely(cv, cfg), CV: cv, Threshold: cfg.CVThreshold}
	if verdict.Unlikely {
		verdict.Comment = processor.FormatUltrasonicReviewComment(settings.Realtime.Dashboard.Locale, cv, cfg.CVThreshold)
	}
	return verdict, "", nil
}

// savedClipSamples decodes the detection's saved clip to mono float32 at rate. On
// failure reason is the public reason and err the underlying error, if any.
func (c *Handler) savedClipSamples(ctx context.Context, clipName string, rate, splitHz int) (samples []float32, reason string, err error) {
	rel, ok := c.resolveClipRel(clipName)
	if !ok || clipName == "" {
		return nil, rerunNoClipMessage, nil
	}
	file, err := c.SFS.OpenRegularRel(rel)
	if err != nil {
		return nil, rerunNoClipMessage, err
	}
	defer func() { _ = file.Close() }()

	probe, err := transcode.ProbeOpenFile(ctx, file, rel)
	if err != nil {
		return nil, rerunDecodeMessage, err
	}
	if !ultrasonic.HasUltrasonicBand(probe.SampleRate, splitHz) {
		return nil, rerunClipBandMessage, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, rerunDecodeMessage, err
	}

	// Decode the whole saved clip at the live model's effective rate; it is then
	// judged window by window (see highestWindowCV). Saved timestamps don't identify
	// the exact hit windows within a recording with pre-capture, post-capture, or
	// multiple hits, so every window of the clip is judged.
	mono, err := transcode.DecodeMonoOpenFile(ctx, file, rel, rate)
	if errors.Is(err, transcode.ErrAudioTooLong) {
		return nil, rerunTooLongMessage, err
	}
	if err != nil {
		return nil, rerunDecodeMessage, err
	}
	span := mono.SpanSamples()
	if span > maxRerunSamples {
		return nil, rerunTooLongMessage, nil
	}
	samples = make([]float32, 0, span)
	if err := mono.Stream(ctx, func(chunk []float32) error {
		samples = append(samples, chunk...)
		return nil
	}); err != nil {
		return nil, rerunDecodeMessage, err
	}
	return samples, "", nil
}

// batWindow returns the live analysis window and step of the bat model, in samples
// of the PCM the live filter judges (at the model's effective rate): the model's
// window (ClipSampleCount, 144,000 samples for the Bat model, 0.5625 s at 256 kHz)
// stepped by its configured fixed step (BSG-BAT: bsgbat.stepframes) or, without one,
// by half a window (the bat overlap is fixed at 50%). It returns zeros when the
// model is unknown.
func batWindow(model detection.ModelInfo, settings *conf.Settings) (window, step int) {
	id, found := registryIDOf(model)
	if !found {
		return 0, 0
	}
	spec, found := classifier.RegistrySpec(id, settings)
	if !found {
		return 0, 0
	}
	window = spec.ClipSampleCount()
	step = spec.StepSamples
	if step <= 0 {
		step = window / 2
	}
	return window, max(step, 1)
}

// highestWindowCV mirrors the live pipeline's verdict on a saved clip. Live, the
// filter judges each model analysis window on its own (broadband-adjusted frame CV
// against the threshold), and a detection stays unlikely only while every hit
// window was judged flat; its comment then reports the highest window CV (see
// mergeUnlikelyVerdict in the processor). Here every window of the clip is judged,
// plus one aligned to the clip's end so the tail is not skipped, and the highest
// CV is returned: the clip is unlikely exactly when that CV is below the threshold.
// Every window includes the clip's pre- and post-capture audio, which the live
// filter never judged, so a bat call there clears the verdict. A clip no longer
// than one window is judged whole. ok is false when no window holds enough frames
// for the CV; err is the context's error when the request ends first.
func highestWindowCV(ctx context.Context, samples []float32, rate, window, step int, cfg conf.UltrasonicFilterConfig) (cv float64, ok bool, err error) {
	if window <= 0 || step <= 0 || len(samples) <= window {
		window, step = len(samples), max(len(samples), 1)
	}
	judge := func(w []float32) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		frameCV, wok := ultrasonic.ComputeUSFrameCV(w, rate, cfg)
		if !wok {
			return nil
		}
		adjusted := ultrasonic.BroadbandAdjustedCV(w, rate, cfg, frameCV)
		if !ok || adjusted > cv {
			cv, ok = adjusted, true
		}
		return nil
	}
	last := 0
	for start := 0; start+window <= len(samples); start += step {
		if err := judge(samples[start : start+window]); err != nil {
			return 0, false, err
		}
		last = start
	}
	if tail := len(samples) - window; tail > last {
		if err := judge(samples[tail:]); err != nil {
			return 0, false, err
		}
	}
	return cv, ok, nil
}
