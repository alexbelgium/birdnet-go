package classifier

import (
	"cmp"
	"math"
	"slices"
)

const (
	batSpotHopSamples = 2500 // 10 ms at 250 kHz
	batSpotMaxCalls   = 4
	batSpotMarginDB   = 6

	// batSpotGateFloorDBFS is the absolute in-band level a window must reach.
	// Without it a clip that is mostly digital zero has a zero median, so any
	// nonzero dither would clear the relative margin.
	batSpotGateFloorDBFS = -85
)

var (
	batSpotPowerRatio = math.Pow(10, float64(batSpotMarginDB)/batSpotPowerDBMultiplier)

	// batSpotGateMinPower is batSpotGateFloorDBFS in bandPower units, for float
	// PCM in [-1, 1] where a full-scale sine (mean square 1/2) is 0 dBFS. By
	// Parseval, a frame whose pre-emphasized signal has mean square m inside
	// the band puts N*m*sum(w^2) = N*m*batSpotHannEnergy unscaled |X|^2 into
	// the full spectrum, half of it into the positive bins bandPower sums.
	// Over batSpotFrames frames that is m * N * batSpotHannEnergy / 2 * frames.
	// The level is measured after pre-emphasis (about -0.4 dB at 40 kHz).
	batSpotGateMinPower = 0.5 * math.Pow(10, batSpotGateFloorDBFS/10.0) *
		batSpotFFTSize * batSpotHannEnergy / 2 * batSpotFrames
)

type batSpotCandidate struct {
	start int
	power float64
}

// selectCalls ranks full 20 ms windows above the clip's median band power by
// at least 6 dB and above the absolute batSpotGateMinPower floor. Greedy non-overlapping selection keeps adjacent windows
// covering the same call from consuming two slots in the four-row ONNX batch.
func (f *batSpotFrontend) selectCalls(pcm []float32) []int {
	if len(pcm) < batSpotCallSamples {
		return nil
	}
	count := 1 + (len(pcm)-batSpotCallSamples)/batSpotHopSamples
	candidates := make([]batSpotCandidate, count)
	for i := range candidates {
		start := i * batSpotHopSamples
		candidates[i] = batSpotCandidate{start: start, power: f.bandPower(pcm[start:][:batSpotCallSamples])}
	}
	slices.SortFunc(candidates, func(a, b batSpotCandidate) int {
		if order := cmp.Compare(b.power, a.power); order != 0 {
			return order
		}
		return cmp.Compare(a.start, b.start)
	})
	median := (candidates[(count-1)/2].power + candidates[count/2].power) / 2
	minPower := max(median*batSpotPowerRatio, batSpotGateMinPower)

	selected := make([]int, 0, batSpotMaxCalls)
	for _, candidate := range candidates {
		if candidate.power < minPower {
			break
		}
		if slices.ContainsFunc(selected, func(start int) bool {
			return max(start, candidate.start)-min(start, candidate.start) < batSpotCallSamples
		}) {
			continue
		}
		selected = append(selected, candidate.start)
		if len(selected) == batSpotMaxCalls {
			break
		}
	}
	return selected
}

// bandPower sums the pre-emphasized STFT power before the dB floor and
// min-max normalization, over the FFT bins the model input uses. Every
// candidate has the same window energy and frame count, so those factors
// cancel when comparing its power to the median.
func (f *batSpotFrontend) bandPower(pcm []float32) float64 {
	if !slices.ContainsFunc(pcm, func(sample float32) bool { return sample != 0 }) {
		return 0
	}
	f.computePower(pcm)
	var sum float64
	for frame := range batSpotFrames {
		start := frame * batSpotSTFTBins
		for _, power := range f.power[start+batSpotFirstBin : start+batSpotEndBin] {
			sum += float64(power)
		}
	}
	return sum
}
