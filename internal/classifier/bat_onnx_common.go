package classifier

import (
	"slices"
	"sync"

	"github.com/tphakala/birdnet-go/internal/errors"
	inferenceonnx "github.com/tphakala/birdnet-go/internal/inference/onnx"
	ort "github.com/yalue/onnxruntime_go"
)

const (
	ortAllowIntraOpSpinning = "session.intra_op.allow_spinning"
	ortAllowInterOpSpinning = "session.inter_op.allow_spinning"
	ortSpinningDisabled     = "0"
)

type batORTBase struct {
	mu        sync.Mutex
	session   *ort.DynamicAdvancedSession
	labels    []string
	info      ModelInfo
	modelPath string
	values    []ort.Value
}

func newBatORTSession(modelID, modelPath string, inputs, outputs []string, threads int) (session *ort.DynamicAdvancedSession, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = errors.New(resultErr).Component("classifier").Category(errors.CategoryModelInit).Context("operation", "create_ort_session").Context("model", modelID).Build()
		}
	}()
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer func() { _ = opts.Destroy() }()
	if threads > 0 {
		if err := opts.SetIntraOpNumThreads(threads); err != nil {
			return nil, err
		}
	}
	if err := opts.SetInterOpNumThreads(1); err != nil {
		return nil, err
	}
	if err := opts.AddSessionConfigEntry(ortAllowIntraOpSpinning, ortSpinningDisabled); err != nil {
		return nil, err
	}
	if err := opts.AddSessionConfigEntry(ortAllowInterOpSpinning, ortSpinningDisabled); err != nil {
		return nil, err
	}
	if err := inferenceonnx.DisableKleidiAIIfNeeded(opts); err != nil {
		return nil, err
	}
	return ort.NewDynamicAdvancedSession(modelPath, inputs, outputs, opts)
}

// Spec returns the analysis buffer specification.
func (b *batORTBase) Spec() ModelSpec { return b.info.Spec }

// ModelID returns the registry identifier.
func (b *batORTBase) ModelID() string { return b.info.ID }

// ModelName returns the display name.
func (b *batORTBase) ModelName() string { return b.info.Name }

// ModelVersion returns the detection version.
func (b *batORTBase) ModelVersion() string { return b.info.DetectionVersion }

// NumSpecies returns the species count.
func (b *batORTBase) NumSpecies() int { return len(b.labels) }

// Labels returns an independent copy of the species labels.
func (b *batORTBase) Labels() []string { return slices.Clone(b.labels) }

// RuntimeInfo reports the ORT execution backend.
func (b *batORTBase) RuntimeInfo() (device, backend, precision string) {
	return deviceCPU, BackendONNX, string(detectQuantization(b.modelPath))
}

// ResolvedModelPath returns the loaded ONNX model path.
func (b *batORTBase) ResolvedModelPath() string { return b.modelPath }

// Close releases the session and tensors under the prediction lock.
func (b *batORTBase) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var result error
	for _, value := range b.values {
		result = errors.Join(result, value.Destroy())
	}
	b.values = nil
	if b.session != nil {
		result = errors.Join(result, b.session.Destroy())
		b.session = nil
	}
	return result
}
