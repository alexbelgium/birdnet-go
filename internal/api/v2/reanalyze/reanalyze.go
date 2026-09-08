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
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

// reanalysisSlot admits exactly one reanalysis at a time, process-wide.
//
// Not a rate limit and not defensive padding: it matches the endpoint's
// concurrency to the hardware's. classifier.Orchestrator serialises inference
// across every model behind one mutex, so a second concurrent reanalysis cannot
// run faster — it can only sit on a decoded PCM buffer (up to 6 MiB per sample
// rate) and a queued goroutine while it waits, on hosts that are frequently a
// Raspberry Pi also running the realtime pipeline. A non-blocking claim turns
// that into an immediate 429 the caller can act on, instead of an invisible
// queue. The frontend's own in-flight dedupe is per-tab and cannot substitute:
// two browsers, or curl, bypass it entirely.
var reanalysisSlot = make(chan struct{}, 1)

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

	// Correctable reports whether this row may be applied as a species
	// correction. False for the non-species sound classes Perch also emits
	// ("power_tool", "engine idling"), which the shared SplitSpeciesName splits
	// into a scientific/common pair like any other label — so "power_tool"
	// arrives here looking exactly like a species named "power". Without this
	// flag the UI offers "Use this" on it and the correction relabels a bird as
	// a sound class, creating a junk v2 label row in the process. Computed
	// server-side so the UI and the endpoint's own gate cannot drift apart.
	Correctable bool `json:"correctable"`
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

	// Claim the single reanalysis slot before doing any work. Released in the
	// deferred receive, including on every error path below.
	select {
	case reanalysisSlot <- struct{}{}:
		defer func() { <-reanalysisSlot }()
	default:
		return c.HandleError(ctx,
			fmt.Errorf("a reanalysis is already running"),
			"Another reanalysis is already running; try again when it finishes",
			http.StatusTooManyRequests)
	}

	// The clip lookup is also the existence check: GetNoteClipPath returns
	// ErrDetectionNotFound for an unknown id, which openClip maps to a 404. A
	// separate DS.Get would be a second round trip proving the same thing.
	clipFile, relClipPath, err := c.openClip(ctx, idStr)
	if err != nil {
		return err
	}
	defer func() { _ = clipFile.Close() }()

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

	// Accumulator, keyed by SCIENTIFIC NAME rather than by raw model label. This
	// is what makes the grid a comparison: BirdNET emits
	// "Ficedula hypoleuca_Pied Flycatcher" while Perch emits bare
	// "Ficedula hypoleuca", so keying on the raw label puts the two models'
	// verdicts on the same bird in two separate rows that never line up — which
	// is precisely the agreement the feature exists to show.
	agg := make(map[string]*speciesAggregate)
	modelInfos := make([]ReanalyzeModelInfo, 0, len(chosen))
	clipDurationSec := 0.0

	for sampleRate, models := range bySampleRate {
		// ffmpeg consumes the whole reader per decode, so rewind before each
		// sample rate. Seeking the already-open handle keeps every filesystem
		// access inside the SecureFS sandbox — reopening by path would not.
		if _, err := clipFile.Seek(0, io.SeekStart); err != nil {
			return c.HandleError(ctx, err, "Failed to read audio clip", http.StatusInternalServerError)
		}
		samples, err := decodeClipMonoPCM16(
			ctx.Request().Context(), ffmpegPath, clipFile, sampleRate, decodeMaxDurationSec)
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
				addSpeciesScore(agg, label, m.id, conf, len(chosen))
			}
		}
	}

	// Deterministic ModelsRun ordering: the outer loop walks a map keyed by sample
	// rate, so the order models are appended in is not stable across requests.
	sort.Slice(modelInfos, func(i, j int) bool { return modelInfos[i].ID < modelInfos[j].ID })

	predictions := make([]ReanalyzePrediction, 0, len(agg))
	for _, a := range agg {
		predictions = append(predictions, ReanalyzePrediction{
			ScientificName: a.scientific,
			CommonName:     a.common,
			ByModel:        a.byModel,
			Correctable:    isBinomialScientificName(a.scientific),
		})
	}
	// Fill in a locale-specific common name for anything only a bare-scientific
	// model scored (Perch v2's output shape), so every row reads the same way.
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

// speciesAggregate collects every model's verdict on one species across the whole
// clip: the best confidence each model produced, plus the best names seen for it.
type speciesAggregate struct {
	scientific string
	common     string
	byModel    map[string]float32
}

