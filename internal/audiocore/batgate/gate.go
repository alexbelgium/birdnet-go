// Package batgate is a cheap signal check run before the bat models. It skips a
// window when it holds no ultrasonic event at all, or when every ultrasonic event
// in it repeats at the mains period (an interference pulse train locked to 50 or
// 60 Hz) with nothing left once that repeating pattern is removed. Anything else
// is passed to the models.
package batgate

import (
	"math"
	"slices"

	"github.com/tphakala/go-audio-dsp/stft"
)

const (
	bandLowHz  = 15000.0 // lowest bat call energy considered: noctules call down to ~17 kHz, and a faint one needs the margin
	bandHighHz = 90000.0 // upper edge, kept under the 96 kHz Nyquist of a 192 kHz source
	subBands   = 5       // the band is cut into this many sub-bands judged separately, so a narrowband call is not diluted by the whole band, and noise in one sub-band does not hide calls in another

	eventFactor   = 8.0 // a frame is an event when a sub-band exceeds this many times its window median (~9 dB)
	minEventFrame = 3   // event frames needed (a 2 ms call spans about 3 frames)

	residualFactor = 3.0 // a residual event must also exceed this many times the template near its phase, so pulse-to-pulse level jitter is not taken for a call

	lockMinScore = 0.3 // share of the envelope variance a mains-period template must explain to call the window mains-locked

	periodSearchSpan  = 0.01   // the mains period is searched within +-1 % of nominal (grid drift)
	periodSearchSteps = 21     // coarse steps over the span (0.1 % apart)
	periodRefineSteps = 11     // fine steps around the best coarse period
	defaultMainsHz    = 50     // used for any mains setting other than 60
	altMainsHz        = 60     // the other supported mains frequency
	envFrameHz        = 1000   // frames are about 1/envFrameHz long
	minFrameSize      = 64     // smallest STFT frame
	minWindowFrames   = 8      // a window shorter than this many frames is not judged
	minBandWidthHz    = 5000.0 // a usable band must be at least this wide above bandLowHz
	nyquistMargin     = 0.95   // the band stops at this share of the Nyquist frequency
)

// Gate reasons, as logged.
const (
	ReasonSourceRateUnknown = "source rate unknown"
	ReasonNoUltrasonicBand  = "no ultrasonic band"
	ReasonWindowTooShort    = "window too short"
	ReasonSTFTUnavailable   = "stft unavailable"
	ReasonDigitalSilence    = "digital silence"
	ReasonNoEvent           = "no ultrasonic event"
	ReasonNotMainsLocked    = "events not mains-locked"
	ReasonEventOverMains    = "event over mains noise"
	ReasonMainsOnly         = "mains-locked noise only"
)

// Verdict is the gate's decision for one window.
type Verdict struct {
	Skip      bool    // true when the window should not be sent to the bat models
	Reason    string  // why, one of the Reason constants
	Events    int     // event frames in the envelope
	Residual  int     // event frames left after removing the mains-period template
	LockScore float64 // share of envelope variance explained by the mains-period template (0..1)
}

