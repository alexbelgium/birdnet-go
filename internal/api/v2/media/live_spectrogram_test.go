package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/audiocore/buffer"
	"github.com/tphakala/birdnet-go/internal/conf"
)

func sinePCM(sampleRate int, duration time.Duration) []byte {
	sampleCount := int(duration.Seconds() * float64(sampleRate))
	pcm := make([]byte, sampleCount*pcmBytesPerSample)
	for i := range sampleCount {
		sample := int16(12000 * math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate)))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(sample))
	}
	return pcm
}

var fakeLiveSpectrogramPNG = []byte("\x89PNG\r\n\x1a\n")

func newLiveSpectrogramTestService(capture liveCaptureBuffer, run func(context.Context, string, []string, []byte) error) *liveSpectrogramService {
	return &liveSpectrogramService{
		entries: make(map[string]*liveSpectrogramCacheEntry),
		lookup: func(source string) (liveCaptureBuffer, error) {
			if source != "test-source" || capture == nil {
				return nil, buffer.ErrBufferNotFound
			}
			return capture, nil
		},
		sources: func() []string { return []string{"test-source"} },
		soxPath: func() string { return "/configured/sox" },
		run:     run,
		overlap: func() float64 { return 0 },
	}
}

func fakeLiveSpectrogramRunner(_ context.Context, _ string, args []string, _ []byte) error {
	return os.WriteFile(args[len(args)-1], fakeLiveSpectrogramPNG, 0o600)
}

type fakeLiveCapture struct {
	mu         sync.Mutex
	sampleRate int
	total      int64
	written    int
	pcm        []byte
}

func (f *fakeLiveCapture) ReadSegment(_, _ time.Time) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.pcm...), nil
}

func (f *fakeLiveCapture) SampleRate() int      { return f.sampleRate }
func (f *fakeLiveCapture) StartTime() time.Time { return time.Unix(1, 0) }
func (f *fakeLiveCapture) WrittenBytes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.written
}
func (f *fakeLiveCapture) TotalBytesWritten() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}
func TestServeLiveSpectrogram(t *testing.T) {
	const sampleRate = 8000
	pcm := sinePCM(sampleRate, liveSpectrogramDuration)
	populated := &fakeLiveCapture{sampleRate: sampleRate, total: int64(len(pcm)), written: len(pcm), pcm: pcm}
	empty := &fakeLiveCapture{sampleRate: sampleRate}

	tests := []struct {
		name       string
		capture    liveCaptureBuffer
		source     string
		wantStatus int
		wantPNG    bool
	}{
		{name: "happy path", capture: populated, source: "test-source", wantStatus: http.StatusOK, wantPNG: true},
		{name: "unknown source", capture: populated, source: "missing", wantStatus: http.StatusNotFound},
		{name: "empty buffer", capture: empty, source: "test-source", wantStatus: http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := &Handler{liveSpectrogram: newLiveSpectrogramTestService(tc.capture, fakeLiveSpectrogramRunner)}
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/api/v2/spectrogram/live?source="+tc.source, http.NoBody)
			rec := httptest.NewRecorder()
			ctx := e.NewContext(req, rec)

			require.NoError(t, handler.ServeLiveSpectrogram(ctx))
			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "no-store", rec.Header().Get(echo.HeaderCacheControl))
			if tc.wantPNG {
				assert.Equal(t, "image/png", rec.Header().Get(echo.HeaderContentType))
				assert.True(t, bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")))
			}
		})
	}
}

func TestLiveSpectrogramCacheRendersSequenceOnce(t *testing.T) {
	const sampleRate = 8000
	pcm := sinePCM(sampleRate, liveSpectrogramDuration)
	capture := &fakeLiveCapture{sampleRate: sampleRate, total: int64(len(pcm)), written: len(pcm), pcm: pcm}

	var renders atomic.Int32
	service := newLiveSpectrogramTestService(capture, func(_ context.Context, _ string, args []string, _ []byte) error {
		renders.Add(1)
		return os.WriteFile(args[len(args)-1], fakeLiveSpectrogramPNG, 0o600)
	})

	first, status := service.image(t.Context(), "test-source")
	require.Equal(t, http.StatusOK, status)
	second, status := service.image(t.Context(), "test-source")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, int32(1), renders.Load())
	assert.Equal(t, first, second)
}

