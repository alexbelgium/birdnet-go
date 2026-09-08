// reanalyze.go — POST /api/v2/detections/:id/reanalyze.
//
// Re-runs inference on a saved detection's audio clip against one or more
// classifier models. Lets an operator ask "what does every classifier I have
// think about this clip?" without persisting anything — the alternate
// predictions are returned in the response and never written to the datastore.
//
// By default the endpoint runs every currently-loaded classifier compatible with
// the saved clip (i.e. non-ultrasonic models, since detection clips are recorded
// at standard audio rates). Callers override that with an explicit modelIds list.
package reanalyze

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// errComponent is the errors.Builder component tag for everything in this
// package, so telemetry attributes reanalysis failures to one place.
const errComponent = "api/v2/reanalyze"

const (
	// decodeMaxDurationSec caps the input duration fed to ffmpeg per request.
	// 60s comfortably covers the default extended capture buffer plus pre/post
	// padding; longer clips are truncated. A hard ceiling on inference cost.
	decodeMaxDurationSec = 60

	// reanalyzeTopN is the maximum number of species predictions returned,
	// ranked by the highest confidence any participating model produced.
	reanalyzeTopN = 10
)

// ReanalyzeRequest is the JSON body of POST /api/v2/detections/:id/reanalyze.
type ReanalyzeRequest struct {
	// ModelIDs optionally restricts which models to run. Each entry may be the
	// orchestrator registry ID ("Perch_V2", "BirdNET_V2.4") or the user-facing
	// config alias ("perch_v2", "birdnet"). Empty (the common case) means every
	// currently-loaded compatible classifier.
	ModelIDs []string `json:"modelIds,omitempty"`
}

// ReanalyzeModelInfo describes a model that participated in the reanalysis.
type ReanalyzeModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SampleRate  int    `json:"sampleRate"`
	WindowCount int    `json:"windowCount"`
}

// ReanalyzePrediction is one species entry in the response. ByModel maps each
// participating model's registry ID to the highest confidence that model gave
// this species across all windows of the clip; a model absent from the map did
// not predict the species in any window.
type ReanalyzePrediction struct {
	ScientificName string             `json:"scientificName"`
	CommonName     string             `json:"commonName,omitempty"`
	ByModel        map[string]float32 `json:"byModel"`
}

// MaxConfidence returns the highest confidence any model produced for this
// species. Used to rank the top-N list.
func (p *ReanalyzePrediction) MaxConfidence() float32 {
	var best float32
	for _, c := range p.ByModel {
		if c > best {
			best = c
		}
	}
	return best
}

// ReanalyzeResponse is the JSON shape returned by the reanalysis endpoint.
type ReanalyzeResponse struct {
	DetectionID     uint                  `json:"detectionId"`
	ClipDurationSec float64               `json:"clipDurationSec"`
	ModelsRun       []ReanalyzeModelInfo  `json:"modelsRun"`
	Predictions     []ReanalyzePrediction `json:"predictions"`
}

