package classifier

import (
	"math"

	"github.com/tphakala/go-audio-dsp/stft"
)

const (
	batDetect2SampleRate       = 256000
	batDetect2FFTSize          = 512
	batDetect2HopSize          = 128
	batDetect2MinFrequency     = 10000
	batDetect2MaxFrequency     = 120000
	batDetect2ImageHeight      = 128
	batDetect2TileWidth        = 256
	batDetect2ResizeFactor     = 0.5
	batDetect2PCENGain         = 0.98
	batDetect2PCENBias         = 2.0
	batDetect2PCENPower        = 0.5
	batDetect2PCENEpsilon      = 1e-6
	batDetect2PCENTimeConstant = 0.4
	batDetect2PCENSmoothingHop = 512
	batDetect2OutputFrameRate  = 1000
)

type batDetect2DSP struct {
	plan    *stft.Plan
	spectra []complex64
	spec    []float64
	image   []float32
	x0, x1  []int
	wx      []float64
}

func newBatDetect2DSP(sampleCount int) (*batDetect2DSP, error) {
	p, err := stft.New(stft.Config{FrameSize: batDetect2FFTSize, HopSize: batDetect2HopSize, Window: stft.Hann})
	if err != nil {
		return nil, err
	}
	frames := p.NumFrames(sampleCount, stft.PadReflect)
	lo, hi := batDetect2FrequencyIndex(batDetect2MinFrequency), batDetect2FrequencyIndex(batDetect2MaxFrequency)
	width := int(batDetect2ResizeFactor * float64(frames))
	d := &batDetect2DSP{plan: p, spectra: make([]complex64, frames*p.NumBins()), spec: make([]float64, (hi-lo)*frames), image: make([]float32, batDetect2ImageHeight*width), x0: make([]int, width), x1: make([]int, width), wx: make([]float64, width)}
	for x := range width {
		sx := (float64(x)+.5)*float64(frames)/float64(width) - .5
		base := math.Floor(sx)
		d.x0[x] = max(0, min(frames-1, int(base)))
		d.x1[x] = min(frames-1, d.x0[x]+1)
		d.wx[x] = sx - base
		if sx < 0 {
			d.wx[x] = 0
		}
	}
	return d, nil
}

func batDetect2FrequencyIndex(freq int) int {
	return int(math.Floor(float64(freq*2) / batDetect2SampleRate * float64(batDetect2FFTSize/2+1)))
}

func (d *batDetect2DSP) preprocess(samples []float32) (image []float32, width int) {
	frames := d.plan.NumFrames(len(samples), stft.PadReflect)
	rows := make([][]complex64, frames)
	bins := d.plan.NumBins()
	for f := range frames {
		rows[f] = d.spectra[f*bins : (f+1)*bins]
	}
	d.plan.Spectrum(rows, samples, stft.PadReflect)
	lo, hi := batDetect2FrequencyIndex(batDetect2MinFrequency), batDetect2FrequencyIndex(batDetect2MaxFrequency)
	srcH := hi - lo
	for y := range srcH {
		for x := range frames {
			v := d.spectra[x*bins+lo+y]
			d.spec[y*frames+x] = math.Hypot(float64(real(v)), float64(imag(v)))
		}
	}
	tFrames := batDetect2PCENTimeConstant * float64(batDetect2SampleRate/10) / batDetect2PCENSmoothingHop
	s := (math.Sqrt(1+4*tFrames*tFrames) - 1) / (2 * tFrames * tFrames)
	scale := math.Exp2(31)
	biasPower := math.Pow(batDetect2PCENBias, batDetect2PCENPower)
	for y := range srcH {
		row := d.spec[y*frames : (y+1)*frames]
		m := 0.0
		mean := 0.0
		for x, v := range row {
			v *= scale
			m = (1-s)*m + s*v
			smooth := math.Pow(batDetect2PCENEpsilon+m, -batDetect2PCENGain)
			row[x] = biasPower * math.Expm1(batDetect2PCENPower*math.Log1p(v*smooth/batDetect2PCENBias))
			mean += row[x]
		}
		mean /= float64(frames)
		for x, v := range row {
			row[x] = max(0, v-mean)
		}
	}
	width = len(d.x0)
	for y := range batDetect2ImageHeight {
		sy := (float64(y)+.5)*float64(srcH)/batDetect2ImageHeight - .5
		base := math.Floor(sy)
		y0 := max(0, min(srcH-1, int(base)))
		y1 := min(srcH-1, y0+1)
		wy := sy - base
		if sy < 0 {
			wy = 0
		}
		for x := range width {
			wx := d.wx[x]
			top := d.spec[y0*frames+d.x0[x]]*(1-wx) + d.spec[y0*frames+d.x1[x]]*wx
			bottom := d.spec[y1*frames+d.x0[x]]*(1-wx) + d.spec[y1*frames+d.x1[x]]*wx
			d.image[y*width+x] = float32(top*(1-wy) + bottom*wy)
		}
	}
	return d.image, width
}

type batDetect2Peak struct {
	score float32
	y, x  int
}

func batDetect2NMS(detection []float32, height, width, copyWidth int) []batDetect2Peak {
	const radius = 4
	peaks := make([]batDetect2Peak, 0)
	for y := range height {
		for x := range copyWidth {
			v := detection[y*width+x]
			if v < batDetect2DetectionThreshold {
				continue
			}
			keep := true
			for yy := max(0, y-radius); yy <= min(height-1, y+radius) && keep; yy++ {
				for xx := max(0, x-radius); xx <= min(width-1, x+radius); xx++ {
					if detection[yy*width+xx] > v {
						keep = false
						break
					}
				}
			}
			if keep {
				peaks = append(peaks, batDetect2Peak{v, y, x})
			}
		}
	}
	return peaks
}