func TestLiveSpectrogramConcurrentRequestsRenderOnce(t *testing.T) {
	const (
		sampleRate = 8000
		waiters    = 8
	)
	pcm := sinePCM(sampleRate, liveSpectrogramDuration)
	capture := &fakeLiveCapture{sampleRate: sampleRate, total: int64(len(pcm)), written: len(pcm), pcm: pcm}
	var renders atomic.Int32
	service := newLiveSpectrogramTestService(capture, func(_ context.Context, _ string, args []string, _ []byte) error {
		renders.Add(1)
		return os.WriteFile(args[len(args)-1], fakeLiveSpectrogramPNG, 0o600)
	})
	statuses := make(chan int, waiters)
	for range waiters {
		go func() {
			_, status := service.image(t.Context(), "test-source")
			statuses <- status
		}()
	}
	for range waiters {
		assert.Equal(t, http.StatusOK, <-statuses)
	}
	assert.Equal(t, int32(1), renders.Load())
}

func TestLiveSpectrogramSoxArguments(t *testing.T) {
	const sampleRate = 8000
	pcm := sinePCM(sampleRate, liveSpectrogramDuration)
	capture := &fakeLiveCapture{sampleRate: sampleRate, total: int64(len(pcm)), written: len(pcm), pcm: pcm}
	var gotBinary string
	var gotArgs []string
	var gotPCM []byte
	service := newLiveSpectrogramTestService(capture, func(_ context.Context, binary string, args []string, stdin []byte) error {
		gotBinary = binary
		gotArgs = append([]string(nil), args...)
		gotPCM = append([]byte(nil), stdin...)
		return os.WriteFile(args[len(args)-1], fakeLiveSpectrogramPNG, 0o600)
	})

	_, status := service.image(t.Context(), "test-source")

	require.Equal(t, http.StatusOK, status)
	require.NotEmpty(t, gotArgs)
	outputPath := gotArgs[len(gotArgs)-1]
	assert.Equal(t, "/configured/sox", gotBinary)
	assert.Equal(t, []string{
		"-V1", "-t", "raw", "-r", "8000", "-e", "signed", "-b", "16", "-c", "1", "-",
		"-n", "remix", "1", "rate", "24k", "spectrogram", "-c", "", "-o", outputPath,
	}, gotArgs)
	assert.Equal(t, pcm, gotPCM)
}

func TestLiveSpectrogramUnavailableWithoutSoxPath(t *testing.T) {
	var runs atomic.Int32
	service := newLiveSpectrogramTestService(nil, func(_ context.Context, _ string, _ []string, _ []byte) error {
		runs.Add(1)
		return nil
	})
	service.soxPath = func() string { return "" }
	handler := &Handler{liveSpectrogram: service}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, LiveSpectrogramPath, http.NoBody)
	rec := httptest.NewRecorder()

	require.NoError(t, handler.ServeLiveSpectrogram(e.NewContext(req, rec)))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Zero(t, runs.Load())
}

func TestLiveSpectrogramAuthRequiresAuthenticationWhenLiveAudioIsPrivate(t *testing.T) {
	core := apitest.NewCore(t, apitest.WithSettingsFunc(func(settings *conf.Settings) {
		settings.Security.BasicAuth.Enabled = true
		settings.Security.PublicAccess.LiveAudio = false
	}))
	core.AuthMiddleware = func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			return ctx.NoContent(http.StatusUnauthorized)
		}
	}
	handler := &Handler{Core: core}
	e := echo.New()
	e.GET("/api/v2/spectrogram/live", func(ctx echo.Context) error {
		return ctx.NoContent(http.StatusOK)
	}, handler.liveSpectrogramAuth)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/spectrogram/live", http.NoBody))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	core.AuthMiddleware = nil
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v2/spectrogram/live", http.NoBody))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// The public-access carve-out must actually let anonymous requests through:
// PublicAccess.LiveAudio is what an operator sets to publish live audio, and if
// liveSpectrogramAuth still invoked AuthMiddleware here the card would silently
// never load for the very users the setting is meant to serve.
func TestLiveSpectrogramAuthAllowsAnonymousWhenLiveAudioIsPublic(t *testing.T) {
	core := apitest.NewCore(t, apitest.WithSettingsFunc(func(settings *conf.Settings) {
		settings.Security.BasicAuth.Enabled = true
		settings.Security.PublicAccess.LiveAudio = true
	}))
	authCalled := false
	core.AuthMiddleware = func(_ echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			authCalled = true
			return ctx.NoContent(http.StatusUnauthorized)
		}
	}
	handler := &Handler{Core: core}
	e := echo.New()
	e.GET(LiveSpectrogramPath, func(ctx echo.Context) error {
		return ctx.NoContent(http.StatusOK)
	}, handler.liveSpectrogramAuth)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, LiveSpectrogramPath, http.NoBody))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, authCalled, "auth middleware must not run when live audio is public")
}
