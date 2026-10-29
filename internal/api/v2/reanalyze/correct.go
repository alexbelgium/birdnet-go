// correct.go — POST /api/v2/detections/:id/correct-species.
//
// Operator-driven correction of a detection's species. After running the
// reanalyze endpoint and seeing what the other loaded classifiers think, the user
// picks "this is the right one"; this endpoint rewrites the detection's species
// and confidence in place and marks it verified='correct' in the same step.
//
// Persistence design — the reason this file exists rather than a datastore method:
// the correction is written here, from the API domain package, by composing the
// exported v2 repositories against a transactional *gorm.DB from V2Manager: model
// lookup -> label lookup -> detection Update -> review upsert. datastore.Interface
// and datastore.DetectionRepository are NOT extended, so no mockery mock is
// regenerated and no upstream datastore file is touched. The repositories' own
// atomic lock guard and OnConflict semantics are reused as-is; the transaction is
// just an envelope around them.
//
// Corrections are v2-only. The legacy schema is deprecated and being removed
// upstream, so a write path for it would break this package on the merge that
// deletes it. Legacy installs get a 501; reanalysis itself still works there.
package reanalyze

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"

	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository"
	"github.com/tphakala/birdnet-go/internal/detection"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// errDetectionLocked is this package's sentinel for "the detection was locked when
// the write ran", so the handler maps that condition to 409.
var errDetectionLocked = errors.NewStd("detection is locked")

// correctionNoteTimeout bounds the audit-note write. It runs on a context
// detached from the request, so without its own deadline a stalled database
// would hold the response open with nothing left to cancel it.
const correctionNoteTimeout = 5 * time.Second

const (
	// taxonomicClassAves is the v2 taxonomic class assigned to a newly created
	// label for a corrected bird species (entities.DefaultTaxonomicClasses seeds it).
	taxonomicClassAves = "Aves"
)

// CorrectSpeciesRequest is the JSON body of
// POST /api/v2/detections/:id/correct-species.
type CorrectSpeciesRequest struct {
	// ScientificName is the binomial the user is asserting, e.g.
	// "Ficedula hypoleuca". The gate against arbitrary species names is ModelID:
	// the correction must be attributed to a loaded model, and on v2 installs the
	// (species, model) pair must exist in that model's label vocabulary.
	ScientificName string `json:"scientificName"`

	// ModelID is the orchestrator registry ID ("BirdNET_V2.4", "Perch_V2") or the
	// user-facing config alias ("birdnet"). Required.
	ModelID string `json:"modelId"`

	// Confidence is the value to record on the corrected detection, normally the
	// max confidence the chosen model gave this species during reanalysis. [0,1].
	Confidence float64 `json:"confidence"`
}

// CorrectSpeciesResponse describes the corrected detection.
type CorrectSpeciesResponse struct {
	DetectionID    uint    `json:"detectionId"`
	ScientificName string  `json:"scientificName"`
	CommonName     string  `json:"commonName"`
	ModelID        string  `json:"modelId"`
	ModelName      string  `json:"modelName"`
	Confidence     float64 `json:"confidence"`
	Verified       string  `json:"verified"`
}

