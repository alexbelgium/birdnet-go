package media

import (
	"bytes"
	"context"
	stderrors "errors"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/spectrogram"
)

// LiveSpectrogramPath is the route pattern, relative to the v2 API group, for
// the live spectrogram PNG. It is exported so the facade's isPrivateModeExempt
// allow-list composes the same constant used at the registration site below and
// cannot drift from it, mirroring how the audio domain exports its HLS paths.
const LiveSpectrogramPath = "/spectrogram/live"

const (
	liveSpectrogramDuration = 3 * time.Second
	// liveSpectrogramWidth matches the width the detections UI requests
	// (AudioPlayer's default size=md), so the live card carries the same
	// pixel budget per refresh as a saved-detection spectrogram. Height is
	// derived from it, keeping the 2:1 ratio the detection player renders at.
	liveSpectrogramWidth   = apicore.SpectrogramSizeMd
	liveSpectrogramTimeout = 5 * time.Second
	pcmBytesPerSample      = 2
)

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
	mu       sync.Mutex
	entries  map[string]*liveSpectrogramCacheEntry
	lookup   func(string) (liveCaptureBuffer, error)
	sources  func() []string
	soxPath  func() string
	run      func(context.Context, string, []string, []byte) error
	overlap  func() float64
	settings func() *conf.Settings
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
		soxPath:  func() string { return core.CurrentSettings().Realtime.Audio.SoxPath },
		run:      runLiveSpectrogramCommand,
		overlap:  func() float64 { return core.CurrentSettings().BirdNET.Overlap },
		settings: core.CurrentSettings,
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

func (s *liveSpectrogramService) image(ctx context.Context, source string) ([]byte, int) {
	soxPath := s.soxPath()
	if soxPath == "" {
		return nil, http.StatusServiceUnavailable
	}
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
	data, err := s.render(ctx, soxPath, pcm, sampleRate, s.settings())
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
	data, status := c.liveSpectrogram.image(ctx.Request().Context(), ctx.QueryParam("source"))
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

func clamp(value, low, high float64) float64 {
	return min(max(value, low), high)
}

func (s *liveSpectrogramService) render(ctx context.Context, soxPath string, pcm []byte, sampleRate int, settings *conf.Settings) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "birdnet-go-live-spectrogram-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	outputPath := filepath.Join(tempDir, "spectrogram.png")
	args := []string{
		"-V1", "-t", "raw", "-r", strconv.Itoa(sampleRate), "-e", "signed", "-b", "16", "-c", "1", "-",
		"-n", "remix", "1", "rate", "24000", "spectrogram",
		"-x", strconv.Itoa(liveSpectrogramWidth), "-y", strconv.Itoa(spectrogram.SoxFriendlyHeight(liveSpectrogramWidth)),
		"-d", strconv.Itoa(int(liveSpectrogramDuration.Seconds())),
		"-z", spectrogram.SoxDynamicRange(settings), "-o", outputPath, "-r",
	}
	args = append(args, spectrogram.SoxStyleArgs(settings.Realtime.Dashboard.Spectrogram.Style)...)
	renderCtx, cancel := context.WithTimeout(ctx, liveSpectrogramTimeout)
	defer cancel()
	if err := s.run(renderCtx, soxPath, args, pcm); err != nil {
		return nil, err
	}
	return os.ReadFile(outputPath)
}

func runLiveSpectrogramCommand(ctx context.Context, binary string, args []string, pcm []byte) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, binary, args...) //nolint:gosec // binary is the startup-validated configured SoX path
	} else {
		cmd = exec.CommandContext(ctx, "nice", append([]string{"-n", "19", binary}, args...)...) //nolint:gosec // binary is the startup-validated configured SoX path
	}
	cmd.Stdin = bytes.NewReader(pcm)
	return cmd.Run()
}
