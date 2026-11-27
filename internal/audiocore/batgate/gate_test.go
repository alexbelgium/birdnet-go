package batgate

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dsppcm "github.com/tphakala/go-audio-dsp/pcm"

	"github.com/tphakala/birdnet-go/internal/audiocore/resample"
)

const (
	testRate       = 256000 // the Bat model's rate
	testSourceRate = 192000 // a 192 kHz capture, band 15-90 kHz
	testWindow     = 144000 // one Bat window, 0.5625 s
)

// noiseWindow returns a window of faint white noise, so every sub-band has a
// non-zero median like a real recording.
func noiseWindow(t *testing.T) []float32 {
	t.Helper()
	r := rand.New(rand.NewPCG(1, 2))
	s := make([]float32, testWindow)
	for i := range s {
		s[i] = float32(0.001 * r.NormFloat64())
	}
	return s
}

// addSweep adds a Hann-shaped linear FM sweep from f0 to f1 Hz lasting dur
// seconds at sample start: a bat call, or a single-frame bat click when short.
func addSweep(s []float32, start int, dur, f0, f1, amp float64) {
	n := int(dur * testRate)
	phase := 0.0
	for i := range n {
		frac := float64(i) / float64(n)
		f := f0 + (f1-f0)*frac
		phase += 2 * math.Pi * f / testRate
		w := 0.5 - 0.5*math.Cos(2*math.Pi*frac)
		s[start+i] += float32(amp * w * math.Sin(phase))
	}
}

// addHarmonicCall adds a constant-frequency call of dur seconds at f0 with
// harmonics at 2*f0, 3*f0, ... up to count, each at rel times the previous
// amplitude: a low call whose harmonic stack reaches every sub-band.
func addHarmonicCall(s []float32, rate, start int, dur, f0 float64, count int, amp, rel float64) {
	n := int(dur * float64(rate))
	for h := 1; h <= count; h++ {
		f := f0 * float64(h)
		for i := range n {
			w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
			s[start+i] += float32(amp * w * math.Sin(2*math.Pi*f*float64(i)/float64(rate)))
		}
		amp *= rel
	}
}

// clip hard-limits the window to +-level, as an overloaded recorder does.
func clip(s []float32, level float32) {
	for i, x := range s {
		s[i] = max(-level, min(level, x))
	}
}

// addImpulse adds a one-sample spike: a broadband click.
func addImpulse(s []float32, at int, amp float64) {
	s[at] += float32(amp)
}

// addClicks adds count broadband impulses at irregular times with levels
// spread from 0.05 to 0.4 and one at 0.5, as rain or crackle gives: impulses
// of many levels, few of them near the loudest.
func addClicks(s []float32, count int) {
	for i, at := range irregular(count) {
		amp := 0.05 + 0.35*float64((i*7)%count)/float64(count)
		if i == count/2 {
			amp = 0.5
		}
		addImpulse(s, at, amp)
	}
}

// irregular returns count sample positions spread over the window without a
// fixed period, so they are not taken for a mains-locked pulse train.
func irregular(count int) []int {
	pos := make([]int, count)
	for i := range pos {
		pos[i] = 2000 + i*(testWindow-4000)/count + (i*i*373)%1500
	}
	return pos
}

func TestCheck_NoiseOnlyIsSkipped(t *testing.T) {
	t.Parallel()
	v := Check(noiseWindow(t), testRate, testSourceRate, 50)
	assert.True(t, v.Skip)
	assert.Equal(t, ReasonNoEvent, v.Reason)
}

func TestCheck_BroadbandClicksOnlyAreSkipped(t *testing.T) {
	t.Parallel()
	s := noiseWindow(t)
	addClicks(s, 20)
	v := Check(s, testRate, testSourceRate, 50)
	assert.True(t, v.Skip, "a window of broadband impulses must be skipped: %+v", v)
	assert.Equal(t, ReasonClicksOnly, v.Reason)
	assert.GreaterOrEqual(t, v.Events, minEventFrame)
	assert.Less(t, v.Events-v.Clicks, minEventFrame)
}