// CorrectDetectionSpecies handles POST /api/v2/detections/:id/correct-species.
// @Summary Correct a detection's species and mark it verified
// @Description Replaces the detection's species and confidence with the chosen
// @Description prediction (typically from a reanalyze response) and records a
// @Description 'correct' review, atomically. The detection must be unlocked.
// @Tags detections
// @Accept json
// @Produce json
// @Param id path int true "Detection ID"
// @Param request body CorrectSpeciesRequest true "Correction payload"
// @Success 200 {object} CorrectSpeciesResponse "Updated detection state"
// @Failure 400 {object} apicore.ErrorResponse "Invalid request or unknown model"
// @Failure 404 {object} apicore.ErrorResponse "Detection not found"
// @Failure 409 {object} apicore.ErrorResponse "Detection is locked"
// @Failure 500 {object} apicore.ErrorResponse "Database failure"
// @Router /detections/{id}/correct-species [post]
func (c *Handler) CorrectDetectionSpecies(ctx echo.Context) error {
	idStr := ctx.Param("id")
	noteID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return c.HandleError(ctx, err, "Detection ID must be a numeric value", http.StatusBadRequest)
	}

	req := &CorrectSpeciesRequest{}
	if err := ctx.Bind(req); err != nil {
		return c.HandleError(ctx, err, "Invalid request body", http.StatusBadRequest)
	}
	switch {
	case req.ScientificName == "":
		return c.HandleError(ctx, fmt.Errorf("scientificName is required"),
			"scientificName is required", http.StatusBadRequest)
	case req.ModelID == "":
		return c.HandleError(ctx, fmt.Errorf("modelId is required"),
			"modelId is required", http.StatusBadRequest)
	case req.Confidence < 0 || req.Confidence > 1:
		return c.HandleError(ctx, fmt.Errorf("confidence %.4f out of range", req.Confidence),
			"confidence must be in [0, 1]", http.StatusBadRequest)
	}

	if !c.v2SchemaActive() {
		return c.HandleError(ctx, fmt.Errorf("species correction requires the v2 database"),
			"Species correction requires the v2 database; migrate the database first",
			http.StatusNotImplemented)
	}

	// Read-side context for the structured log diff below, and the 404/409 fast
	// paths. The authoritative lock guard runs inside the write itself, atomically.
	existing, ok, respErr := c.loadDetection(ctx, idStr)
	if !ok {
		return respErr
	}
	if existing.Locked {
		return c.HandleError(ctx, fmt.Errorf("detection is locked"),
			"Detection is locked and cannot be corrected; unlock it first", http.StatusConflict)
	}

	bn, err := c.GetBirdNETInstance()
	if err != nil {
		return c.HandleError(ctx, err, "Classifier not available", http.StatusServiceUnavailable)
	}
	resolvedID := resolveModelID(req.ModelID)
	chosen, modelExists := lookupLoadedModel(bn.ModelInfos(), resolvedID)
	if !modelExists || chosen.DetectionName == "" || chosen.DetectionVersion == "" {
		return c.HandleError(ctx, fmt.Errorf("model %q is not loaded", req.ModelID),
			"Specified model is not loaded; cannot apply correction", http.StatusBadRequest)
	}
	// Only a model reanalysis could have run on this detection may be credited
	// with a correction: bat models for a bat detection, non-bat models otherwise
	// (the same isCompatibleModel rule).
	if !isCompatibleModel(chosen.Spec, isBatDetection(existing)) {
		return c.HandleError(ctx, fmt.Errorf("model %q does not match the detection's model type", req.ModelID),
			"Specified model is not applicable to this detection's audio; cannot apply correction",
			http.StatusBadRequest)
	}

	// Gate the species against the loaded classifiers' combined vocabulary. This
	// is the only thing standing between a typo'd scientificName and permanent
	// data damage: the legacy path writes the string straight onto the note, and
	// on v2 the label GetOrCreate would mint a junk label row that then shows up
	// in every species list forever. AllLabels is the unfiltered union across all
	// loaded models, so anything the reanalysis grid could have offered passes.
	// The binomial check comes first and is the one that stops a sound class.
	// "power_tool" splits to ("power", "tool") just like a species label, so it
	// is present in AllLabels and would sail through the vocabulary check below;
	// only its shape gives it away. Without this, an operator (or a direct API
	// call) can relabel a bird detection as a species called "power" and mint a
	// junk v2 label row for it.
	if !isBinomialScientificName(req.ScientificName) {
		return c.HandleError(ctx,
			fmt.Errorf("scientificName %q is not a Latin binomial", req.ScientificName),
			"That prediction is a sound class, not a species; it cannot be applied as a correction",
			http.StatusBadRequest)
	}
	if !speciesKnownToLoadedModels(bn, req.ScientificName) {
		return c.HandleError(ctx,
			fmt.Errorf("species %q is not in any loaded model's vocabulary", req.ScientificName),
			"No loaded classifier knows that species; check the spelling of the scientific name",
			http.StatusBadRequest)
	}

	// Locale-appropriate common name from the orchestrator's resolver chain, so a
	// corrected detection reads the same as an organically detected one. Falls back
	// to the scientific name for species outside the vocabulary.
	commonName := bn.ResolveName(req.ScientificName, c.CurrentLocale())
	if commonName == "" {
		commonName = req.ScientificName
	}

	if err := c.writeSpeciesCorrectionV2(ctx.Request().Context(), uint(noteID),
		req.ScientificName, req.Confidence, chosen.ToDetectionModelInfo(),
	); err != nil {
		c.LogAPIRequest(ctx, logger.LogLevelError, "Species correction failed",
			logger.String("detection_id", idStr),
			logger.String("model_id", resolvedID),
			logger.String("scientific_name", req.ScientificName),
			logger.Error(err))

		// Map the known sentinels to precise statuses; anything else stays 500
		// with a generic client message so schema internals do not leak. The real
		// error is in the structured log above.
		switch {
		case errors.Is(err, errDetectionLocked):
			return c.HandleError(ctx, err,
				"Failed to correct species: detection is locked", http.StatusConflict)
		case errors.Is(err, repository.ErrDetectionNotFound):
			return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
		case errors.Is(err, repository.ErrModelNotFound), errors.Is(err, repository.ErrLabelNotFound):
			return c.HandleError(ctx, err,
				"The chosen model does not know this species; cannot apply correction",
				http.StatusBadRequest)
		default:
			return c.HandleError(ctx, err,
				"Internal error while correcting species", http.StatusInternalServerError)
		}
	}

	// Invalidate the detection cache so list and dashboard queries reflect the new
	// label immediately. Without this the 5-minute cache keeps serving the
	// pre-correction species: the operator sees the correction stick on the detail
	// page while the parent list still shows the old name. Same thing the delete,
	// review and lock handlers do.
	c.DetectionCache.Invalidate()

	// Record what changed in the detection's notes, so the correction leaves an
	// audit trail next to the detection rather than only in the server log.
	//
	// Done AFTER the write, and deliberately best-effort: the correction has
	// already committed, so failing the request here would report a failure for
	// something that succeeded and invite the operator to retry it. The failure
	// is logged instead.
	//
	// The context is detached from the request but given its own deadline.
	// Detaching matters because by this point the response is decided: if the
	// client has disconnected, or hit its timeout after a slow correction, the
	// request context is already cancelled and passing it straight through would
	// skip the note exactly when a correction did land — when the audit trail
	// matters most. The deadline matters because this write is synchronous, ahead
	// of ctx.JSON, so a detached context with no bound would let a stalled
	// database hold the response and its goroutine open indefinitely.
	if note := correctionNote(existing, req.ScientificName, commonName, &chosen, req.Confidence); note != "" {
		if c.Repo == nil {
			c.LogAPIRequest(ctx, logger.LogLevelWarn, "Correction note not recorded: no detection repository",
				logger.String("detection_id", idStr))
		} else {
			noteCtx, cancelNote := context.WithTimeout(
				context.WithoutCancel(ctx.Request().Context()), correctionNoteTimeout)
			if err := c.Repo.AddComment(noteCtx, idStr, note); err != nil {
				c.LogAPIRequest(ctx, logger.LogLevelWarn, "Correction applied but the note could not be saved",
					logger.String("detection_id", idStr),
					logger.Error(err))
			}
			cancelNote()
		}
	}

	c.LogAPIRequest(ctx, logger.LogLevelInfo, "Species correction applied",
		logger.String("detection_id", idStr),
		logger.String("model_id", resolvedID),
		logger.String("from_species", existing.Species.ScientificName),
		logger.String("to_species", req.ScientificName),
		logger.Float64("from_confidence", existing.Confidence),
		logger.Float64("to_confidence", req.Confidence))

	return ctx.JSON(http.StatusOK, CorrectSpeciesResponse{
		DetectionID:    existing.ID,
		ScientificName: req.ScientificName,
		CommonName:     commonName,
		ModelID:        resolvedID,
		ModelName:      chosen.Name,
		Confidence:     req.Confidence,
		Verified:       string(entities.VerificationCorrect),
	})
}

