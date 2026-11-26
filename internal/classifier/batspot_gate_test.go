package classifier

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatSpotGate_MedianMargin(t *testing.T) {
	t.Parallel()

	f, err := newBatSpotFrontend()
	require.NoError(t, err)
	pcm := make([]float32, batSpotClipSamples)

	// A stationary 40 kHz background has the same energy in every window.
	const (
		toneHz            = 40_000
		tonePeriodSamples = 25 // exactly four cycles at 250 kHz
		quietAmplitude    = 0.02
		belowMarginGain   = 1.5
		aboveMarginGain   = 2.2
	)
	for i := range pcm {
		pcm[i] = quietAmplitude * float32(math.Sin(2*math.Pi*toneHz*float64(i%tonePeriodSamples)/batSpotSampleRate))
	}
	assert.Empty(t, f.selectCalls(pcm))

	// Each amplified call occupies one grid-aligned window. Its neighbors
	// overlap half its audio. The faint call stays below the median + 6 dB.
	faintStart := 10 * batSpotCallSamples
	loudStart := 30 * batSpotCallSamples
	for i := range batSpotCallSamples {
		pcm[faintStart+i] *= belowMarginGain
		pcm[loudStart+i] *= aboveMarginGain
	}
	assert.Equal(t, []int{loudStart}, f.selectCalls(pcm))
}

func TestBatSpotGate_PrefersStrongerNonOverlappingCalls(t *testing.T) {
	t.Parallel()

	f, err := newBatSpotFrontend()
	require.NoError(t, err)
	pcm := make([]float32, batSpotClipSamples)

	const pulseSpacing = 4 * batSpotCallSamples
	callStarts := []int{pulseSpacing, 2 * pulseSpacing, 3 * pulseSpacing, 4 * pulseSpacing, 5 * pulseSpacing}
	for i, start := range callStarts {
		pcm[start+batSpotHopSamples/2] = float32(i+1) * 0.05
	}
	selected := f.selectCalls(pcm)
	require.Len(t, selected, batSpotMaxCalls)
	for i, start := range selected {
		// The selected window covers one of the four loudest pulses.
		pulse := callStarts[len(callStarts)-1-i] + batSpotHopSamples/2
		assert.LessOrEqual(t, start, pulse)
		assert.Greater(t, start+batSpotCallSamples, pulse)
	}
}

func TestBatSpotGate_AbsoluteFloor(t *testing.T) {
	t.Parallel()

	f, err := newBatSpotFrontend()
	require.NoError(t, err)

	// A full-scale 40 kHz sine at the floor level lands within about 1 dB of
	// batSpotGateMinPower (pre-emphasis gain and window leakage).
	const (
		toneHz            = 40_000
		tonePeriodSamples = 25
	)
	tone := make([]float32, batSpotCallSamples)
	amplitude := math.Pow(10, batSpotGateFloorDBFS/20.0)
	for i := range tone {
		tone[i] = float32(amplitude * math.Sin(2*math.Pi*toneHz*float64(i%tonePeriodSamples)/batSpotSampleRate))
	}
	ratioDB := 10 * math.Log10(f.bandPower(tone)/batSpotGateMinPower)
	assert.InDelta(t, 0, ratioDB, 1.0)

	// Most of the clip is digital zero, so the median band power is zero.
	// One-LSB 16-bit dither in the remaining 30% must not pass the gate.
	const lsb = 1.0 / 32768
	pcm := make([]float32, batSpotClipSamples)
	rng := rand.New(rand.NewPCG(1, 2))
	ditherEnd := len(pcm) * 3 / 10
	for i := range ditherEnd {
		pcm[i] = float32((2*rng.Float64() - 1) * lsb)
	}
	assert.Empty(t, f.selectCalls(pcm))

	// A real call in the same clip is still selected.
	const callAmplitude = 0.05
	callStart := 10 * batSpotCallSamples
	for i := range batSpotCallSamples {
		pcm[callStart+i] += float32(callAmplitude * math.Sin(2*math.Pi*toneHz*float64(i%tonePeriodSamples)/batSpotSampleRate))
	}
	assert.Equal(t, []int{callStart}, f.selectCalls(pcm))
}
