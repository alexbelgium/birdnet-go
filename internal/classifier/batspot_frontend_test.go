package classifier

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const batSpotFixtureDir = "testdata/batspot"

var batSpotFixtures = []string{"bat_peak", "sweep40k", "silence"}

func readBatSpotFixture(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(batSpotFixtureDir, filename))
	require.NoError(t, err)
	return data
}

func readBatSpotFloat32(t *testing.T, filename string, count int) []float32 {
	t.Helper()
	data := readBatSpotFixture(t, filename)
	const float32Bytes = 4
	require.Len(t, data, count*float32Bytes)
	result := make([]float32, count)
	for i := range result {
		bits := binary.LittleEndian.Uint32(data[i*float32Bytes:])
		result[i] = math.Float32frombits(bits)
	}
	return result
}

func TestBatSpotFrontendParity(t *testing.T) {
	t.Parallel()
	for _, name := range batSpotFixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newBatSpotFrontend()
			require.NoError(t, err)
			pcm := readBatSpotFloat32(t, name+".pcm.f32", batSpotCallSamples)
			expected := readBatSpotFloat32(t, name+".input.f32", batSpotInputSize)
			got := make([]float32, batSpotInputSize)
			f.window(got, pcm)
			const recordedTolerance = 1e-5
			tolerance := float64(recordedTolerance)
			if name == "sweep40k" {
				// The float32 FFT implementation affects bins just above the dB
				// floor. The reference NumPy FFT also differs from Torch here.
				tolerance = 1e-3
			}
			require.InDeltaSlice(t, expected, got, tolerance)
		})
	}
}