// writeSpeciesCorrectionV2 routes the correction through the v2 repositories: it
// resolves the (model, label) pair the new species needs, then updates the
// detection's label_id/model_id/confidence and upserts the review inside one
// transaction, so a detection is never left with a new species and stale
// verification state. The transaction is wrapped in datastore.RetryOnLock at
// WHOLE-TRANSACTION granularity: a MySQL deadlock rolls the transaction back, so
// retrying an individual statement inside it (which is all the repositories' own
// internal RetryOnLock can do) would resume against a discarded transaction.
//
// Reference-data resolution (label type, taxonomic class, label) happens OUTSIDE
// the transaction, matching v2only.Datastore.Save, which documents the same
// split: "Label GetOrCreate calls are outside the transaction. If the detection
// save fails, orphaned reference data may persist. This is acceptable as they
// will be reused on subsequent saves."
//
// The label lookup is GetOrCreate, not GetByScientificNameAndModel. v2 labels are
// per (scientific_name, model_id) and are created at save time, so a species the
// chosen model has predicted but never persisted has no label row yet — which is
// precisely the interesting correction. A plain lookup would reject it.
//
// The repositories are constructed here against the transaction handle rather
// than reusing the v2only datastore's own, which are unexported. Their
// constructors take a plain *gorm.DB, and repository/transactor.go documents
// binding them to a tx as the intended pattern.
func (c *Handler) writeSpeciesCorrectionV2(
	ctx context.Context,
	noteID uint,
	scientific string,
	confidence float64,
	model detection.ModelInfo,
) error {
	if c.V2Manager == nil {
		return fmt.Errorf("v2 schema is active but no v2 manager is wired")
	}
	db := c.V2Manager.DB()
	if db == nil {
		return fmt.Errorf("v2 database handle unavailable")
	}
	if model.Name == "" || model.Version == "" {
		return fmt.Errorf("model name/version required for a v2 correction")
	}

	// TablePrefix() is non-empty only on MySQL deployments still inside the
	// v1->v2 migration window, where v2 tables carry a "v2_" prefix so they do
	// not collide with the legacy schema. The repositories take that as a bool.
	useV2Prefix := c.V2Manager.TablePrefix() != ""
	isMySQL := c.V2Manager.IsMySQL()

	modelRepo := repository.NewModelRepository(db, nil, useV2Prefix, isMySQL)
	labelRepo := repository.NewLabelRepository(db, nil, useV2Prefix, isMySQL)
	labelTypeRepo := repository.NewLabelTypeRepository(db, nil, useV2Prefix)
	taxClassRepo := repository.NewTaxonomicClassRepository(db, nil, useV2Prefix)

	// Every enabled model is registered at startup via
	// datastore.Interface.EnsureModelRegistered, so a plain lookup is correct
	// here: a miss means the operator picked a model this database has never
	// seen, which is a 400, not a row to create.
	aiModel, err := modelRepo.GetByNameVersionVariant(ctx, model.Name, model.Version, model.Variant)
	if err != nil {
		return fmt.Errorf("v2 ai_models lookup failed: %w", err)
	}

	speciesType, err := labelTypeRepo.GetOrCreate(ctx, entities.LabelTypeSpecies)
	if err != nil {
		return fmt.Errorf("v2 label_types lookup failed: %w", err)
	}
	// Aves is the default taxonomic class for a corrected label, matching what
	// Save assigns to a bird detection. A label that already exists keeps
	// whatever class and type it was created with — GetOrCreate only applies
	// these to a genuinely new row — so a Perch non-bird sound class that has
	// been seen before is not reclassified by a correction.
	var avesClassID *uint
	if avesClass, classErr := taxClassRepo.GetByName(ctx, taxonomicClassAves); classErr == nil && avesClass != nil {
		avesClassID = &avesClass.ID
	}

	label, err := labelRepo.GetOrCreate(ctx, scientific, aiModel.ID, speciesType.ID, avesClassID)
	if err != nil {
		return fmt.Errorf("v2 labels lookup failed: %w", err)
	}

	return datastore.RetryOnLock(ctx, "reanalyze_correct_species_v2", func() error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			detRepo := repository.NewDetectionRepository(tx, nil, useV2Prefix, isMySQL)

			// Update carries its own atomic NOT EXISTS(detection_locks) guard and
			// returns ErrDetectionNotFound / ErrDetectionLocked.
			if err := detRepo.Update(ctx, noteID, map[string]any{
				"label_id":   label.ID,
				"model_id":   aiModel.ID,
				"confidence": confidence,
			}); err != nil {
				if !errors.Is(err, repository.ErrDetectionLocked) {
					return fmt.Errorf("v2 detection update failed: %w", err)
				}
				// Update reports ErrDetectionLocked for ANY zero-row update on a
				// row that exists — and MySQL reports zero rows for a no-op
				// update too, which is what an operator confirming the species a
				// detection already has produces. Confirm the lock before
				// refusing, or that operator is told to unlock a detection that
				// was never locked.
				locked, lockErr := detRepo.IsLocked(ctx, noteID)
				if lockErr != nil {
					return fmt.Errorf("v2 lock check after zero-row update: %w", lockErr)
				}
				if locked {
					return errDetectionLocked
				}
				// Not locked: the detection already carries this species. Fall
				// through to the review upsert, which is the other half of what
				// was asked for.
			}

			if err := detRepo.SaveReview(ctx, &entities.DetectionReview{
				DetectionID: noteID,
				Verified:    entities.VerificationCorrect,
			}); err != nil {
				return fmt.Errorf("v2 review upsert failed: %w", err)
			}
			return nil
		})
	}, nil)
}

