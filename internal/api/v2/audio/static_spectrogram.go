package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/tphakala/birdnet-go/internal/audiocore"
)

const (
	// StaticSpectrogramPath is kept with the handler so PrivateMode can exempt
	// the route while its dynamic live-audio middleware remains authoritative.
	StaticSpectrogramPath = "/streams/spectrogram/:sourceID"

	staticSpectrogramDefaultDuration = 10 * time.Second
	staticSpectrogramMinDuration     = time.Second
	staticSpectrogramMaxDuration     = 10 * time.Second
	staticSpectrogramCaptureGrace    = 5 * time.Second
	staticSpectrogramGlobalLimit     = 2
	staticSpectrogramFFTSize         = 2048
	staticSpectrogramWidth           = 960
	staticSpectrogramHeight          = 512
	staticSpectrogramBitDepth        = 16
	staticSpectrogramChannels        = 1
	staticSpectrogramBytesPerSample  = staticSpectrogramBitDepth / 8
	staticSpectrogramMinDB           = -100.0
	staticSpectrogramMaxDB           = -20.0
	// staticSpectrogramDetectFrames caps the FFT frames averaged to detect the
	// real source rate.
	staticSpectrogramDetectFrames = 64
)

// staticSpectrogramSourceRates are the real source rates checked, lowest first,
// when audio is captured at a higher rate (bat streams always capture at
// >= 192 kHz, whatever the device currently sends).
var staticSpectrogramSourceRates = [...]int{48000, 96000}

const (
	staticSpectrogramSampleRateHeader = "X-Spectrogram-Sample-Rate"
	staticSpectrogramGeneratedHeader  = "X-Spectrogram-Generated-At"
	staticSpectrogramDurationHeader   = "X-Spectrogram-Duration"
)

var staticSpectrogramCaptures = struct {
	sync.Mutex
	active map[string]struct{}
	slots  chan struct{}
}{
	active: make(map[string]struct{}),
	slots:  make(chan struct{}, staticSpectrogramGlobalLimit),
}

// RegisterStaticSpectrogramRoutes registers the native-rate snapshot endpoint.
func (c *Handler) RegisterStaticSpectrogramRoutes(g *echo.Group) {
	g.GET(StaticSpectrogramPath, c.GetStaticSpectrogram, c.publicLiveAudioAuth)
}

// GetStaticSpectrogram captures native-rate PCM from an existing source and
// returns a full-Nyquist-range PNG. The request context owns the router route,
// so a disconnected client stops capture immediately.
func (c *Handler) GetStaticSpectrogram(ctx echo.Context) error {
	sourceID, err := c.validateAndDecodeSourceID(ctx)
	if err != nil {
		return err
	}

	duration := staticSpectrogramDuration(ctx.QueryParam("duration"))
	eng := c.Engine.Load()
	if eng == nil {
		return c.HandleError(ctx, nil, "Audio engine is not available", http.StatusServiceUnavailable)
	}
	source, ok := eng.Registry().Get(sourceID)
	if !ok {
		return c.HandleError(ctx, nil, "Audio source not found", http.StatusNotFound)
	}
	if source.SampleRate <= 0 {
		return c.HandleError(ctx, nil, "Audio source sample rate is not available", http.StatusServiceUnavailable)
	}

	clientSourceKey := c.extractRemoteAddr(ctx) + "\x00" + sourceID
	release, acquired := acquireStaticSpectrogramCapture(clientSourceKey)
	if !acquired {
		return c.HandleError(ctx, nil, "A static spectrogram capture is already active", http.StatusTooManyRequests)
	}
	defer release()

	targetBytes := int64(source.SampleRate) * int64(duration/time.Second) * staticSpectrogramBytesPerSample
	consumer := newStaticSpectrogramConsumer(source.SampleRate, targetBytes)
	if routeErr := eng.Router().AddRoute(sourceID, consumer, source.SampleRate, 0, nil); routeErr != nil {
		return c.HandleError(ctx, routeErr, "Failed to start static spectrogram capture", http.StatusInternalServerError)
	}
	defer eng.Router().RemoveRoute(sourceID, consumer.ID())

	captureTimer := time.NewTimer(duration + staticSpectrogramCaptureGrace)
	defer captureTimer.Stop()
	select {
	case <-ctx.Request().Context().Done():
		return ctx.Request().Context().Err()
	case <-captureTimer.C:
		return c.HandleError(ctx, nil, "Timed out waiting for audio samples", http.StatusGatewayTimeout)
	case <-consumer.done:
	}

	pcm := consumer.samples()
	// Show the rate the device really sends, not the capture rate, so the axis
	// follows a source that switches between 48 and 192 kHz on its own.
	displayRate := detectSourceSampleRate(pcm, source.SampleRate)
	pngData, renderErr := renderStaticSpectrogram(ctx.Request().Context(), pcm, source.SampleRate, displayRate)
	if renderErr != nil {
		if ctx.Request().Context().Err() != nil {
			return ctx.Request().Context().Err()
		}
		return c.HandleError(ctx, renderErr, "Failed to render static spectrogram", http.StatusInternalServerError)
	}

	generatedAt := time.Now()
	ctx.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	ctx.Response().Header().Set(staticSpectrogramSampleRateHeader, strconv.Itoa(displayRate))
	ctx.Response().Header().Set(staticSpectrogramGeneratedHeader, generatedAt.Format(time.RFC3339))
	ctx.Response().Header().Set(staticSpectrogramDurationHeader, strconv.FormatFloat(duration.Seconds(), 'f', 0, 64))
	return ctx.Blob(http.StatusOK, "image/png", pngData)
}