// ReanalyzeDetection handles POST /api/v2/detections/:id/reanalyze.
// @Summary Reanalyze a saved detection clip with one or more models
// @Description Decodes the saved audio clip and runs it through the requested
// @Description classifier models (or every compatible loaded model when none is
// @Description specified). Returns the top-N species predictions with per-model
// @Description max confidence. Does not modify the detection record.
// @Tags detections
// @Accept json
// @Produce json
// @Param id path int true "Detection (note) ID"
// @Param request body ReanalyzeRequest false "Model selection (optional; defaults to all compatible)"
// @Success 200 {object} ReanalyzeResponse "Top-N predictions across the chosen models"
// @Failure 400 {object} apicore.ErrorResponse "Invalid request or unknown model"
// @Failure 404 {object} apicore.ErrorResponse "Detection or audio clip not found"
// @Failure 500 {object} apicore.ErrorResponse "Decode or inference failure"
// @Failure 503 {object} apicore.ErrorResponse "Classifier orchestrator unavailable"
// @Router /detections/{id}/reanalyze [post]
func (c *Handler) ReanalyzeDetection(ctx echo.Context) error {
	idStr := ctx.Param("id")
	detectionID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return c.HandleError(ctx, err, "Detection ID must be a numeric value", http.StatusBadRequest)
	}

	// An empty body means "run all compatible loaded models", which Echo's Bind
	// reports as success. Only malformed JSON or a wrong content-type lands in
	// the error branch, and that should fail loudly rather than silently giving
	// a buggy client the default behaviour it did not ask for.
	req := &ReanalyzeRequest{}
	if err := ctx.Bind(req); err != nil {
		return c.HandleError(ctx, err, "Invalid request body", http.StatusBadRequest)
	}

	// The clip lookup is also the existence check: GetNoteClipPath returns
	// ErrDetectionNotFound for an unknown id, which resolveClipPath maps to a 404.
	// A separate DS.Get would be a second round trip proving the same thing.
	absClipPath, relClipPath, err := c.resolveClipPath(ctx, idStr)
	if err != nil {
		return err
	}

	bn, err := c.GetBirdNETInstance()
	if err != nil {
		return c.HandleError(ctx, err, "Classifier not available", http.StatusServiceUnavailable)
	}

	chosen, err := selectModelsForReanalysis(bn.ModelInfos(), req.ModelIDs)
	if err != nil {
		return c.HandleError(ctx, err, err.Error(), http.StatusBadRequest)
	}
	if len(chosen) == 0 {
		return c.HandleError(ctx,
			fmt.Errorf("no compatible models loaded"),
			"No compatible classifier models are loaded for this clip",
			http.StatusBadRequest)
	}

	// Group models by sample rate so the clip is decoded once per unique rate
	// rather than once per model. BirdNET v2.4 (48k) + Perch v2 (32k) is two
	// decodes no matter how many models share either rate.
	bySampleRate := make(map[int][]loadedModel, len(chosen))
	for _, m := range chosen {
		bySampleRate[m.spec.SampleRate] = append(bySampleRate[m.spec.SampleRate], m)
	}

	ffmpegPath := c.CurrentSettings().Realtime.Audio.FfmpegPath

	// Accumulator: raw model label -> per-model max confidence. Keying on the raw
	// label until aggregation keeps duplicates across windows of one model
	// merging correctly before SplitSpeciesName is applied.
	byLabelModel := make(map[string]map[string]float32)
	modelInfos := make([]ReanalyzeModelInfo, 0, len(chosen))
	clipDurationSec := 0.0

	for sampleRate, models := range bySampleRate {
		samples, err := decodeClipMonoPCM16(
			ctx.Request().Context(), ffmpegPath, absClipPath, sampleRate, decodeMaxDurationSec)
		if err != nil {
			c.LogAPIRequest(ctx, logger.LogLevelError, "Failed to decode clip for reanalysis",
				logger.String("detection_id", idStr),
				logger.String("clip_path", relClipPath),
				logger.Int("sample_rate", sampleRate),
				logger.Error(err))
			return c.HandleError(ctx, err, "Failed to decode audio clip", http.StatusInternalServerError)
		}
		// All decodes of one source yield the same duration modulo resampler
		// edge effects; take the longest view.
		if d := float64(len(samples)) / float64(sampleRate); d > clipDurationSec {
			clipDurationSec = d
		}

		// Models are run one after another, not concurrently. classifier's
		// Orchestrator holds a single process-wide inferenceMu that serialises
		// inference across ALL models (orchestrator.go: "serializes inference across
		// all models"), so a goroutine fan-out here would buy no parallelism — it
		// would only pile N goroutines onto one non-cancellable mutex that the
		// realtime pipeline is also queueing on.
		for _, m := range models {
			scores, windowCount, err := reanalyzeSamples(
				ctx.Request().Context(), bn.PredictModel, m.id, m.spec, samples)
			if err != nil {
				c.LogAPIRequest(ctx, logger.LogLevelError, "Reanalysis inference failed",
					logger.String("detection_id", idStr),
					logger.String("model_id", m.id),
					logger.Error(err))
				return c.HandleError(ctx, fmt.Errorf("inference for %s: %w", m.id, err),
					"Inference failed", http.StatusInternalServerError)
			}
			modelInfos = append(modelInfos, ReanalyzeModelInfo{
				ID:          m.id,
				Name:        m.name,
				SampleRate:  m.spec.SampleRate,
				WindowCount: windowCount,
			})
			for label, conf := range scores {
				perModel, ok := byLabelModel[label]
				if !ok {
					perModel = make(map[string]float32, len(chosen))
					byLabelModel[label] = perModel
				}
				if existing, seen := perModel[m.id]; !seen || conf > existing {
					perModel[m.id] = conf
				}
			}
		}
	}

	// Deterministic ModelsRun ordering: the outer loop walks a map keyed by sample
	// rate, so the order models are appended in is not stable across requests.
	sort.Slice(modelInfos, func(i, j int) bool { return modelInfos[i].ID < modelInfos[j].ID })

	// SplitSpeciesName is applied here so the frontend gets common name primary +
	// scientific secondary, and the resolver fills locale-specific common names
	// for bare-scientific labels (Perch v2's output shape).
	predictions := make([]ReanalyzePrediction, 0, len(byLabelModel))
	for label, perModel := range byLabelModel {
		scientific, common := classifier.SplitSpeciesName(label)
		if scientific == "" {
			// SplitSpeciesName could not parse a scientific name out of the raw
			// label. Keep the label visible rather than emitting a blank row.
			scientific = label
			common = ""
		}
		predictions = append(predictions, ReanalyzePrediction{
			ScientificName: scientific,
			CommonName:     common,
			ByModel:        perModel,
		})
	}
	applyLocalizedCommonNames(bn, predictions, c.CurrentLocale())

	sort.Slice(predictions, func(i, j int) bool {
		return predictions[i].MaxConfidence() > predictions[j].MaxConfidence()
	})
	if len(predictions) > reanalyzeTopN {
		predictions = predictions[:reanalyzeTopN]
	}

	c.LogAPIRequest(ctx, logger.LogLevelInfo, "Reanalysis complete",
		logger.String("detection_id", idStr),
		logger.Int("model_count", len(modelInfos)),
		logger.Int("prediction_count", len(predictions)))

	return ctx.JSON(http.StatusOK, ReanalyzeResponse{
		DetectionID:     uint(detectionID),
		ClipDurationSec: clipDurationSec,
		ModelsRun:       modelInfos,
		Predictions:     predictions,
	})
}

