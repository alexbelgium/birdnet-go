package inference

import (
	"slices"

	"github.com/tphakala/birdnet-go/internal/errors"
	ov "github.com/tphakala/birdnet-go/internal/inference/openvino"
)

// MultiOutputClassifier reads selected tensors from a single model inference.
// Callers must serialize prediction and cleanup.
type MultiOutputClassifier interface {
	// PredictOutputs runs inference and returns independent Go-owned tensors.
	PredictOutputs(samples []float32) ([][]float32, error)
	// Close releases model resources. It is idempotent.
	Close()
}

// OpenVINOMultiOutputOptions configures tensor outputs without species labels.
type OpenVINOMultiOutputOptions struct {
	Threads       int    // CPU thread budget; zero lets OpenVINO auto-tune.
	PrecisionHint string // Empty uses the backend's default precision hint.
	Device        string // CPU (default) or GPU.
	OutputIndices []int  // Selected output ports, in returned tensor order.
	OutputSizes   []int  // Required element count of each selected output.
}

type openvinoMultiOutputClassifier struct {
	c ov.MultiOutputClassifier
}

// NewOpenVINOMultiOutputClassifier compiles a model and validates every selected
// output size. InitOpenVINO must succeed first. Existing single-output callers
// retain their original output selection and label validation.
func NewOpenVINOMultiOutputClassifier(modelPath string, opts *OpenVINOMultiOutputOptions) (MultiOutputClassifier, error) {
	if opts == nil || len(opts.OutputIndices) == 0 || len(opts.OutputIndices) != len(opts.OutputSizes) || slices.ContainsFunc(opts.OutputSizes, func(n int) bool { return n <= 0 }) {
		return nil, errors.Newf("OpenVINO requires an index and positive size for every selected output").
			Component("inference").Category(errors.CategoryValidation).Context("operation", "initialize_multi_output").Build()
	}
	c, err := ov.NewClassifier(modelPath, ov.Options{
		Threads: max(opts.Threads, 0), PrecisionHint: opts.PrecisionHint,
		Device: opts.Device, OutputIndices: opts.OutputIndices,
	})
	if err != nil {
		return nil, err
	}
	multi, ok := c.(ov.MultiOutputClassifier)
	if !ok {
		_ = c.Close()
		return nil, errors.Newf("OpenVINO backend does not support multiple outputs").
			Component("inference").Category(errors.CategoryModelInit).Context("operation", "initialize_multi_output").Build()
	}
	if sizes := multi.OutputSizes(); !slices.Equal(sizes, opts.OutputSizes) {
		_ = c.Close()
		return nil, errors.Newf("OpenVINO output sizes %v do not match expected %v", sizes, opts.OutputSizes).
			Component("inference").Category(errors.CategoryValidation).Context("operation", "initialize_multi_output").Build()
	}
	ovActiveClassifiers.Add(1)
	return &openvinoMultiOutputClassifier{c: multi}, nil
}

// PredictOutputs returns selected output tensors from a single inference.
func (c *openvinoMultiOutputClassifier) PredictOutputs(samples []float32) ([][]float32, error) {
	if c.c == nil {
		return nil, ov.ErrOpenVINOUnavailable
	}
	return c.c.PredictOutputs(samples)
}

// Close releases the native model and updates the active-backend count once.
func (c *openvinoMultiOutputClassifier) Close() {
	if c.c != nil {
		_ = c.c.Close()
		c.c = nil
		ovActiveClassifiers.Add(-1)
	}
}
