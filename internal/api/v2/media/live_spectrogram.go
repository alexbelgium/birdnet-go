package media

import (
	"bytes"
	"encoding/binary"
	stderrors "errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
)

const (
	liveSpectrogramDuration = 3 * time.Second
	liveSpectrogramWidth    = 640
	liveSpectrogramHeight   = 192
	liveSpectrogramFFTSize  = 1024
	liveSpectrogramMaxHz    = 12000
	pcmBytesPerSample       = 2
)

var liveSpectrogramHannWindow = func() []float64 {
	window := make([]float64, liveSpectrogramFFTSize)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(liveSpectrogramFFTSize-1))
	}
	return window
}()

type liveCaptureBuffer interface {
	ReadSegment(startTime, endTime time.Time) ([]byte, error)
	SampleRate() int
	StartTime() time.Time
	WrittenBytes() int
	TotalBytesWritten() int64
}

type liveSpectrogramCacheEntry struct {
	mu        sync.Mutex
	sequence  int64
	stepBytes int64
	png       []byte
}

type liveSpectrogramService struct {
	mu      sync.Mutex
	entries map[string]*liveSpectrogramCacheEntry
	lookup  func(string) (liveCaptureBuffer, error)
	sources func() []string
	render  func([]byte, int) ([]byte, error)
	overlap func() float64
}

func newLiveSpectrogramService(core *apicore.Core) *liveSpectrogramService {
	return &liveSpectrogramService{
		entries: make(map[string]*liveSpectrogramCacheEntry),
		lookup: func(source string) (liveCaptureBuffer, error) {
			eng := core.Engine.Load()
			if eng == nil {
				return nil, errLiveAudioUnavailable
			}
			return eng.BufferManager().CaptureBuffer(source)
		},
		sources: func() []string {
			eng := core.Engine.Load()
			if eng == nil {
				return nil
			}
			health := eng.BufferManager().CaptureBufferHealthAll()
			ids := make([]string, 0, len(health))
			for _, item := range health {
				ids = append(ids, item.SourceID)
			}
			sort.Strings(ids)
			return ids
		},
		render:  renderLiveSpectrogramPNG,
		overlap: func() float64 { return core.CurrentSettings().BirdNET.Overlap },
	}
}

var errLiveAudioUnavailable = stderrors.New("live audio unavailable")

func (s *liveSpectrogramService) entry(source string) *liveSpectrogramCacheEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[source]; entry != nil {
		return entry
	}
	entry := &liveSpectrogramCacheEntry{}
	s.entries[source] = entry
	return entry
}

func (s *liveSpectrogramService) image(source string) ([]byte, int) {
	if source == "" {
		sources := s.sources()
		if len(sources) == 0 {
			return nil, http.StatusServiceUnavailable
		}
		source = sources[0]
	}

	capture, err := s.lookup(source)
	if err != nil {
		if stderrors.Is(err, errLiveAudioUnavailable) {
			return nil, http.StatusServiceUnavailable
		}
		return nil, http.StatusNotFound
	}
	sampleRate := capture.SampleRate()
	stepBytes := liveSpectrogramStepBytes(sampleRate, s.overlap())
	if stepBytes <= 0 {
		return nil, http.StatusServiceUnavailable
	}
	sequence := capture.TotalBytesWritten() / stepBytes

	entry := s.entry(source)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if len(entry.png) > 0 && entry.sequence == sequence && entry.stepBytes == stepBytes {
		return entry.png, http.StatusOK
	}

	pcm, ok := liveSpectrogramChunk(capture, sequence, stepBytes)
	if !ok {
		return nil, http.StatusServiceUnavailable
	}
	data, err := s.render(pcm, sampleRate)
	if err != nil {
		return nil, http.StatusServiceUnavailable
	}
	entry.png = data
	entry.sequence = sequence
	entry.stepBytes = stepBytes
	return data, http.StatusOK
}

func liveSpectrogramStepBytes(sampleRate int, overlap float64) int64 {
	if sampleRate <= 0 {
		return 0
	}
	if math.IsNaN(overlap) || math.IsInf(overlap, 0) {
		overlap = 0
	}
	overlap = clamp(overlap, 0, liveSpectrogramDuration.Seconds()-0.1)
	clipBytes := int64(liveSpectrogramDuration.Seconds()) * int64(sampleRate*pcmBytesPerSample)
	overlapDuration := time.Duration(overlap * float64(time.Second))
	overlapSamples := int64(overlapDuration) * int64(sampleRate) / int64(time.Second)
	return clipBytes - overlapSamples*pcmBytesPerSample
}