// speciesKnownToLoadedModels reports whether scientificName appears in the union
// of every loaded classifier's label set. Comparison is case-insensitive on the
// scientific half of each raw label ("Ficedula hypoleuca_Pied Flycatcher"), which
// is the form the reanalyze response returns to the client.
//
// The union is rebuilt per call rather than cached. Corrections are a manual,
// human-paced action on a handful of detections, and a cache here would have to
// be invalidated on every model load/unload/reload — a standing correctness
// hazard bought for an endpoint nobody calls in a loop.
func speciesKnownToLoadedModels(bn *classifier.Orchestrator, scientificName string) bool {
	want := strings.ToLower(strings.TrimSpace(scientificName))
	if want == "" {
		return false
	}
	for _, label := range bn.AllLabels() {
		scientific, _ := classifier.SplitSpeciesName(label)
		if scientific == "" {
			scientific = label
		}
		if strings.EqualFold(strings.TrimSpace(scientific), want) {
			return true
		}
	}
	return false
}

// correctionNote renders the one-line audit entry appended to a detection's notes
// when a correction changes something. It returns "" when nothing changed — an
// operator confirming the species a detection already has still records a review,
// but adding a note saying "changed from X to X" would be noise.
//
// The model comparison is skipped when the detection carries no model info, so
// an unknown previous model is never guessed at.
func correctionNote(existing *detection.Result, scientific, common string, model *classifier.ModelInfo, confidence float64) string {
	speciesChanged := !strings.EqualFold(strings.TrimSpace(existing.Species.ScientificName), strings.TrimSpace(scientific))
	modelKnown := existing.Model.Name != ""
	// Compared through the same conversion the write path uses, so the note judges
	// the identity that is actually persisted rather than a subset of it. Variant
	// is part of that identity: swapping the stock model for a custom variant of
	// the same name and version really does change the attribution (and possibly
	// the classifier path) even when the species does not change.
	target := model.ToDetectionModelInfo()
	modelChanged := modelKnown &&
		(existing.Model.Name != target.Name ||
			existing.Model.Version != target.Version ||
			existing.Model.Variant != target.Variant)

	if !speciesChanged && !modelChanged {
		return ""
	}

	var b strings.Builder
	b.WriteString("Reanalysis: ")
	switch {
	case speciesChanged:
		fmt.Fprintf(&b, "species changed from %s to %s",
			formatSpecies(existing.Species.ScientificName, existing.Species.CommonName),
			formatSpecies(scientific, common))
	default:
		fmt.Fprintf(&b, "species confirmed as %s", formatSpecies(scientific, common))
	}
	if modelChanged {
		fmt.Fprintf(&b, "; model changed from %s to %s",
			formatModel(existing.Model.Name, existing.Model.Version, existing.Model.Variant), model.Name)
	} else {
		fmt.Fprintf(&b, "; attributed to %s", model.Name)
	}
	fmt.Fprintf(&b, " at %.1f%% confidence (was %.1f%%).", confidence*100, existing.Confidence*100)
	return b.String()
}

// formatSpecies renders a species as "Scientific (Common)", or just the
// scientific name when no common name is known.
func formatSpecies(scientific, common string) string {
	if common == "" || strings.EqualFold(common, scientific) {
		return scientific
	}
	return scientific + " (" + common + ")"
}

// formatModel renders a persisted model identity, including the variant when the
// detection carries one, so an attribution change between two variants of the
// same model reads as a change rather than as "X 2.4 to X 2.4".
func formatModel(name, version, variant string) string {
	out := strings.TrimSpace(name + " " + version)
	if variant != "" {
		out += " (" + variant + ")"
	}
	return out
}
