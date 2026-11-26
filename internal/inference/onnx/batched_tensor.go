package onnx

import (
	"fmt"
	"runtime"

	ort "github.com/yalue/onnxruntime_go"
)

// BatchedTensorModel runs a single float32 input and output whose leading
// dimensions are dynamic batch axes. Each call supplies its own batch size.
type BatchedTensorModel struct {
	session    guardedSession
	inputShape ort.Shape
	inputSize  int
	outputSize int
}

// NewBatchedTensorModel binds the named input and output. inputDims contains
// the fixed dimensions of one input row. The graph may declare those axes as
// static or dynamic. The output must be shaped [batch, outputSize].
func NewBatchedTensorModel(path, inputName, outputName string, inputDims []int64, outputSize, threads int) (*BatchedTensorModel, error) {
	inputs, outputs, err := ort.GetInputOutputInfo(path)
	if err != nil {
		return nil, fmt.Errorf("read batched ONNX model metadata: %w", err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return nil, fmt.Errorf("batched ONNX model needs one input and one output, got %d and %d", len(inputs), len(outputs))
	}
	if err := checkBatchedTensor(&inputs[0], inputName, inputDims, true); err != nil {
		return nil, err
	}
	if err := checkBatchedTensor(&outputs[0], outputName, []int64{int64(outputSize)}, false); err != nil {
		return nil, err
	}
	inputShape, inputSize, err := resolveTensorShape(append([]int64{pinnedBatchSize}, inputDims...))
	if err != nil {
		return nil, err
	}

	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	var threadErr error
	session, err := createSession(path, []string{inputName}, []string{outputName}, func(options *ort.SessionOptions) {
		threadErr = options.SetIntraOpNumThreads(threads)
	})
	if err != nil {
		return nil, err
	}
	if threadErr != nil {
		_ = session.Destroy()
		return nil, fmt.Errorf("configure batched ONNX inference threads: %w", threadErr)
	}
	return &BatchedTensorModel{
		session:    guardedSession{s: session},
		inputShape: inputShape,
		inputSize:  inputSize,
		outputSize: outputSize,
	}, nil
}

func checkBatchedTensor(info *ort.InputOutputInfo, name string, rowDims []int64, allowDynamicRow bool) error {
	if err := requireFloatTensor(info); err != nil {
		return fmt.Errorf("ONNX tensor %q: %w", info.Name, err)
	}
	if info.Name != name || len(info.Dimensions) != len(rowDims)+1 || info.Dimensions[0] != dynamicDim {
		return fmt.Errorf("ONNX tensor %q shape %v, expected %q with a dynamic batch and row shape %v", info.Name, info.Dimensions, name, rowDims)
	}
	for i, dim := range info.Dimensions[1:] {
		if dim != rowDims[i] && (!allowDynamicRow || dim != dynamicDim) {
			return fmt.Errorf("ONNX tensor %q shape %v is incompatible with row shape %v", name, info.Dimensions, rowDims)
		}
	}
	return nil
}

// Run returns batch-major logits in the provided output buffer.
func (m *BatchedTensorModel) Run(input, output []float32) error {
	batch := len(input) / m.inputSize
	if batch == 0 || len(input)%m.inputSize != 0 || len(output) != batch*m.outputSize {
		return fmt.Errorf("invalid batched ONNX tensor lengths: input %d, output %d", len(input), len(output))
	}
	shape := m.inputShape.Clone()
	shape[0] = int64(batch)
	in, err := ort.NewTensor(shape, input)
	if err != nil {
		return fmt.Errorf("create batched ONNX input tensor: %w", err)
	}
	defer func() { _ = in.Destroy() }()

	out, err := ort.NewTensor(ort.NewShape(int64(batch), int64(m.outputSize)), output)
	if err != nil {
		return fmt.Errorf("create batched ONNX output tensor: %w", err)
	}
	defer func() { _ = out.Destroy() }()

	if err := m.session.run([]ort.Value{in}, []ort.Value{out}); err != nil {
		return fmt.Errorf("run batched ONNX inference: %w", err)
	}
	return nil
}

// Close releases the session after any in-flight runs finish.
func (m *BatchedTensorModel) Close() error { return m.session.close() }
