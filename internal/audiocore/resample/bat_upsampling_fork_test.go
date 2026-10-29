package resample

import (
	"encoding/binary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/go-audio-dsp/stft"
	"math"
	"math/rand/v2"
	"testing"
)

// Guard the source-Nyquist boundary of common ultrasonic capture routes. Leave
// a transition band for the finite filter; measure float output to exclude PCM16
// quantization noise and discard startup frames to measure steady-state imaging.
func TestUltrasonicUpsamplingRejectsImages(t *testing.T) {
	t.Parallel()
	const sourceRate = 192000
	const frameSize = 4096
	const stopbandHz = 100000
	const maximumImagePowerDB = -55.0
	rng := rand.New(rand.NewPCG(1, 2))
	pcm := make([]byte, sourceRate*2)
	for i := range sourceRate {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(rng.NormFloat64()*1000)))
	}
	for _, rate := range []int{256000, 384000} {
		r, err := NewResampler(sourceRate, rate)
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, r.Close()) })
		samples, err := r.ResampleFloat32Into(pcm)
		require.NoError(t, err)
		plan, err := stft.New(stft.Config{FrameSize: frameSize, HopSize: frameSize, Window: stft.Hann})
		require.NoError(t, err)
		spectra := make([][]complex64, plan.NumFrames(len(samples), stft.PadReflect))
		for i := range spectra {
			spectra[i] = make([]complex64, plan.NumBins())
		}
		plan.Spectrum(spectra, samples, stft.PadReflect)
		var total, images float64
		for _, row := range spectra[2 : len(spectra)-2] {
			for bin, v := range row {
				power := float64(real(v))*float64(real(v)) + float64(imag(v))*float64(imag(v))
				total += power
				if bin*rate/frameSize >= stopbandHz {
					images += power
				}
			}
		}
		imageDB := 10 * math.Log10(images/total)
		t.Logf("192000 -> %d Hz: power above %d Hz = %.2f dB relative", rate, stopbandHz, imageDB)
		assert.Less(t, imageDB, maximumImagePowerDB)
	}
}