// addSpeciesScore folds one model's score for one raw label into the aggregate,
// merging on the scientific name so different label spellings for the same bird
// land in one row. modelHint sizes the per-species map to the number of models
// being run, which is its final size in the common case.
func addSpeciesScore(agg map[string]*speciesAggregate, rawLabel, modelID string, conf float32, modelHint int) {
	// SplitSpeciesName is the same splitter the realtime display path uses, so a
	// label renders here exactly as it does everywhere else in the UI. It returns
	// an empty scientific name for a multi-word non-binomial label, putting the
	// whole thing in common; key on whichever half is populated so such a row
	// still merges with itself across windows and models instead of collapsing
	// every one of them onto the "" key.
	scientific, common := classifier.SplitSpeciesName(rawLabel)
	key := scientific
	if key == "" {
		key = common
	}
	if key == "" {
		key = rawLabel
	}

	a, ok := agg[key]
	if !ok {
		a = &speciesAggregate{scientific: scientific, byModel: make(map[string]float32, modelHint)}
		agg[key] = a
	}
	// First model to supply a common name wins; a model that emits bare
	// scientific names must not blank out a name another model already gave.
	if a.common == "" {
		a.common = common
	}
	if existing, seen := a.byModel[modelID]; !seen || conf > existing {
		a.byModel[modelID] = conf
	}
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
			resolvedID := resolveModelID(raw)
			if _, dup := seen[resolvedID]; dup {
				continue
			}
			info, ok := lookupLoadedModel(infos, resolvedID)
			if !ok {
				return nil, fmt.Errorf("model %q is not loaded; enable it in Settings -> Models first", raw)
			}
			if !isStandardAudioModel(info.Spec) {
				return nil, fmt.Errorf("model %q cannot analyze a saved clip: it expects raw ultrasonic audio that recorded clips do not contain", raw)
			}
			seen[resolvedID] = struct{}{}
			out = append(out, loadedModel{id: resolvedID, name: info.Name, spec: info.Spec})
		}
		return out, nil
	}

	// Index-based loop so classifier.ModelInfo (a large struct) is not copied per
	// iteration just to read three fields.
	var out []loadedModel
	for i := range infos {
		if !isStandardAudioModel(infos[i].Spec) {
			continue
		}
		out = append(out, loadedModel{id: infos[i].ID, name: infos[i].Name, spec: infos[i].Spec})
	}
	return out, nil
}

// isStandardAudioModel reports whether a model can say anything meaningful about
// a saved detection clip. A non-zero RawSampleRate means the model expects raw
// ultrasonic audio (the bat classifier wants 256 kHz); saved clips are recorded
// at standard rates and carry no ultrasonic content, so resampling one up and
// running the model over it produces confident-looking nonsense. Rejected for
// the default set AND for an explicit request, because a number that means
// nothing is worse to show a user than an error saying so.
func isStandardAudioModel(spec classifier.ModelSpec) bool {
	return spec.RawSampleRate == 0 && spec.SampleRate > 0
}

// isBinomialScientificName reports whether s looks like a Latin binomial:
// exactly two space-separated words, the first capitalised and the second not.
// This is the same rule classifier.isBinomialName applies, reimplemented here
// because that one is unexported.
//
// It is what separates a real species from the sound classes Perch also emits.
// SplitSpeciesName has no notion of the difference — it splits "power_tool" into
// ("power", "tool") exactly as it splits a BirdNET label — so the shape of the
// scientific half is the only signal available.
func isBinomialScientificName(s string) bool {
	words := strings.Fields(s)
	if len(words) != 2 {
		return false
	}
	first, _ := utf8.DecodeRuneInString(words[0])
	second, _ := utf8.DecodeRuneInString(words[1])
	return first != utf8.RuneError && second != utf8.RuneError &&
		unicode.IsUpper(first) && unicode.IsLower(second)
}

// lookupLoadedModel returns the model with the given registry ID, or ok=false
// when it is not in the supplied set. Indexed loop so the (large)
// classifier.ModelInfo is copied exactly once, for the entry actually returned.
func lookupLoadedModel(infos []classifier.ModelInfo, modelID string) (info classifier.ModelInfo, ok bool) {
	for i := range infos {
		if infos[i].ID == modelID {
			return infos[i], true
		}
	}
	return classifier.ModelInfo{}, false
}

