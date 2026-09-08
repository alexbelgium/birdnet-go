// correct.go — POST /api/v2/detections/:id/correct-species.
//
// Operator-driven correction of a detection's species. After running the
// reanalyze endpoint and seeing what the other loaded classifiers think, the user
// picks "this is the right one"; this endpoint rewrites the detection's species
// and confidence in place and marks it verified='correct' in the same step.
//
// Persistence design — the reason this file exists rather than a datastore method:
// the correction is written here, from the API domain package, by composing
// primitives datastore already exports. datastore.Interface and
// datastore.DetectionRepository are NOT extended, so no mockery mock is
// regenerated and no upstream datastore file is touched. Two paths:
//
//   - v2 installs (DS.SchemaVersion() == datastore.SchemaVersionV2): build the
//     three v2 repositories against a transactional *gorm.DB from V2Manager and
//     run model lookup -> label lookup -> detection Update -> review upsert inside
//     it. The repositories' own atomic lock guard and OnConflict semantics are
//     reused as-is; the transaction is just an envelope around them.
//   - legacy installs: DS.Transaction with GORM model types (datastore.Note,
//     NoteLock, NoteReview) so table and column names are derived from the structs
//     rather than hardcoded — a rename upstream becomes a compile error here, not
//     a runtime SQL error.
package reanalyze

import (
	"context"
	stderrors "errors"
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
	"github.com/tphakala/birdnet-go/internal/logger"
)

// errDetectionLocked is this package's sentinel for "the detection was locked when
// the write ran". Both persistence paths normalise onto it so the handler maps a
// single condition to 409 without caring which schema answered.
var errDetectionLocked = stderrors.New("detection is locked")

