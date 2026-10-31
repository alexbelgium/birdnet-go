package classifier

import (
	"math"
	"slices"

	"github.com/tphakala/go-audio-dsp/mel"
	"github.com/tphakala/go-audio-dsp/stft"
)

const (
	bsgBatRawSampleRate = 384000
	bsgBatFFTSize       = 1024
	bsgBatHopSize       = 768
	bsgBatFrames        = 512
	bsgBatMelBands      = 128
	bsgBatMinFrequency  = 9000.0
	bsgBatMaxFrequency  = 150000.0
	bsgBatLogOffset     = 1e-6
	bsgBatClipMaximum   = 6.0
	bsgBatInputSamples  = (bsgBatFrames-1)*bsgBatHopSize + bsgBatFFTSize
)

type bsgBatDSP struct {
	extractor      *mel.Extractor
	matrix         mel.Matrix
	column, output []float32
}

func newBSGBatDSP() (*bsgBatDSP, error) {
	ex, err := mel.New(mel.Config{SampleRate: bsgBatRawSampleRate, FrameSize: bsgBatFFTSize, HopSize: bsgBatHopSize, NumMels: bsgBatMelBands, MinHz: bsgBatMinFrequency, MaxHz: bsgBatMaxFrequency, Scale: mel.Slaney, Norm: mel.NormSlaney, Input: mel.InputPower, Log: mel.Log10, LogOffset: bsgBatLogOffset})
	if err != nil {
		return nil, err
	}
	return &bsgBatDSP{extractor: ex, matrix: mel.Matrix{Data: make([]float32, bsgBatFrames*bsgBatMelBands)}, column: make([]float32, bsgBatFrames), output: make([]float32, bsgBatFrames*bsgBatMelBands)}, nil
}

func (d *bsgBatDSP) preprocess(samples []float32) ([]float32, error) {
	if _, err := d.extractor.ComputeInto(&d.matrix, samples, stft.NoPad); err != nil {
		return nil, err
	}
	values := d.matrix.Data
	var mean float64
	for _, v := range values {
		mean += float64(v)
	}
	mean /= float64(len(values))
	var variance float64
	for _, v := range values {
		delta := float64(v) - mean
		variance += delta * delta
	}
	stdDev := math.Sqrt(variance / float64(len(values)))
	if stdDev == 0 {
		// A constant spectrogram (e.g. digital silence) normalises to all zeros:
		// every value equals its band median, so the clipped output is 0.
		clear(d.output)
		return d.output, nil
	}
	for band := range bsgBatMelBands {
		for frame := range bsgBatFrames {
			d.column[frame] = values[frame*bsgBatMelBands+band]
		}
		slices.Sort(d.column)
		median := float64(d.column[bsgBatFrames/2-1]+d.column[bsgBatFrames/2]) / 2
		for frame := range bsgBatFrames {
			v := (float64(values[frame*bsgBatMelBands+band]) - median) / stdDev
			d.output[frame*bsgBatMelBands+band] = float32(max(0, min(bsgBatClipMaximum, v)))
		}
	}
	return d.output, nil
}
