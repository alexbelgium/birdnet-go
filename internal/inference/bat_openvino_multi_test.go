package inference

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ov "github.com/tphakala/birdnet-go/internal/inference/openvino"
)

func TestBatOpenVINOMultipleOutputValidation(t *testing.T) {
	t.Parallel()
	for _, opts := range []OpenVINOMultiOutputOptions{
		{},
		{OutputIndices: []int{0}},
		{OutputIndices: []int{0}, OutputSizes: []int{0}},
		{OutputIndices: []int{0}, OutputSizes: []int{-1}},
	} {
		c, err := NewOpenVINOMultiOutputClassifier("unused.onnx", &opts)
		require.ErrorContains(t, err, "positive size")
		assert.Nil(t, c)
	}
	if !ov.Supported {
		c, err := NewOpenVINOMultiOutputClassifier("unused.onnx", &OpenVINOMultiOutputOptions{
			OutputIndices: []int{0, 1}, OutputSizes: []int{8, 8},
		})
		require.ErrorIs(t, err, ov.ErrOpenVINOUnavailable)
		assert.Nil(t, c)
	}
}

func TestBatOpenVINOMultipleOutputRoundTrip(t *testing.T) {
	lib := os.Getenv("OV_TEST_LIB")
	if !ov.Supported || lib == "" {
		t.Skip("requires the openvino tag and OV_TEST_LIB")
	}
	require.NoError(t, InitOpenVINO(lib))
	t.Cleanup(func() { assert.NoError(t, DestroyOpenVINO()) })
	// Distinct identity/ReLU outputs expose incorrect selection, ordering, or
	// shared buffers. Dynamic batch also exercises the original rank-2 path.
	const graph = `<net name="two_outputs" version="11"><layers>
<layer id="0" name="input" type="Parameter" version="opset1"><data shape="-1,8" element_type="f32"/><output><port id="0" precision="FP32"><dim>-1</dim><dim>8</dim></port></output></layer>
<layer id="1" name="relu" type="ReLU" version="opset1"><input><port id="0" precision="FP32"><dim>-1</dim><dim>8</dim></port></input><output><port id="1" precision="FP32"><dim>-1</dim><dim>8</dim></port></output></layer>
<layer id="2" name="identity_output" type="Result" version="opset1"><input><port id="0" precision="FP32"><dim>-1</dim><dim>8</dim></port></input></layer>
<layer id="3" name="relu_output" type="Result" version="opset1"><input><port id="0" precision="FP32"><dim>-1</dim><dim>8</dim></port></input></layer>
</layers><edges><edge from-layer="0" from-port="0" to-layer="1" to-port="0"/><edge from-layer="0" from-port="0" to-layer="2" to-port="0"/><edge from-layer="1" from-port="1" to-layer="3" to-port="0"/></edges></net>`
	path := filepath.Join(t.TempDir(), "two_outputs.xml")
	require.NoError(t, os.WriteFile(path, []byte(graph), 0o600))
	opts := OpenVINOMultiOutputOptions{
		Threads: 1, Device: OVDeviceCPU, PrecisionHint: OVPrecisionF32,
		OutputIndices: []int{1, 0}, OutputSizes: []int{8, 8},
	}
	for _, indices := range [][]int{{-1, 0}, {0, 2}} {
		invalid := opts
		invalid.OutputIndices = indices
		c, err := NewOpenVINOMultiOutputClassifier(path, &invalid)
		require.Error(t, err)
		assert.Nil(t, c)
	}
	invalid := opts
	invalid.OutputSizes = []int{8, 7}
	c, err := NewOpenVINOMultiOutputClassifier(path, &invalid)
	require.ErrorContains(t, err, "output sizes")
	assert.Nil(t, c)

	c, err = NewOpenVINOMultiOutputClassifier(path, &opts)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	input := []float32{-1, 2, -3, 4, -5, 6, -7, 8}
	want := [][]float32{{0, 2, 0, 4, 0, 6, 0, 8}, input}
	outputs, err := c.PredictOutputs(input)
	require.NoError(t, err)
	assert.Equal(t, want, outputs)
	_, err = c.PredictOutputs(make([]float32, len(input)))
	require.NoError(t, err)
	assert.Equal(t, want, outputs, "another inference must not overwrite returned outputs")
	_, err = c.PredictOutputs(input[:1])
	require.ErrorContains(t, err, "input length")
	c.Close()
	c.Close()
	_, err = c.PredictOutputs(input)
	require.ErrorIs(t, err, ov.ErrOpenVINOUnavailable)

	// The existing single-output API must keep selecting port zero by default.
	single, err := NewOpenVINOClassifier(path, OpenVINOClassifierOptions{
		Labels: make([]string, len(input)), Device: OVDeviceCPU, PrecisionHint: OVPrecisionF32, Threads: 1,
	})
	require.NoError(t, err)
	t.Cleanup(single.Close)
	got, err := single.Predict(input)
	require.NoError(t, err)
	assert.Equal(t, input, got)
}