func TestCheck_BatCallsPass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		dur, f0, f1 float64
		amp         float64
		calls       int
		clipAt      float32 // 0: no clipping
	}{
		// A single-frame click, under 1 ms, confined to a few kHz around 45 kHz.
		{name: "single-frame narrowband clicks", dur: 0.0008, f0: 46000, f1: 44000, amp: 0.3, calls: 3},
		// A pipistrelle-like FM-QCF call, 5 ms from 75 to 45 kHz.
		{name: "pipistrelle calls", dur: 0.005, f0: 75000, f1: 45000, amp: 0.3, calls: 4},
		// A loud, steep Myotis-like sweep, 3 ms from 110 to 25 kHz.
		{name: "loud steep myotis sweeps", dur: 0.003, f0: 110000, f1: 25000, amp: 0.9, calls: 4},
		// The same sweeps from an overloaded recorder, hard-clipped at 0.1.
		{name: "clipped myotis sweeps", dur: 0.003, f0: 110000, f1: 25000, amp: 0.9, calls: 4, clipAt: 0.1},
		// Very short clipped sweeps, 0.18 ms, each a single frame.
		{name: "clipped 0.18 ms sweeps", dur: 0.00018, f0: 90000, f1: 30000, amp: 0.9, calls: 3, clipAt: 0.05},
		// Feeding-buzz calls, short and loud.
		{name: "feeding buzz", dur: 0.0007, f0: 60000, f1: 30000, amp: 0.8, calls: 6},
		// A low noctule call, 8 ms from 30 to 18 kHz.
		{name: "noctule calls", dur: 0.008, f0: 30000, f1: 18000, amp: 0.3, calls: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := noiseWindow(t)
			for _, at := range irregular(tc.calls) {
				addSweep(s, at, tc.dur, tc.f0, tc.f1, tc.amp)
			}
			if tc.clipAt > 0 {
				clip(s, tc.clipAt)
			}
			v := Check(s, testRate, testSourceRate, 50)
			assert.False(t, v.Skip, "bat calls must reach the models: %+v", v)
			assert.Equal(t, 0, v.Clicks, "no bat call frame may be taken for a broadband click: %+v", v)
		})
	}
}

func TestCheck_HarmonicStackPasses(t *testing.T) {
	t.Parallel()
	// A 17.5 kHz call with harmonics up to 87.5 kHz, each 3 dB under the
	// previous one, puts energy in every sub-band; it is still a bat call.
	for _, m := range []struct{ rate, window int }{{testRate, testWindow}, {384000, 393472}} {
		s := make([]float32, m.window)
		copy(s, noiseWindow(t))
		for i := testWindow; i < m.window; i++ {
			s[i] = s[i-testWindow]
		}
		for c := range 3 {
			addHarmonicCall(s, m.rate, 2000+c*m.window/3, 0.004, 17500, 5, 0.3, 0.7)
		}
		v := Check(s, m.rate, testSourceRate, 50)
		assert.False(t, v.Skip, "harmonic calls must reach the models at %d Hz: %+v", m.rate, v)
		assert.Equal(t, 0, v.Clicks, "rate %d: %+v", m.rate, v)
	}
}

