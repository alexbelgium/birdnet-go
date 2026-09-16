package audio

import (
	"bytes"
	"context"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/api/v2/apitest"
	"github.com/tphakala/birdnet-go/internal/audiocore"
	"github.com/tphakala/birdnet-go/internal/audiocore/engine"
)

func TestStaticSpectrogramDurationClampsInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "missing uses default", raw: "", want: staticSpectrogramDefaultDuration},
		{name: "invalid uses default", raw: "abc", want: staticSpectrogramDefaultDuration},
		{name: "below minimum", raw: "0", want: staticSpectrogramMinDuration},
		{name: "inside range", raw: "4", want: 4 * time.Second},
		{name: "above maximum", raw: "99", want: staticSpectrogramMaxDuration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, staticSpectrogramDuration(tt.raw))
		})
	}
}

func TestStaticSpectrogramConsumerStopsAtTarget(t *testing.T) {
	t.Parallel()
	consumer := newStaticSpectrogramConsumer(48000, 4)
	require.NoError(t, consumer.Write(audiocore.AudioFrame{Data: []byte{1, 0, 2, 0, 3, 0}}))

	select {
	case <-consumer.done:
	default:
		require.Fail(t, "consumer did not signal completion")
	}
	assert.Equal(t, []int16{1, 2}, consumer.samples())
}

func TestRenderStaticSpectrogramProducesFullSizePNG(t *testing.T) {
	t.Parallel()
	const sampleRate = 48000
	samples := make([]int16, staticSpectrogramFFTSize*2)
	for i := range samples {
		samples[i] = int16(30000 * math.Sin(2*math.Pi*1000*float64(i)/sampleRate))
	}

	data, err := renderStaticSpectrogram(t.Context(), samples, sampleRate)
	require.NoError(t, err)
	decoded, err := png.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, staticSpectrogramWidth, decoded.Width)
	assert.Equal(t, staticSpectrogramHeight, decoded.Height)
}

func TestGetStaticSpectrogramReturnsNativeRatePNG(t *testing.T) {
	e := echo.New()
	core := apitest.NewCore(t, apitest.WithEcho(e))
	eng := engine.New(t.Context(), &engine.Config{}, nil)
	t.Cleanup(eng.Stop)
	core.Engine.Store(eng)

	const (
		sourceID   = "native-rate-source"
		sampleRate = 8000
	)
	_, err := eng.Registry().Register(&audiocore.SourceConfig{
		ID:               sourceID,
		DisplayName:      "Native rate source",
		Type:             audiocore.SourceTypeAudioCard,
		ConnectionString: "native-rate-device",
		SampleRate:       sampleRate,
		BitDepth:         staticSpectrogramBitDepth,
		Channels:         staticSpectrogramChannels,
	})
	require.NoError(t, err)

	handler := &Handler{Core: core}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/streams/spectrogram/"+sourceID+"?duration=1", http.NoBody)
	rec := httptest.NewRecorder()
	echoContext := e.NewContext(req, rec)
	echoContext.SetPath(StaticSpectrogramPath)
	echoContext.SetParamNames("sourceID")
	echoContext.SetParamValues(sourceID)

	result := make(chan error, 1)
	go func() {
		result <- handler.GetStaticSpectrogram(echoContext)
	}()
	require.Eventually(t, func() bool {
		return len(eng.Router().Routes(sourceID)) == 1
	}, time.Second, 10*time.Millisecond)

	ref := audiocore.NewFrameRef(func() {})
	eng.Router().Dispatch(audiocore.AudioFrame{
		SourceID:   sourceID,
		Data:       make([]byte, sampleRate*staticSpectrogramBytesPerSample),
		SampleRate: sampleRate,
		BitDepth:   staticSpectrogramBitDepth,
		Channels:   staticSpectrogramChannels,
		Timestamp:  time.Now(),
		Ref:        ref,
	})
	ref.Release()

	select {
	case handlerErr := <-result:
		require.NoError(t, handlerErr)
	case <-time.After(3 * time.Second):
		require.Fail(t, "handler did not return after receiving the requested PCM")
	}
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get(echo.HeaderContentType))
	assert.Equal(t, strconv.Itoa(sampleRate), rec.Header().Get(staticSpectrogramSampleRateHeader))
	_, decodeErr := png.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, decodeErr)
}

func TestGetStaticSpectrogramCancellationRemovesRoute(t *testing.T) {
	e := echo.New()
	core := apitest.NewCore(t, apitest.WithEcho(e))
	eng := engine.New(t.Context(), &engine.Config{}, nil)
	t.Cleanup(eng.Stop)
	core.Engine.Store(eng)

	const sourceID = "ultrasonic-source"
	_, err := eng.Registry().Register(&audiocore.SourceConfig{
		ID:               sourceID,
		DisplayName:      "Ultrasonic source",
		Type:             audiocore.SourceTypeAudioCard,
		ConnectionString: "test-device",
		SampleRate:       192000,
		BitDepth:         staticSpectrogramBitDepth,
		Channels:         staticSpectrogramChannels,
	})
	require.NoError(t, err)

	handler := &Handler{Core: core}
	requestContext, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/streams/spectrogram/"+sourceID, http.NoBody).WithContext(requestContext)
	rec := httptest.NewRecorder()
	echoContext := e.NewContext(req, rec)
	echoContext.SetPath(StaticSpectrogramPath)
	echoContext.SetParamNames("sourceID")
	echoContext.SetParamValues(sourceID)

	result := make(chan error, 1)
	go func() {
		result <- handler.GetStaticSpectrogram(echoContext)
	}()

	require.Eventually(t, func() bool {
		return len(eng.Router().Routes(sourceID)) == 1
	}, time.Second, 10*time.Millisecond)
	cancel()

	select {
	case handlerErr := <-result:
		require.ErrorIs(t, handlerErr, context.Canceled)
	case <-time.After(time.Second):
		require.Fail(t, "handler did not stop after request cancellation")
	}
	assert.Empty(t, eng.Router().Routes(sourceID))
}
