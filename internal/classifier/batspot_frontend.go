package classifier

import (
	"math"

	"github.com/tphakala/go-audio-dsp/stft"

	"github.com/tphakala/birdnet-go/internal/conf"
)

const (
	batSpotSampleRate  = conf.BatSpotSampleRate
	batSpotCallSamples = 5000 // 20 ms at 250 kHz

	batSpotFFTSize           = 256
	batSpotSTFTHop           = 128
	batSpotSTFTBins          = batSpotFFTSize/2 + 1
	batSpotFrames            = 1 + (batSpotCallSamples-batSpotFFTSize)/batSpotSTFTHop
	batSpotFrequencyBins     = 256
	batSpotInputSize         = batSpotFrames * batSpotFrequencyBins
	batSpotMinHz             = 10_000
	batSpotMaxHz             = 125_000
	batSpotFirstBin          = batSpotFFTSize * batSpotMinHz / batSpotSampleRate
	batSpotEndBin            = (batSpotFFTSize*batSpotMaxHz + batSpotSampleRate - 1) / batSpotSampleRate
	batSpotPreEmphasis       = 0.98
	batSpotHannEnergy        = 3.0 * batSpotFFTSize / 8
	batSpotPowerFloor        = 1e-10 // -100 dB
	batSpotPowerDBMultiplier = 10
)

// batSpotFrontend implements the ANIMAL-SPOT linear spectrogram and min-max
// normalization for full BatSpot m09 call windows. The caller owns its scratch
// space exclusively and provides exactly one full window and output tensor row.
type batSpotFrontend struct {
	stft       *stft.Plan
	emphasized [batSpotCallSamples]float32
	power      [batSpotFrames * batSpotSTFTBins]float32
}

func newBatSpotFrontend() (*batSpotFrontend, error) {
	plan, err := stft.New(stft.Config{FrameSize: batSpotFFTSize, HopSize: batSpotSTFTHop, Window: stft.Hann})
	if err != nil {
		return nil, err
	}
	return &batSpotFrontend{stft: plan}, nil
}

func (f *batSpotFrontend) computePower(pcm []float32) {
	f.emphasized[0] = pcm[0]
	for i := 1; i < len(f.emphasized); i++ {
		f.emphasized[i] = pcm[i] - batSpotPreEmphasis*pcm[i-1]
	}
	f.stft.PowerInto(f.power[:], f.emphasized[:], stft.NoPad)
}

func (f *batSpotFrontend) window(dst, pcm []float32) {
	f.computePower(pcm)

	low, high := float32(math.Inf(1)), float32(math.Inf(-1))
	for frame := range batSpotFrames {
		power := f.power[frame*batSpotSTFTBins:]
		// Compute dB once per cropped FFT bin. Nearest-neighbor expansion
		// uses every cropped bin, so the same bins define the extrema.
		var db [batSpotEndBin - batSpotFirstBin]float32
		for bin := range db {
			p := float64(power[batSpotFirstBin+bin]) / batSpotHannEnergy
			v := float32(batSpotPowerDBMultiplier * math.Log10(max(p, batSpotPowerFloor)))
			db[bin] = v
			low, high = min(low, v), max(high, v)
		}
		row := dst[frame*batSpotFrequencyBins:][:batSpotFrequencyBins]
		for j := range row {
			row[j] = db[j*len(db)/batSpotFrequencyBins]
		}
	}

	if high == low {
		clear(dst)
		return
	}
	for i := range dst {
		dst[i] = (dst[i] - low) / (high - low)
	}
}