func TestCheck_LoneShortBatClickAmongClicksPasses(t *testing.T) {
	t.Parallel()
	// Rain-like impulses alone are skipped as clicks. A louder audible thump
	// (10 ms at 500 Hz) sets the window peak, so no impulse is protected as
	// possibly clipped and none counts as an event.
	s := noiseWindow(t)
	addClicks(s, 30)
	for i := range 2560 {
		s[120000+i] += float32(0.9 * math.Sin(2*math.Pi*500*float64(i)/testRate))
	}
	v := Check(s, testRate, testSourceRate, 50)
	assert.True(t, v.Skip, "%+v", v)
	assert.Equal(t, ReasonClicksOnly, v.Reason)
	assert.Equal(t, v.Events, v.Clicks, "%+v", v)

	// One 0.18 ms bat click among them, one or two frames, leaves fewer than
	// minEventFrame events once the impulses are set aside; it is tonal, so
	// the window still reaches the models.
	addSweep(s, 70001, 0.00018, 46000, 44000, 0.3)
	v = Check(s, testRate, testSourceRate, 50)
	assert.False(t, v.Skip, "a lone bat click among clicks must reach the models: %+v", v)
	assert.Equal(t, ReasonTonalAmongClicks, v.Reason)
}

func TestCheck_SingleFrameBatClicksAmongBroadbandClicksPass(t *testing.T) {
	t.Parallel()
	s := noiseWindow(t)
	addClicks(s, 20)
	// Three single-frame bat clicks between the impulses still make the window:
	// the impulses are set aside, the bat clicks are not.
	for _, at := range []int{9000, 61000, 117000} {
		addSweep(s, at, 0.0008, 46000, 44000, 0.3)
	}
	v := Check(s, testRate, testSourceRate, 50)
	assert.False(t, v.Skip, "bat clicks mixed with broadband clicks must reach the models: %+v", v)
	assert.Positive(t, v.Clicks)
}

func TestCheck_NarrowBandKeepsClicksAsEvents(t *testing.T) {
	t.Parallel()
	// A 96 kHz capture leaves a 15-45.6 kHz band, too narrow to tell a click
	// from a sweep, so the click test is off and impulses still count.
	s := noiseWindow(t)
	addClicks(s, 20)
	v := Check(s, testRate, 96000, 50)
	assert.Equal(t, 0, v.Clicks)
	assert.GreaterOrEqual(t, v.Events, minEventFrame)
	assert.False(t, v.Skip, "%+v", v)
}

func TestCheck_ClippedShortCallsPassAfterResampling(t *testing.T) {
	t.Parallel()
	// Three 0.3 ms sweeps from 95 to 25 kHz, captured at 192 kHz by a recorder
	// overloaded 18 times over (clipped at 0.05 of a 0.9 call), then resampled
	// to each bat model's rate as the live path does. Clipping makes each call
	// as flat as an impulse; the calls must still reach the models.
	const src = 192000
	r := rand.New(rand.NewPCG(3, 4))
	in := make([]float64, src*3/4)
	for i := range in {
		in[i] = 0.001 * r.NormFloat64()
	}
	for c := range 3 {
		start, n := 10000+c*13440, 57 // 0.3 ms at 192 kHz
		phase := 0.0
		for i := range n {
			frac := float64(i) / float64(n)
			phase += 2 * math.Pi * (95000 - 70000*frac) / src
			in[start+i] += 0.9 * (0.5 - 0.5*math.Cos(2*math.Pi*frac)) * math.Sin(phase)
		}
	}
	pcm := make([]byte, 2*len(in))
	for i, x := range in {
		v := int16(max(-0.05, min(0.05, x)) * 16384)
		pcm[2*i], pcm[2*i+1] = byte(v), byte(uint16(v)>>8) //nolint:gosec // G115: little-endian split of a test sample
	}
	for _, rate := range []int{256000, 384000} {
		rs, err := resample.NewResampler(src, rate)
		require.NoError(t, err)
		out, err := rs.ResampleInto(pcm)
		rs.Close()
		require.NoError(t, err)
		s := make([]float32, len(out)/2)
		_, err = dsppcm.BytesToFloat32(s, out)
		require.NoError(t, err)
		v := Check(s, rate, src, 50)
		assert.False(t, v.Skip, "clipped calls must reach the models at %d Hz: %+v", rate, v)
		assert.GreaterOrEqual(t, v.Events-v.Clicks, minEventFrame, "at %d Hz: %+v", rate, v)
	}
}
