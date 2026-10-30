//go:build openvino

package openvino

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBSGBatInputShapes covers the shared input binding with tiny identity
// graphs: existing rank-2 waveform inputs, rank-4 mel inputs, and invalid shapes.
// Set OV_TEST_LIB to run it against a local OpenVINO runtime.
func TestBSGBatInputShapes(t *testing.T) {
	lib := os.Getenv("OV_TEST_LIB")
	if lib == "" {
		t.Skip("set OV_TEST_LIB to run OpenVINO tensor-shape regression tests")
	}
	require.NoError(t, InitOV(lib))
	t.Cleanup(func() { assert.NoError(t, DestroyOV()) })
	for _, tc := range []struct {
		name  string
		shape []int64
		valid bool
	}{
		{name: "dynamic waveform batch", shape: []int64{-1, 8}, valid: true},
		{name: "dynamic mel batch", shape: []int64{-1, 1, 2, 4}, valid: true},
		{name: "static mel", shape: []int64{1, 1, 2, 4}, valid: true},
		{name: "dynamic mel frames", shape: []int64{-1, 1, -1, 4}},
		{name: "rank one", shape: []int64{-1}},
		{name: "allocation overflow", shape: []int64{-1, 1, 2147483648, 2147483648}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := writeBSGBatIdentityIR(t, tc.shape)
			c, err := NewClassifier(model, Options{Device: DeviceCPU, PrecisionHint: PrecisionF32, Threads: 1})
			if !tc.valid {
				require.ErrorContains(t, err, "cannot reshape for batch-1 inference")
				require.Nil(t, c)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, c.Close()) })
			input := []float32{1, 2, 3, 4, 5, 6, 7, 8}
			assert.Equal(t, len(input), c.NumClasses())
			got, err := c.PredictRaw(input)
			require.NoError(t, err)
			assert.Equal(t, input, got)
			_, err = c.PredictRaw(input[:1])
			require.ErrorContains(t, err, "input length")
		})
	}
}

func writeBSGBatIdentityIR(t *testing.T, shape []int64) string {
	t.Helper()
	dims := make([]string, len(shape))
	var ports strings.Builder
	for i, dim := range shape {
		dims[i] = strconv.FormatInt(dim, 10)
		fmt.Fprintf(&ports, "<dim>%d</dim>", dim)
	}
	// An identity graph needs no weights file and preserves every input element,
	// making incorrect flattening or axis binding observable without large models.
	xml := fmt.Sprintf(`<net name="identity" version="11"><layers>
<layer id="0" name="input" type="Parameter" version="opset1">
<data shape="%s" element_type="f32"/>
<output><port id="0" precision="FP32">%s</port></output></layer>
<layer id="1" name="output" type="Result" version="opset1">
<input><port id="0" precision="FP32">%s</port></input></layer>
</layers><edges><edge from-layer="0" from-port="0" to-layer="1" to-port="0"/></edges></net>`,
		strings.Join(dims, ","), ports.String(), ports.String())
	path := filepath.Join(t.TempDir(), "identity.xml")
	require.NoError(t, os.WriteFile(path, []byte(xml), 0o600))
	return path
}
