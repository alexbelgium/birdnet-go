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

	eventFactor   = 8.0 // a frame is an event when its band power exceeds this many times the window median (~9 dB)
	minEventFrame = 3   // event frames needed (a 2 ms call spans about 3 frames)

	residualFactor = 3.0 // a residual event must also exceed this many times the template at its phase, so pulse-to-pulse level jitter is not taken for a call

	lockMinScore = 0.3 // share of the band power variance a mains-period template must explain to call the window mains-locked

	periodSearchSpan  = 0.01 // the mains period is searched within +-1 % of nominal (grid drift)
	periodSearchSteps = 21
)

// Verdict is the gate's decision for one window.
type Verdict struct {
	Skip      bool    // true when the window should not be sent to the bat models
	Reason    string  // why, for logs
	Events    int     // event frames in the raw envelope
	Residual  int     // event frames left after removing the mains-period template
	LockScore float64 // share of envelope variance explained by the mains-period template (0..1)
}

// Check judges one analysis window. samples are mono float32 in [-1, 1) at
// sampleRate; sourceRate is the capture rate before any resampling (0 when
// unknown); mainsHz is 50 or 60.
// The gate never skips when it cannot judge (unknown or too low source rate,
// too short a window, digital silence): it passes the window to the models.
func Check(samples []float32, sampleRate, sourceRate, mainsHz int) Verdict {
	pass := func(reason string) Verdict { return Verdict{Reason: reason} }

	if sourceRate <= 0 || sampleRate <= 0 {
		return pass("source rate unknown")
	}
	high := min(bandHighHz, 0.95*float64(sourceRate)/2, 0.95*float64(sampleRate)/2)
	if high <= bandLowHz+5000 {
		return pass("no ultrasonic band")
	}
	if mainsHz <= 0 {
		mainsHz = 50
	}

	// Envelope: band power in ~1 ms frames with 50 % overlap.
	frameSize := nextPow2(sampleRate / 1000)
	hop := frameSize / 2
	if len(samples) < frameSize*8 {
		return pass("window too short")
	}
	plan, err := stft.New(stft.Config{FrameSize: frameSize, HopSize: hop, Window: stft.HannSymmetric})
	if err != nil {
		return pass("stft unavailable")
	}
	binHz := float64(sampleRate) / float64(frameSize)
	lo, hi := int(bandLowHz/binHz), int(high/binHz)
	numFrames := 1 + (len(samples)-frameSize)/hop
	env := make([]float64, numFrames)
	frame := make([]float32, frameSize)
	power := make([]float32, plan.NumBins())
	for f := range numFrames {
		copy(frame, samples[f*hop:f*hop+frameSize])
		plan.PowerInto(power, frame, stft.NoPad)
		var s float64
		for b := lo; b <= hi && b < len(power); b++ {
			s += float64(power[b])
		}
		env[f] = s
	}

	floor := median(env)
	if floor <= 0 {
		return pass("digital silence")
	}
	limit := eventFactor * floor
	v := Verdict{Events: countAbove(env, limit)}
	if v.Events < minEventFrame {
		v.Skip, v.Reason = true, "no ultrasonic event"
		return v
	}

	// Fold the envelope on the mains period, take the per-phase median as the
	// template of the repeating interference, and keep the period that explains
	// the most variance (the grid frequency drifts a little).
	hopSec := float64(hop) / float64(sampleRate)
	nominal := 1 / float64(mainsHz)
	total := variance(env)
	var bestTemplate []float64
	var bestPeriod float64
	bestScore := -1.0
	for i := range periodSearchSteps {
		period := nominal * (1 - periodSearchSpan + 2*periodSearchSpan*float64(i)/float64(periodSearchSteps-1))
		tmpl, phases := foldTemplate(env, hopSec, period)
		score := 0.0
		if total > 0 {
			score = templateVariance(tmpl, phases) / total
		}
		if score > bestScore {
			bestScore, bestTemplate, bestPeriod = score, tmpl, period
		}
	}
	v.LockScore = math.Min(1, math.Max(0, bestScore))
	if v.LockScore < lockMinScore {
		v.Reason = "events not mains-locked"
		return v
	}

	// Residual: what the mains template does not explain.
	// Each frame is compared with the template's peak over its phase and the two
	// next to it, so a pulse landing across a frame boundary is still explained.
	_, phases := foldTemplate(env, hopSec, bestPeriod)
	n := len(bestTemplate)
	for f, x := range env {
		ph := phases[f]
		ref := max(bestTemplate[(ph+n-1)%n], bestTemplate[ph], bestTemplate[(ph+1)%n])
		if x-ref > limit && x > residualFactor*ref {
			v.Residual++
		}
	}
	if v.Residual >= minEventFrame {
		v.Reason = "event over mains noise"
		return v
	}
	v.Skip, v.Reason = true, "mains-locked noise only"
	return v
}

// foldTemplate returns the per-phase median of env folded on period, and the
// phase index of every frame.
func foldTemplate(env []float64, hopSec, period float64) (tmpl []float64, phases []int) {
	nPhase := max(4, int(math.Round(period/hopSec)))
	buckets := make([][]float64, nPhase)
	phases = make([]int, len(env))
	for f, x := range env {
		t := float64(f) * hopSec
		ph := int(math.Mod(t, period)/period*float64(nPhase)) % nPhase
		phases[f] = ph
		buckets[ph] = append(buckets[ph], x)
	}
	tmpl = make([]float64, nPhase)
	for i, b := range buckets {
		if len(b) > 0 {
			tmpl[i] = median(b)
		}
	}
	return tmpl, phases
}

// templateVariance is the variance of the template as seen by the frames, the
// part of the envelope variance the repeating pattern accounts for.
func templateVariance(tmpl []float64, phases []int) float64 {
	vals := make([]float64, len(phases))
	for f, ph := range phases {
		vals[f] = tmpl[ph]
	}
	return variance(vals)
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
	p := 64
	for p < n {
		p <<= 1
	}
	return p
}