// loadedModel carries the registry ID, display name, and spec of a model that is
// currently loaded by the orchestrator.
type loadedModel struct {
	id   string
	name string
	spec classifier.ModelSpec
}

// selectModelsForReanalysis resolves the request's model list (or the default set
// when it is empty) into the concrete set of loaded models to run. It takes the
// orchestrator's model snapshot rather than the orchestrator itself so it is
// unit-testable without a loaded model file on disk.
//
// Default policy: every loaded model with Spec.RawSampleRate == 0, i.e. every
// standard-audio model. Ultrasonic models (bat detection, RawSampleRate=256000)
// are excluded because saved detection clips do not carry ultrasonic content, so
// running them would produce meaningless scores.
//
// Explicit policy: each entry is resolved (config alias or registry ID) and must
// be loaded. An unknown or unloaded ID is an error rather than a silent drop, so
// the caller learns its request was not honoured.
func selectModelsForReanalysis(infos []classifier.ModelInfo, requestedIDs []string) ([]loadedModel, error) {
	if len(requestedIDs) > 0 {
		// Dedupe after registry-ID resolution so {"modelIds":["birdnet","BirdNET_V2.4"]}
		// does not schedule the same model twice (both resolve to "BirdNET_V2.4"),
		// which would duplicate modelsRun entries and burn inference cost.
		out := make([]loadedModel, 0, len(requestedIDs))
		seen := make(map[string]struct{}, len(requestedIDs))
		for _, raw := range requestedIDs {
			resolvedID := raw
			if registryID, ok := classifier.ResolveConfigModelID(raw); ok {
				resolvedID = registryID
			}
			if _, dup := seen[resolvedID]; dup {
				continue
			}
			spec, name, ok := lookupLoadedModel(infos, resolvedID)
			if !ok {
				return nil, fmt.Errorf("model %q is not loaded; enable it in Settings -> Models first", raw)
			}
			seen[resolvedID] = struct{}{}
			out = append(out, loadedModel{id: resolvedID, name: name, spec: spec})
		}
		return out, nil
	}

	// Index-based loop so classifier.ModelInfo (a large struct) is not copied per
	// iteration just to read three fields.
	var out []loadedModel
	for i := range infos {
		if infos[i].Spec.RawSampleRate != 0 || infos[i].Spec.SampleRate <= 0 {
			continue
		}
		out = append(out, loadedModel{id: infos[i].ID, name: infos[i].Name, spec: infos[i].Spec})
	}
	return out, nil
}