// Check judges one analysis window. samples are mono float32 in [-1, 1) at
// sampleRate; sourceRate is the capture rate before any resampling (0 when
// unknown); mainsHz is 60 for 60 Hz mains, anything else means 50 Hz.
// The gate never skips when it cannot judge (unknown or too low source rate,
// too short a window, digital silence): it passes the window to the models.
func Check(samples []float32, sampleRate, sourceRate, mainsHz int) Verdict {
	pass := func(reason string) Verdict { return Verdict{Reason: reason} }

	if sourceRate <= 0 || sampleRate <= 0 {
		return pass(ReasonSourceRateUnknown)
	}
	high := min(bandHighHz, nyquistMargin*float64(sourceRate)/2, nyquistMargin*float64(sampleRate)/2)
	if high <= bandLowHz+minBandWidthHz {
		return pass(ReasonNoUltrasonicBand)
	}
	if mainsHz != altMainsHz {
		mainsHz = defaultMainsHz
	}

	// Envelope: sub-band power in ~1 ms frames with 50 % overlap.
	frameSize := nextPow2(sampleRate / envFrameHz)
	hop := frameSize / 2
	if len(samples) < frameSize*minWindowFrames {
		return pass(ReasonWindowTooShort)
	}
	plan, err := stft.New(stft.Config{FrameSize: frameSize, HopSize: hop, Window: stft.HannSymmetric})
	if err != nil {
		return pass(ReasonSTFTUnavailable)
	}
	binHz := float64(sampleRate) / float64(frameSize)
	lo, hi := int(bandLowHz/binHz), min(int(high/binHz), plan.NumBins()-1)
	numFrames := 1 + (len(samples)-frameSize)/hop
	bands := make([][]float64, subBands)
	for k := range bands {
		bands[k] = make([]float64, numFrames)
	}
	frame := make([]float32, frameSize)
	power := make([]float32, plan.NumBins())
	span := hi - lo + 1
	for f := range numFrames {
		copy(frame, samples[f*hop:f*hop+frameSize])
		plan.PowerInto(power, frame, stft.NoPad)
		for k := range bands {
			var s float64
			for b := lo + span*k/subBands; b < lo+span*(k+1)/subBands; b++ {
				s += float64(power[b])
			}
			bands[k][f] = s
		}
	}

	// Each sub-band is scaled by its own median; the envelope keeps the
	// strongest sub-band of each frame.
	env := make([]float64, numFrames)
	for k := range bands {
		m := median(bands[k])
		if m <= 0 {
			return pass(ReasonDigitalSilence)
		}
		for f, x := range bands[k] {
			env[f] = max(env[f], x/m)
		}
	}

	limit := eventFactor * median(env)
	v := Verdict{Events: countAbove(env, limit)}
	if v.Events < minEventFrame {
		v.Skip, v.Reason = true, ReasonNoEvent
		return v
	}

	// Fold the envelope on the mains period, take the per-phase median as the
	// template of the repeating interference, and keep the period that explains
	// the most variance: a coarse search over the grid drift, then a fine one
	// around the best coarse period.
	hopSec := float64(hop) / float64(sampleRate)
	nominal := 1 / float64(mainsHz)
	total := variance(env)
	if total <= 0 {
		v.Reason = ReasonNotMainsLocked
		return v
	}
	coarseStep := 2 * periodSearchSpan * nominal / float64(periodSearchSteps-1)
	bestPeriod, bestScore := 0.0, -1.0
	try := func(period float64) {
		if s := foldedVariance(env, hopSec, period) / total; s > bestScore {
			bestScore, bestPeriod = s, period
		}
	}
	for i := range periodSearchSteps {
		try(nominal*(1-periodSearchSpan) + coarseStep*float64(i))
	}
	center := bestPeriod
	for i := range periodRefineSteps {
		try(center - coarseStep/2 + coarseStep*float64(i)/float64(periodRefineSteps-1))
	}
	v.LockScore = math.Min(1, math.Max(0, bestScore))
	if v.LockScore < lockMinScore {
		v.Reason = ReasonNotMainsLocked
		return v
	}

	// Residual: what the mains template does not explain. Each frame is compared
	// with the template's peak over its phase and the two next to it, so a pulse
	// landing across a frame boundary is still explained.
	tmpl := foldTemplate(env, hopSec, bestPeriod)
	n := len(tmpl)
	for f, x := range env {
		ph := phaseOf(f, hopSec, bestPeriod, n)
		ref := max(tmpl[(ph+n-1)%n], tmpl[ph], tmpl[(ph+1)%n])
		if x-ref > limit && x > residualFactor*ref {
			v.Residual++
		}
	}
	if v.Residual >= minEventFrame {
		v.Reason = ReasonEventOverMains
		return v
	}
	v.Skip, v.Reason = true, ReasonMainsOnly
	return v
}

// phaseCount is the number of phase bins of one period.
func phaseCount(hopSec, period float64) int {
	return max(4, int(math.Round(period/hopSec)))
}

// phaseOf returns the phase bin of frame f for the given period.
func phaseOf(f int, hopSec, period float64, n int) int {
	t := float64(f) * hopSec
	return int(math.Mod(t, period)/period*float64(n)) % n
}

// foldTemplate returns the per-phase median of env folded on period.
func foldTemplate(env []float64, hopSec, period float64) []float64 {
	n := phaseCount(hopSec, period)
	buckets := make([][]float64, n)
	for f, x := range env {
		ph := phaseOf(f, hopSec, period, n)
		buckets[ph] = append(buckets[ph], x)
	}
	tmpl := make([]float64, n)
	for i, b := range buckets {
		if len(b) > 0 {
			tmpl[i] = median(b)
		}
	}
	return tmpl
}

// foldedVariance is the variance of the folded template as seen by the frames,
// the part of the envelope variance the repeating pattern accounts for.
func foldedVariance(env []float64, hopSec, period float64) float64 {
	tmpl := foldTemplate(env, hopSec, period)
	n := len(tmpl)
	var s, ss float64
	for f := range env {
		x := tmpl[phaseOf(f, hopSec, period, n)]
		s += x
		ss += x * x
	}
	cnt := float64(len(env))
	m := s / cnt
	return ss/cnt - m*m
}

func countAbove(v []float64, limit float64) int {
	n := 0
	for _, x := range v {
		if x > limit {
			n++
		}
	}
	return n
}

func variance(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	m := s / float64(len(v))
	var ss float64
	for _, x := range v {
		ss += (x - m) * (x - m)
	}
	return ss / float64(len(v))
}

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func nextPow2(n int) int {
	p := minFrameSize
	for p < n {
		p <<= 1
	}
	return p
}