// resolveModelID maps a user-facing config alias ("birdnet", "perch_v2") onto the
// orchestrator registry ID ("BirdNET_V2.4", "Perch_V2"), passing through anything
// that is already a registry ID. Both endpoints accept either spelling.
func resolveModelID(raw string) string {
	if registryID, ok := classifier.ResolveConfigModelID(raw); ok {
		return registryID
	}
	return raw
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
) (best map[string]float32, windowCount int, err error) {
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

	best = make(map[string]float32)
	runWindow := func(offset int) error {
		// Stop as soon as the client goes away. PredictModel blocks on a
		// non-cancellable process-wide mutex, so ctx is not observed inside a
		// single window; checking between windows bounds the wasted inference on
		// an abandoned request to one window instead of the whole clip.
		if err := ctx.Err(); err != nil {
			return err
		}
		results, err := predict(ctx, modelID, [][]float32{samples[offset : offset+clipLen]})
		if err != nil {
			return err
		}
		windowCount++
		for _, r := range results {
			if existing, ok := best[r.Species]; !ok || r.Confidence > existing {
				best[r.Species] = r.Confidence
			}
		}
		return nil
	}

	covered := 0
	for offset := 0; offset+clipLen <= len(samples); offset += stride {
		if err := runWindow(offset); err != nil {
			return nil, windowCount, err
		}
		covered = offset + clipLen
	}
	// The strided walk leaves a tail unanalyzed whenever the clip length is not a
	// whole number of strides past one window — a 4 s clip through a 3 s model
	// runs only offset 0, so its final second is never looked at. Close that with
	// one window anchored at the END of the clip. It overlaps the previous window,
	// which costs nothing: aggregation is max-per-species, so a double look can
	// only confirm a score, never inflate one.
	if covered < len(samples) {
		if err := runWindow(len(samples) - clipLen); err != nil {
			return nil, windowCount, err
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

// openClip looks up the detection's saved clip and opens it THROUGH SecureFS,
// returning the open handle and the normalized relative path (for logging). On
// failure it returns an already-formed echo error response, so callers `return err`.
//
// Opening rather than resolving to an absolute path is the point: SecureFS wraps
// os.Root, which refuses to follow a symlink out of the media root. A path handed
// to an external process leaves that enforcement behind, so a symlink planted in
// the clips directory would be followed by ffmpeg. The caller pipes this handle
// to ffmpeg's stdin instead.
//
// The normalization mirrors the media domain's normalizeAndValidatePathWithLogger,
// which is unexported there. Recomposing it from the exported
// apicore.NormalizeClipPath keeps this package's upstream footprint at zero;
// importing a sibling domain would break the api/v2 acyclic-import rule that
// apicore's import guard enforces.
func (c *Handler) openClip(ctx echo.Context, idStr string) (clip *os.File, relPath string, err error) {
	clipPath, err := c.DS.GetNoteClipPath(idStr)
	switch {
	case err != nil && isDetectionNotFoundErr(err):
		return nil, "", c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
	case err != nil && isClipNotFoundErr(err):
		return nil, "", c.HandleError(ctx, err,
			"No audio clip available for this detection", http.StatusNotFound)
	case err != nil:
		return nil, "", c.HandleError(ctx, err,
			"Failed to look up clip path", http.StatusInternalServerError)
	case clipPath == "":
		return nil, "", c.HandleError(ctx, fmt.Errorf("clip path empty"),
			"No audio clip available for this detection", http.StatusNotFound)
	}

	normalized := apicore.NormalizeClipPath(clipPath, c.CurrentSettings().Realtime.Audio.Export.Path)
	if normalized == "" {
		return nil, "", c.HandleError(ctx, fmt.Errorf("empty normalized clip path"),
			"Invalid clip path", http.StatusBadRequest)
	}
	rel, err := c.SFS.ValidateRelativePath(normalized)
	if err != nil {
		return nil, "", c.HandleError(ctx, err, "Invalid clip path", http.StatusBadRequest)
	}
	f, err := c.SFS.Open(rel)
	if err != nil {
		if isClipNotFoundErr(err) {
			return nil, "", c.HandleError(ctx, err,
				"No audio clip available for this detection", http.StatusNotFound)
		}
		return nil, "", c.HandleError(ctx, err, "Failed to open audio clip", http.StatusInternalServerError)
	}
	return f, rel, nil
}

// isDetectionNotFoundErr reports whether err means the detection row itself does
// not exist, across the three shapes the datastore layer produces for it: the v2
// repository sentinel, a bare GORM miss, and the legacy DataStore.Get path, which
// returns a CategoryNotFound enhanced error that does NOT wrap
// gorm.ErrRecordNotFound — checking only the sentinels turns a legacy 404 into a
// 500.
func isDetectionNotFoundErr(err error) bool {
	return stderrors.Is(err, repository.ErrDetectionNotFound) ||
		stderrors.Is(err, gorm.ErrRecordNotFound) ||
		errors.IsNotFound(err)
}

// isClipNotFoundErr reports whether err means the clip OR its parent detection
// does not exist. Same sentinel set the media domain checks; duplicated here
// rather than exported from media, because importing a sibling domain would
// break the api/v2 acyclic-import rule that apicore's import guard enforces.
func isClipNotFoundErr(err error) bool {
	return isDetectionNotFoundErr(err) ||
		stderrors.Is(err, os.ErrNotExist) ||
		stderrors.Is(err, repository.ErrNoClipPath)
}