// lookupLoadedModel returns the spec and display name of the model with the given
// registry ID, or ok=false when it is not in the supplied set.
func lookupLoadedModel(infos []classifier.ModelInfo, modelID string) (spec classifier.ModelSpec, name string, ok bool) {
	for i := range infos {
		if infos[i].ID == modelID {
			return infos[i].Spec, infos[i].Name, true
		}
	}
	return classifier.ModelSpec{}, "", false
}

// predictModelFn is the subset of *classifier.Orchestrator that reanalyzeSamples
// depends on. Factoring it out lets tests pass a stub instead of instantiating a
// real orchestrator, which would require a model file on disk.
type predictModelFn func(ctx context.Context, modelID string, sample [][]float32) ([]datastore.Results, error)

// reanalyzeSamples slides a clip-length window across the decoded audio at 50%
// overlap (matching the realtime pipeline) and dispatches each window to predict.
// Per raw label it keeps the maximum confidence seen across all windows.
//
// Returns (label -> max confidence, window count, error). Ranking and top-N
// truncation are the multi-model aggregator's job, not this function's, because
// it is called once per model over the same clip.
func reanalyzeSamples(
	ctx context.Context,
	predict predictModelFn,
	modelID string,
	spec classifier.ModelSpec,
	samples []float32,
) (map[string]float32, int, error) {
	if len(samples) == 0 {
		return nil, 0, errors.Newf("no audio samples to analyze").
			Component(errComponent).
			Category(errors.CategoryValidation).
			Build()
	}

	// Nanosecond math rather than Seconds()*SampleRate: the registry only ships
	// whole-second windows today (3s BirdNET, 5s Perch), so this is future-proofing
	// for a fractional-window model, but it costs nothing and removes a silent
	// off-by-N-samples failure mode if one ever appears.
	clipLen := int(spec.ClipLength.Nanoseconds() * int64(spec.SampleRate) / int64(time.Second))
	if clipLen <= 0 {
		return nil, 0, errors.Newf("model %q has invalid clip length", modelID).
			Component(errComponent).
			Category(errors.CategoryValidation).
			Context("model_id", modelID).
			Context("clip_length_sec", spec.ClipLength.Seconds()).
			Context("sample_rate", spec.SampleRate).
			Build()
	}

	if len(samples) < clipLen {
		padded := make([]float32, clipLen)
		copy(padded, samples)
		samples = padded
	}

	stride := clipLen / 2
	if stride <= 0 {
		stride = clipLen
	}

	best := make(map[string]float32)
	windowCount := 0
	for offset := 0; offset+clipLen <= len(samples); offset += stride {
		// Stop as soon as the client goes away. PredictModel blocks on a
		// non-cancellable process-wide mutex, so ctx is not observed inside a
		// single window; checking between windows bounds the wasted inference on
		// an abandoned request to one window instead of the whole clip.
		if err := ctx.Err(); err != nil {
			return nil, windowCount, err
		}
		window := samples[offset : offset+clipLen]
		results, err := predict(ctx, modelID, [][]float32{window})
		if err != nil {
			return nil, windowCount, err
		}
		windowCount++
		for _, r := range results {
			if existing, ok := best[r.Species]; !ok || r.Confidence > existing {
				best[r.Species] = r.Confidence
			}
		}
	}
	return best, windowCount, nil
}