func staticSpectrogramDuration(raw string) time.Duration {
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return staticSpectrogramDefaultDuration
	}
	return min(max(time.Duration(seconds)*time.Second, staticSpectrogramMinDuration), staticSpectrogramMaxDuration)
}

func acquireStaticSpectrogramCapture(key string) (release func(), ok bool) {
	staticSpectrogramCaptures.Lock()
	if _, exists := staticSpectrogramCaptures.active[key]; exists {
		staticSpectrogramCaptures.Unlock()
		return nil, false
	}
	select {
	case staticSpectrogramCaptures.slots <- struct{}{}:
		staticSpectrogramCaptures.active[key] = struct{}{}
		staticSpectrogramCaptures.Unlock()
	default:
		staticSpectrogramCaptures.Unlock()
		return nil, false
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			staticSpectrogramCaptures.Lock()
			delete(staticSpectrogramCaptures.active, key)
			<-staticSpectrogramCaptures.slots
			staticSpectrogramCaptures.Unlock()
		})
	}, true
}

type staticSpectrogramConsumer struct {
	id         string
	sampleRate int
	target     int64
	written    atomic.Int64
	closed     atomic.Bool
	done       chan struct{}
	doneOnce   sync.Once
	mu         sync.Mutex
	pcm        []byte
}

func newStaticSpectrogramConsumer(sampleRate int, targetBytes int64) *staticSpectrogramConsumer {
	return &staticSpectrogramConsumer{
		id:         fmt.Sprintf("static_spectrogram_%s", uuid.New().String()[:8]),
		sampleRate: sampleRate,
		target:     targetBytes,
		done:       make(chan struct{}),
		pcm:        make([]byte, 0, targetBytes),
	}
}

func (c *staticSpectrogramConsumer) ID() string      { return c.id }
func (c *staticSpectrogramConsumer) SampleRate() int { return c.sampleRate }
func (c *staticSpectrogramConsumer) BitDepth() int   { return staticSpectrogramBitDepth }
func (c *staticSpectrogramConsumer) Channels() int   { return staticSpectrogramChannels }

func (c *staticSpectrogramConsumer) Write(frame audiocore.AudioFrame) error { //nolint:gocritic // interface requires value parameter
	if c.closed.Load() || c.written.Load() >= c.target {
		return nil
	}

	c.mu.Lock()
	remaining := c.target - int64(len(c.pcm))
	if remaining > 0 {
		copyLen := min(int64(len(frame.Data)), remaining)
		c.pcm = append(c.pcm, frame.Data[:copyLen]...)
		c.written.Store(int64(len(c.pcm)))
	}
	complete := int64(len(c.pcm)) >= c.target
	c.mu.Unlock()

	if complete {
		c.doneOnce.Do(func() { close(c.done) })
	}
	return nil
}

func (c *staticSpectrogramConsumer) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *staticSpectrogramConsumer) samples() []int16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	samples := make([]int16, len(c.pcm)/staticSpectrogramBytesPerSample)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(c.pcm[i*staticSpectrogramBytesPerSample:]))
	}
	return samples
}

// detectSourceSampleRate returns the rate the audio really carries when it was
// captured at captureRate. Audio upsampled to the capture rate holds only 16-bit
// rounding noise above its original Nyquist, while real audio, even a quiet
// ultrasonic mic, sits clearly above that floor. The band checked starts at 1.25x
// the candidate Nyquist to stay clear of the resampler's transition band. The
// lowest candidate whose band is at the floor wins; otherwise captureRate is real.
func detectSourceSampleRate(samples []int16, captureRate int) int {
	if len(samples) < staticSpectrogramFFTSize {
		return captureRate
	}
	window := staticSpectrogramWindow()
	var windowPower float64
	for _, w := range window {
		windowPower += w * w
	}

	// Average the power spectrum over evenly spread frames.
	half := staticSpectrogramFFTSize / 2
	power := make([]float64, half)
	frames := min(staticSpectrogramDetectFrames, len(samples)/staticSpectrogramFFTSize)
	step := (len(samples) - staticSpectrogramFFTSize) / max(frames-1, 1)
	buf := make([]complex128, staticSpectrogramFFTSize)
	for f := range frames {
		start := f * step
		for i := range staticSpectrogramFFTSize {
			buf[i] = complex(float64(samples[start+i])/32768.0*window[i], 0)
		}
		staticSpectrogramFFT(buf)
		for bin := range half {
			power[bin] += real(buf[bin])*real(buf[bin]) + imag(buf[bin])*imag(buf[bin])
		}
	}

	// Rounding noise of 16-bit PCM has variance 1/12 LSB^2 per sample; treat a
	// band within 3 dB (2x) of it as empty.
	emptyBandPower := 2 * float64(frames) * windowPower / 12 / (32768.0 * 32768.0)
	binHz := float64(captureRate) / staticSpectrogramFFTSize
	top := int(float64(captureRate) / 2 * 0.95 / binHz)
	for _, rate := range staticSpectrogramSourceRates {
		bottom := int(float64(rate) / 2 * 1.25 / binHz)
		if rate >= captureRate || bottom >= top {
			continue
		}
		var band float64
		for bin := bottom; bin < top; bin++ {
			band += power[bin]
		}
		if band/float64(top-bottom) < emptyBandPower {
			return rate
		}
	}
	return captureRate
}

