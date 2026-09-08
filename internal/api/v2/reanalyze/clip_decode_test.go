package reanalyze

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeClipMonoPCM16_RejectsEmptyFfmpegPath(t *testing.T) {
	t.Parallel()

	// An install with no ffmpeg configured must fail with a clear configuration
	// error, not exec an empty path and surface a confusing ENOENT.
	_, err := decodeClipMonoPCM16(t.Context(), "", "/tmp/whatever.wav", 48000, 60)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg path not configured")
}

func TestDecodeClipMonoPCM16_RejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	_, err := decodeClipMonoPCM16(t.Context(), "ffmpeg", "clip.wav", 0, 60)
	require.Error(t, err, "a zero sample rate would make the window math divide by zero")

	_, err = decodeClipMonoPCM16(t.Context(), "ffmpeg", "clip.wav", 48000, 0)
	require.Error(t, err, "a zero duration cap would remove the bound on decode cost")
}

func TestDecodeByteCap_ScalesWithRateAndDuration(t *testing.T) {
	t.Parallel()

	// A fixed cap silently becomes a length limit at a rate nobody had in mind
	// when it was chosen. The cap must always leave room for the full duration
	// ffmpeg is allowed to emit at that rate.
	for _, tc := range []struct{ rate, seconds int }{
		{32000, 60}, {48000, 60}, {48000, 15}, {256000, 60},
	} {
		nominal := tc.rate * tc.seconds * bytesPerDecodedSample
		assert.Greater(t, decodeByteCap(tc.rate, tc.seconds), nominal,
			"cap must exceed the nominal output at %d Hz for %d s", tc.rate, tc.seconds)
	}
}

// requireFFmpeg skips the test when ffmpeg is unavailable.
func requireFFmpeg(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	return path
}

// writeTestWAV writes a mono 16-bit PCM WAV of the given duration and rate,
// filled with a 440 Hz sine so a decode is verifiably lossless rather than
// silence that would pass even if ffmpeg emitted nothing.
func writeTestWAV(t *testing.T, path string, sampleRate, durationSec int) {
	t.Helper()

	numSamples := sampleRate * durationSec
	dataBytes := numSamples * 2

	buf := make([]byte, 0, 44+dataBytes)
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+dataBytes)) //nolint:gosec // test fixture
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

	require.NoError(t, os.WriteFile(path, buf, 0o600))
}

func TestDecodeClipMonoPCM16_Roundtrip(t *testing.T) {
	t.Parallel()
	ffmpegPath := requireFFmpeg(t)

	clip := filepath.Join(t.TempDir(), "clip.wav")
	writeTestWAV(t, clip, 48000, 2)

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

// TestDecodeClipMonoPCM16_FullLengthAAC is the regression guard for the input
// method itself. This project can export clips as AAC, and its muxer appends the
// moov atom after the audio data rather than fast-starting the file
// (internal/audiocore/aac/encode.go), so decoding one requires ffmpeg to seek.
//
// An earlier revision piped the clip to ffmpeg's stdin. ffmpeg's pipe protocol is
// non-seekable, so a full-length .m4a decoded to zero bytes with "partial file" —
// while a SHORT one still worked, because it fits ffmpeg's probe buffer. That is
// precisely how the regression escaped a 2-second fixture, so this test uses a
// clip long enough to exceed the probe buffer.
func TestDecodeClipMonoPCM16_FullLengthAAC(t *testing.T) {
	t.Parallel()
	ffmpegPath := requireFFmpeg(t)

	dir := t.TempDir()
	wav := filepath.Join(dir, "source.wav")
	m4a := filepath.Join(dir, "clip.m4a")
	writeTestWAV(t, wav, 48000, 45)

	//nolint:gosec // G204: ffmpegPath is from exec.LookPath, the rest are test-owned temp paths
	if out, err := exec.CommandContext(t.Context(), ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-i", wav, "-c:a", "aac", "-y", m4a).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot encode AAC here: %v: %s", err, out)
	}

	// Sanity-check the fixture actually reproduces the trailing-moov layout; if
	// the encoder ever fast-starts by default this test would silently stop
	// guarding anything.
	raw, err := os.ReadFile(m4a) //nolint:gosec // test-owned temp path
	require.NoError(t, err)
	require.Greater(t, bytes.Index(raw, []byte("moov")), bytes.Index(raw, []byte("mdat")),
		"fixture must have a trailing moov atom for this test to mean anything")

	samples, err := decodeClipMonoPCM16(t.Context(), ffmpegPath, m4a, 48000, 60)
	require.NoError(t, err)
	// 45 s at 48 kHz. AAC adds encoder padding, so allow generous slack; the
	// point is that it is roughly the whole clip and emphatically not zero.
	assert.InDelta(t, 45*48000, len(samples), 48000)
}

func TestDecodeClipMonoPCM16_MissingFileIsAnError(t *testing.T) {
	t.Parallel()
	ffmpegPath := requireFFmpeg(t)

	// A clip row pointing at a file that is no longer on disk must fail loudly,
	// not return an empty sample stream that looks like silence.
	_, err := decodeClipMonoPCM16(t.Context(), ffmpegPath,
		filepath.Join(t.TempDir(), "does-not-exist.wav"), 48000, 60)
	require.Error(t, err)
}

func TestDecodeClipMonoPCM16_HonorsDurationCap(t *testing.T) {
	t.Parallel()
	ffmpegPath := requireFFmpeg(t)

	clip := filepath.Join(t.TempDir(), "long.wav")
	writeTestWAV(t, clip, 48000, 10)

	// -t 2 must truncate a 10 s clip to ~2 s of samples; without the cap an
	// oversized clip would decide how much inference the request costs.
	samples, err := decodeClipMonoPCM16(t.Context(), ffmpegPath, clip, 48000, 2)
	require.NoError(t, err)
	assert.InDelta(t, 96000, len(samples), 4096)
}
