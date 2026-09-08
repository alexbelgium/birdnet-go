package reanalyze

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeClipMonoPCM16_RejectsEmptyFfmpegPath(t *testing.T) {
	t.Parallel()

	// An install with no ffmpeg configured must fail with a clear configuration
	// error, not exec an empty path and surface a confusing ENOENT.
	_, err := decodeClipMonoPCM16(t.Context(), "", bytes.NewReader(nil), 48000, 60)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg path not configured")
}

func TestDecodeClipMonoPCM16_RejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	_, err := decodeClipMonoPCM16(t.Context(), "ffmpeg", bytes.NewReader(nil), 0, 60)
	require.Error(t, err, "a zero sample rate would make the window math divide by zero")

	_, err = decodeClipMonoPCM16(t.Context(), "ffmpeg", bytes.NewReader(nil), 48000, 0)
	require.Error(t, err, "a zero duration cap would remove the bound on decode cost")
}

// writeTestWAV writes a mono 16-bit PCM WAV of the given duration at the given
// sample rate, filled with a 440 Hz sine so the decode is verifiably lossy-free
// rather than silence that would pass even if ffmpeg emitted nothing.
func writeTestWAV(t *testing.T, path string, sampleRate, durationSec int) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, testWAVBytes(sampleRate, durationSec), 0o600))
}

// openTestWAV returns a reader over a generated WAV, matching how the handler
// feeds ffmpeg: a stream on stdin, never a path.
func openTestWAV(t *testing.T, sampleRate, durationSec int) *bytes.Reader {
	t.Helper()
	return bytes.NewReader(testWAVBytes(sampleRate, durationSec))
}

func testWAVBytes(sampleRate, durationSec int) []byte {
	numSamples := sampleRate * durationSec
	dataBytes := numSamples * 2

	buf := make([]byte, 0, 44+dataBytes)
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+dataBytes))
	buf = append(buf, "WAVEfmt "...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)                   // PCM fmt chunk size
	buf = binary.LittleEndian.AppendUint16(buf, 1)                    // PCM
	buf = binary.LittleEndian.AppendUint16(buf, 1)                    // mono
	buf = binary.LittleEndian.AppendUint32(buf, uint32(sampleRate))   //nolint:gosec // test fixture
	buf = binary.LittleEndian.AppendUint32(buf, uint32(sampleRate*2)) //nolint:gosec // byte rate
	buf = binary.LittleEndian.AppendUint16(buf, 2)                    // block align
	buf = binary.LittleEndian.AppendUint16(buf, 16)                   // bits per sample
	buf = append(buf, "data"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(dataBytes)) //nolint:gosec // test fixture

	for i := range numSamples {
		v := math.Sin(2 * math.Pi * 440 * float64(i) / float64(sampleRate))
		buf = binary.LittleEndian.AppendUint16(buf, uint16(int16(v*20000))) //nolint:gosec // deliberate two's-complement reinterpretation
	}

	return buf
}

func TestDecodeClipMonoPCM16_Roundtrip(t *testing.T) {
	t.Parallel()

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH; skipping decode roundtrip")
	}

	clip := openTestWAV(t, 48000, 2)

	// Decode at a DIFFERENT rate than the source so the resample path is
	// exercised, not just a passthrough copy.
	samples, err := decodeClipMonoPCM16(t.Context(), ffmpegPath, clip, 32000, 60)
	require.NoError(t, err)

	// 2 s at 32 kHz. ffmpeg's resampler can differ by a few frames at the edges,
	// so allow a small tolerance rather than demanding an exact count.
	assert.InDelta(t, 64000, len(samples), 512)

	var peak float32
	for _, s := range samples {
		if s > peak {
			peak = s
		}
	}
	assert.Greater(t, peak, float32(0.3), "decoded audio should carry the source sine, not silence")
}

func TestDecodeClipMonoPCM16_GarbageInputIsAnError(t *testing.T) {
	t.Parallel()

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH; skipping garbage-input decode")
	}

	// Not audio: ffmpeg must fail and the error must carry its stderr, rather
	// than the caller getting an empty sample stream that looks like silence.
	_, err = decodeClipMonoPCM16(t.Context(), ffmpegPath,
		bytes.NewReader([]byte("this is not an audio file at all")), 48000, 60)
	require.Error(t, err)
}

func TestDecodeClipMonoPCM16_HonorsDurationCap(t *testing.T) {
	t.Parallel()

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH; skipping duration cap check")
	}

	clip := openTestWAV(t, 48000, 10)

	// -t 2 must truncate a 10 s clip to ~2 s of samples; without the cap an
	// oversized clip would decide how much inference the request costs.
	samples, err := decodeClipMonoPCM16(t.Context(), ffmpegPath, clip, 48000, 2)
	require.NoError(t, err)
	assert.InDelta(t, 96000, len(samples), 4096)
}