func staticSpectrogramWindow() []float64 {
	window := make([]float64, staticSpectrogramFFTSize)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(staticSpectrogramFFTSize-1))
	}
	return window
}

// renderStaticSpectrogram renders samples captured at sampleRate, with the
// frequency axis spanning 0 to displayRate/2.
func renderStaticSpectrogram(ctx context.Context, samples []int16, sampleRate, displayRate int) ([]byte, error) {
	if sampleRate <= 0 || len(samples) < staticSpectrogramFFTSize {
		return nil, fmt.Errorf("insufficient PCM for spectrogram")
	}
	displayRate = min(max(displayRate, 1), sampleRate)
	topBin := (staticSpectrogramFFTSize / 2) * displayRate / sampleRate

	img := image.NewRGBA(image.Rect(0, 0, staticSpectrogramWidth, staticSpectrogramHeight))
	window := staticSpectrogramWindow()
	fftBuffer := make([]complex128, staticSpectrogramFFTSize)
	maxStart := len(samples) - staticSpectrogramFFTSize
	for x := range staticSpectrogramWidth {
		if x%32 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		start := 0
		if staticSpectrogramWidth > 1 {
			start = x * maxStart / (staticSpectrogramWidth - 1)
		}
		for i := range staticSpectrogramFFTSize {
			fftBuffer[i] = complex(float64(samples[start+i])/32768.0*window[i], 0)
		}
		staticSpectrogramFFT(fftBuffer)

		for y := range staticSpectrogramHeight {
			bin := min((staticSpectrogramHeight-1-y)*topBin/(staticSpectrogramHeight-1), staticSpectrogramFFTSize/2-1)
			magnitude := 2 * cmplxAbs(fftBuffer[bin]) / float64(staticSpectrogramFFTSize)
			db := 20 * math.Log10(max(magnitude, math.SmallestNonzeroFloat64))
			level := (db - staticSpectrogramMinDB) / (staticSpectrogramMaxDB - staticSpectrogramMinDB)
			img.SetRGBA(x, y, staticSpectrogramColor(level))
		}
	}

	output := contextBuffer{ctx: ctx}
	if err := png.Encode(&output, img); err != nil {
		return nil, fmt.Errorf("encode spectrogram PNG: %w", err)
	}
	return output.Bytes(), nil
}

type contextBuffer struct {
	bytes.Buffer
	ctx context.Context
}

func (b *contextBuffer) Write(p []byte) (int, error) {
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	default:
		return b.Buffer.Write(p)
	}
}

func staticSpectrogramFFT(data []complex128) {
	for i, j := 1, 0; i < len(data); i++ {
		bit := len(data) >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			data[i], data[j] = data[j], data[i]
		}
	}

	for length := 2; length <= len(data); length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		step := complex(math.Cos(angle), math.Sin(angle))
		for start := 0; start < len(data); start += length {
			w := complex(1, 0)
			half := length / 2
			for offset := range half {
				even := data[start+offset]
				odd := w * data[start+offset+half]
				data[start+offset] = even + odd
				data[start+offset+half] = even - odd
				w *= step
			}
		}
	}
}

func cmplxAbs(value complex128) float64 {
	return math.Hypot(real(value), imag(value))
}

func staticSpectrogramColor(level float64) color.RGBA {
	level = min(max(level, 0), 1)
	stops := [...]color.RGBA{
		{R: 0, G: 0, B: 4, A: 255},
		{R: 87, G: 16, B: 110, A: 255},
		{R: 188, G: 55, B: 84, A: 255},
		{R: 249, G: 142, B: 9, A: 255},
		{R: 252, G: 255, B: 164, A: 255},
	}
	scaled := level * float64(len(stops)-1)
	index := min(int(scaled), len(stops)-2)
	fraction := scaled - float64(index)
	lerp := func(a, b uint8) uint8 {
		return uint8(float64(a) + (float64(b)-float64(a))*fraction)
	}
	return color.RGBA{
		R: lerp(stops[index].R, stops[index+1].R),
		G: lerp(stops[index].G, stops[index+1].G),
		B: lerp(stops[index].B, stops[index+1].B),
		A: 255,
	}
}
