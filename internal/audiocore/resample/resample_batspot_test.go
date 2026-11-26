package resample

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResampleTo_Upsample192kTo250k verifies the BatSpot path from a 192 kHz
// capture to its 250 kHz analysis rate, a non-integer 125/96 ratio.
func TestResampleTo_Upsample192kTo250k(t *testing.T) {
	const fromRate = 192000
	const toRate = 250000
	const inputSamples = 19200 // 100 ms at 192 kHz

	r, err := NewResampler(fromRate, toRate)
	require.NoError(t, err)
	require.NotNil(t, r)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	input := makePCM16(t, inputSamples)
	dst := make([]byte, r.EstimateOutputBytes(len(input)))

	n, err := r.ResampleTo(input, dst)
	require.NoError(t, err)

	outputSamples := n / bytesPerSample
	expectedSamples := inputSamples * toRate / fromRate
	assert.InDelta(t, expectedSamples, outputSamples, float64(expectedSamples/10),
		"output sample count should be approximately %d (got %d)", expectedSamples, outputSamples)
}