func liveSpectrogramChunk(capture liveCaptureBuffer, sequence, stepBytes int64) ([]byte, bool) {
	sampleRate := capture.SampleRate()
	bytesPerSecond := int64(sampleRate * pcmBytesPerSample)
	clipBytes := int64(liveSpectrogramDuration.Seconds()) * bytesPerSecond
	chunkEnd := sequence * stepBytes
	total := capture.TotalBytesWritten()
	oldest := total - int64(capture.WrittenBytes())
	if chunkEnd < clipBytes || chunkEnd > total || chunkEnd-clipBytes < oldest {
		return nil, false
	}
	start := capture.StartTime().Add(time.Duration(chunkEnd-clipBytes-oldest) * time.Second / time.Duration(bytesPerSecond))
	end := capture.StartTime().Add(time.Duration(chunkEnd-oldest) * time.Second / time.Duration(bytesPerSecond))
	pcm, err := capture.ReadSegment(start, end)
	return pcm, err == nil && int64(len(pcm)) == clipBytes
}

// ServeLiveSpectrogram returns a PNG rendering of the newest capture-buffer audio.
func (c *Handler) ServeLiveSpectrogram(ctx echo.Context) error {
	ctx.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	data, status := c.liveSpectrogram.image(ctx.QueryParam("source"))
	if status != http.StatusOK {
		return ctx.NoContent(status)
	}
	return ctx.Blob(http.StatusOK, "image/png", data)
}

// liveSpectrogramAuth applies the same hot-reloadable public-access policy as live audio.
func (c *Handler) liveSpectrogramAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		if c.CurrentSettings().Security.PublicAccess.LiveAudio {
			return next(ctx)
		}
		if c.AuthMiddleware == nil {
			return ctx.NoContent(http.StatusServiceUnavailable)
		}
		return c.AuthMiddleware(next)(ctx)
	}
}

func renderLiveSpectrogramPNG(pcm []byte, sampleRate int) ([]byte, error) {
	samples := make([]float64, len(pcm)/pcmBytesPerSample)
	for i := range samples {
		samples[i] = float64(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
	}

	img := image.NewRGBA(image.Rect(0, 0, liveSpectrogramWidth, liveSpectrogramHeight))
	window := make([]complex128, liveSpectrogramFFTSize)
	magnitudes := make([]float64, liveSpectrogramWidth*liveSpectrogramHeight)
	peakDB := -120.0
	maxStart := len(samples) - liveSpectrogramFFTSize
	maxFrequency := min(float64(liveSpectrogramMaxHz), float64(sampleRate)/2)
	topBin := int(maxFrequency * liveSpectrogramFFTSize / float64(sampleRate))

	for x := range liveSpectrogramWidth {
		start := 0
		if liveSpectrogramWidth > 1 {
			start = x * maxStart / (liveSpectrogramWidth - 1)
		}
		for i := range liveSpectrogramFFTSize {
			window[i] = complex(samples[start+i]*liveSpectrogramHannWindow[i], 0)
		}
		fft(window)
		for y := range liveSpectrogramHeight {
			bin := y * topBin / (liveSpectrogramHeight - 1)
			value := window[bin]
			magnitudeSquared := real(value)*real(value) + imag(value)*imag(value)
			db := 10 * math.Log10(magnitudeSquared+1e-24)
			magnitudes[x*liveSpectrogramHeight+y] = db
			peakDB = max(peakDB, db)
		}
	}

	for x := range liveSpectrogramWidth {
		for y := range liveSpectrogramHeight {
			db := magnitudes[x*liveSpectrogramHeight+y]
			intensity := clamp((db-(peakDB-75))/75, 0, 1)
			img.SetRGBA(x, liveSpectrogramHeight-1-y, liveSpectrogramColor(intensity))
		}
	}

	var out bytes.Buffer
	err := png.Encode(&out, img)
	return out.Bytes(), err
}

func fft(values []complex128) {
	n := len(values)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			values[i], values[j] = values[j], values[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		angle := -2 * math.Pi / float64(length)
		root := complex(math.Cos(angle), math.Sin(angle))
		for start := 0; start < n; start += length {
			factor := complex(1, 0)
			for offset := 0; offset < length/2; offset++ {
				even := values[start+offset]
				odd := values[start+offset+length/2] * factor
				values[start+offset] = even + odd
				values[start+offset+length/2] = even - odd
				factor *= root
			}
		}
	}
}

func clamp(value, low, high float64) float64 {
	return min(max(value, low), high)
}

func liveSpectrogramColor(value float64) color.RGBA {
	// Dark navy through cyan to warm yellow, with a restrained low-noise floor.
	if value < 0.5 {
		t := value * 2
		return color.RGBA{R: uint8(8 + 10*t), G: uint8(18 + 150*t), B: uint8(32 + 170*t), A: 255}
	}
	t := (value - 0.5) * 2
	return color.RGBA{R: uint8(18 + 237*t), G: uint8(168 + 75*t), B: uint8(202 - 154*t), A: 255}
}