const (
	// verificationCorrect is the legacy NoteReview.Verified value meaning "the
	// operator confirmed this species". It mirrors entities.VerificationCorrect
	// on the v2 side; the two schemas store the same string.
	verificationCorrect = "correct"

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

	// Read-side context for the structured log diff below, and the 404/409 fast
	// paths. The authoritative lock guard runs inside the write itself, atomically.
	existing, err := c.DS.Get(idStr)
	if err != nil {
		// Distinguish "no such detection" from "the fetch itself failed". A
		// transient database error reported as 404 tells the operator their
		// detection is gone, which is both wrong and alarming.
		if isDetectionNotFoundErr(err) {
			return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
		}
		return c.HandleError(ctx, err, "Failed to fetch detection", http.StatusInternalServerError)
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
	// Reject ultrasonic-only models (the bat classifier). They expect raw audio at
	// a rate saved detection clips do not carry, so a correction attributed to one
	// is meaningless — the same RawSampleRate filter reanalysis applies.
	if chosen.Spec.RawSampleRate != 0 {
		return c.HandleError(ctx, fmt.Errorf("model %q is ultrasonic-only", req.ModelID),
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

	if err := c.writeSpeciesCorrection(ctx.Request().Context(), bn, uint(noteID),
		req.ScientificName, commonName, req.Confidence, chosen.ToDetectionModelInfo(),
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
		case stderrors.Is(err, errDetectionLocked):
			return c.HandleError(ctx, err,
				"Failed to correct species: detection is locked", http.StatusConflict)
		case stderrors.Is(err, repository.ErrDetectionNotFound):
			return c.HandleError(ctx, err, "Detection not found", http.StatusNotFound)
		case stderrors.Is(err, repository.ErrModelNotFound), stderrors.Is(err, repository.ErrLabelNotFound):
			return c.HandleError(ctx, err,
				"The chosen model does not know this species; cannot apply correction",
				http.StatusBadRequest)
		default:
			return c.HandleError(ctx, err,
				"Internal error while correcting species", http.StatusInternalServerError)
		}
	}

	// Flush the detection cache so list and dashboard queries reflect the new
	// label immediately. Without this the 5-minute cache keeps serving the
	// pre-correction species: the operator sees the correction stick on the detail
	// page while the parent list still shows the old name. Same thing the delete,
	// review and lock handlers do.
	c.DetectionCache.Flush()

	c.LogAPIRequest(ctx, logger.LogLevelInfo, "Species correction applied",
		logger.String("detection_id", idStr),
		logger.String("model_id", resolvedID),
		logger.String("from_species", existing.ScientificName),
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

// writeSpeciesCorrection persists the correction, dispatching on the schema the
// running datastore actually owns. Both branches are all-or-nothing: the species
// change and the 'correct' review either both commit or neither does, so a
// detection is never left with a new species and stale verification state. Both
// are wrapped in datastore.RetryOnLock at WHOLE-TRANSACTION granularity — a
// MySQL deadlock rolls the transaction back, so retrying an individual statement
// inside it (which is all the repositories' own internal RetryOnLock can do)
// would resume against a transaction the server has already discarded.
func (c *Handler) writeSpeciesCorrection(
	ctx context.Context,
	bn *classifier.Orchestrator,
	noteID uint,
	scientific, common string,
	confidence float64,
	model detection.ModelInfo,
) error {
	if c.DS.SchemaVersion() == datastore.SchemaVersionV2 {
		return c.writeSpeciesCorrectionV2(ctx, noteID, scientific, confidence, model)
	}
	return c.writeSpeciesCorrectionLegacy(ctx, bn, noteID, scientific, common, confidence)
}

// writeSpeciesCorrectionLegacy rewrites the v1 notes row and upserts note_reviews
// in one transaction.
//
// The species UPDATE carries its own NOT EXISTS(note_locks) guard so the lock
// check and the write are atomic: a lock taken between the handler's read and
// this write wins the race and the correction is refused, rather than silently
// overwriting a locked detection. Table and column names come from the GORM
// models, not string literals, so an upstream rename fails to compile here
// instead of failing at runtime.
func (c *Handler) writeSpeciesCorrectionLegacy(
	ctx context.Context,
	bn *classifier.Orchestrator,
	noteID uint,
	scientific, common string,
	confidence float64,
) error {
	// species_code must move with the species. A stale eBird code outlives the
	// correction into the API response and every consumer that keys on it, so
	// resolve the new one; an empty code (species outside the primary model's
	// taxonomy) is strictly better than the previous species' code.
	speciesCode, _ := bn.GetSpeciesCode(scientific)

	return datastore.RetryOnLock(ctx, "reanalyze_correct_species_legacy", func() error {
		return c.DS.Transaction(func(tx *gorm.DB) error {
			return applyLegacyCorrection(tx, noteID, scientific, common, speciesCode, confidence)
		})
	}, nil)
}

// applyLegacyCorrection is the body of the legacy correction transaction, split
// out as a free function over *gorm.DB so it can be exercised against a real
// SQLite schema in tests rather than only through a running datastore.
func applyLegacyCorrection(tx *gorm.DB, noteID uint, scientific, common, speciesCode string, confidence float64) error {
	locked := tx.Model(&datastore.NoteLock{}).Select("1").Where("note_id = ?", noteID)
	result := tx.Model(&datastore.Note{}).
		Where("id = ?", noteID).
		Where("NOT EXISTS (?)", locked).
		Updates(map[string]any{
			"scientific_name": scientific,
			"common_name":     common,
			"species_code":    speciesCode,
			"confidence":      confidence,
			// The raw label belonged to the model's original call. Clearing it
			// means "no alias was applied to this name", which is true of an
			// operator-asserted scientific name.
			"raw_scientific_name": "",
		})
	if result.Error != nil {
		return fmt.Errorf("update note species: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		// Zero rows has THREE causes, and they must not be collapsed. The note is
		// gone (404); the note is locked, so the NOT EXISTS guard excluded it
		// (409); or MySQL matched the row and changed nothing because every value
		// was already what we are writing — which happens when an operator
		// confirms the species a detection already has. Reporting that last case
		// as "locked" tells them to unlock a detection that was never locked.
		var noteCount int64
		if err := tx.Model(&datastore.Note{}).Where("id = ?", noteID).Count(&noteCount).Error; err != nil {
			return fmt.Errorf("check note existence: %w", err)
		}
		if noteCount == 0 {
			return repository.ErrDetectionNotFound
		}
		var lockCount int64
		if err := tx.Model(&datastore.NoteLock{}).Where("note_id = ?", noteID).Count(&lockCount).Error; err != nil {
			return fmt.Errorf("check note lock: %w", err)
		}
		if lockCount > 0 {
			return errDetectionLocked
		}
		// No-op update on an unlocked row: the species is already what was asked
		// for. Fall through and still record the review — marking it verified is
		// the other half of what the operator requested.
	}

	// Where+Assign+FirstOrCreate is dialect-safe (SELECT, then INSERT or UPDATE),
	// so this upsert works on SQLite and MySQL without a hand-written ON CONFLICT
	// clause. Assign overwrites an existing row, so a detection previously marked
	// false_positive becomes correct rather than keeping the stale verdict.
	review := &datastore.NoteReview{NoteID: noteID, Verified: verificationCorrect}
	if err := tx.Where("note_id = ?", noteID).
		Assign(datastore.NoteReview{Verified: verificationCorrect, UpdatedAt: time.Now()}).
		FirstOrCreate(review).Error; err != nil {
		return fmt.Errorf("upsert note review: %w", err)
	}
	return nil
}

// writeSpeciesCorrectionV2 routes the correction through the v2 repositories: it
// resolves the (model, label) pair the new species needs, then updates the
// detection's label_id/model_id/confidence and upserts the review inside one
// transaction.
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
				if !stderrors.Is(err, repository.ErrDetectionLocked) {
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