// applyLocalizedCommonNames fills in CommonName via the orchestrator's resolver
// chain in the configured locale. Predictions that already carry a model-supplied
// common name keep it; only bare-scientific labels (Perch v2's shape) get resolved.
func applyLocalizedCommonNames(bn *classifier.Orchestrator, preds []ReanalyzePrediction, locale string) {
	for i := range preds {
		if preds[i].CommonName != "" || preds[i].ScientificName == "" {
			continue
		}
		if name := bn.ResolveName(preds[i].ScientificName, locale); name != "" {
			preds[i].CommonName = name
		}
	}
}

// resolveClipPath looks up the detection's saved clip and maps it onto the media
// SecureFS root, returning (absolute path, normalized relative path). On failure
// it returns an already-formed echo error response, so callers `return err`.
//
// This mirrors the media domain's normalizeAndValidatePathWithLogger, which is
// unexported there. Recomposing it from the exported apicore.NormalizeClipPath +
// SFS.ValidateRelativePath is cheaper than exporting a helper out of the media
// domain, and keeps this package's upstream footprint at zero.
func (c *Handler) resolveClipPath(ctx echo.Context, idStr string) (absPath, relPath string, err error) {
	clipPath, err := c.DS.GetNoteClipPath(idStr)
	switch {
	case stderrors.Is(err, repository.ErrDetectionNotFound), stderrors.Is(err, gorm.ErrRecordNotFound):
		return "", "", c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
	case err != nil && isClipNotFoundErr(err):
		return "", "", c.HandleError(ctx, err,
			"No audio clip available for this detection", http.StatusNotFound)
	case err != nil:
		return "", "", c.HandleError(ctx, err,
			"Failed to look up clip path", http.StatusInternalServerError)
	case clipPath == "":
		return "", "", c.HandleError(ctx, fmt.Errorf("clip path empty"),
			"No audio clip available for this detection", http.StatusNotFound)
	}

	normalized := apicore.NormalizeClipPath(clipPath, c.CurrentSettings().Realtime.Audio.Export.Path)
	if normalized == "" {
		return "", "", c.HandleError(ctx, fmt.Errorf("empty normalized clip path"),
			"Invalid clip path", http.StatusBadRequest)
	}
	rel, err := c.SFS.ValidateRelativePath(normalized)
	if err != nil {
		return "", "", c.HandleError(ctx, err, "Invalid clip path", http.StatusBadRequest)
	}
	return filepath.Join(c.SFS.BaseDir(), rel), rel, nil
}

// isClipNotFoundErr reports whether err means the clip or its parent detection
// does not exist. Same sentinel set the media domain checks; duplicated here (four
// lines) rather than exported from media, which would make this package depend on
// a sibling domain and break the api/v2 acyclic-import rule.
func isClipNotFoundErr(err error) bool {
	return stderrors.Is(err, os.ErrNotExist) ||
		stderrors.Is(err, gorm.ErrRecordNotFound) ||
		stderrors.Is(err, repository.ErrDetectionNotFound) ||
		stderrors.Is(err, repository.ErrNoClipPath)
}
