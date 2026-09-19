// clip_decode.go — ffmpeg-based decoder for saved clip files (wav/opus/flac/mp3/…)
// into mono float32 PCM at a model's target sample rate. Used by the reanalyze
// endpoint to feed a saved clip back through the classifier pipeline.
package reanalyze

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"

	"github.com/tphakala/birdnet-go/internal/audiocore/convert"
	"github.com/tphakala/birdnet-go/internal/errors"
)

// bytesPerDecodedSample is the width of one mono s16le sample ffmpeg writes.
const bytesPerDecodedSample = 2

// decodeByteCap returns the ceiling on the PCM buffer read back from ffmpeg for
// one decode. It is derived from the two parameters that actually determine the
// output size rather than fixed, because a fixed cap silently becomes wrong at a
// rate nobody had in mind when it was chosen: 6 MiB is ~62 s at 48 kHz but only
// ~12 s at 256 kHz, so an ultrasonic-rate decode would always trip it and fail
// with a byte-cap error that says nothing about the real cause.
//
// The doubling is slack for ffmpeg's resampler, which can emit a few frames more
// than the nominal duration; the cap is a backstop against ffmpeg ignoring -t,
// not a precise length check.
func decodeByteCap(targetSampleRate, maxDurationSec int) int {
	return targetSampleRate * maxDurationSec * bytesPerDecodedSample * 2
}

// decodeClipMonoPCM16 invokes ffmpeg to decode the audio in clip into raw signed
// 16-bit little-endian PCM, downmixed to mono and resampled to targetSampleRate,
// and returns it as the float32-normalised sample stream
// Orchestrator.PredictModel expects.
//
// clipPath is an absolute path under the media root, resolved by the caller
// through apicore.NormalizeClipPath + SecureFS.ValidateRelativePath. It is passed
// to ffmpeg as a path rather than piped to its stdin, matching the four existing
// call sites in the media domain that hand
// filepath.Join(c.SFS.BaseDir(), normalizedPath) to ffmpeg/sox for spectrogram
// generation.
//
// Piping was tried and reverted: ffmpeg's pipe protocol is non-seekable, and the
// AAC clips this project writes carry a trailing moov atom (see
// internal/audiocore/aac/encode.go — the muxer is not fast-started), so a
// full-length .m4a decodes to zero bytes with "partial file". A short one fits
// ffmpeg's probe buffer and works, which is exactly how that regression hides.
//
// maxDurationSec caps the input duration handed to ffmpeg (-t), which bounds both
// decode time and output size. A derived byte cap is enforced on the captured
// buffer as well, in case ffmpeg ignores -t for a container it cannot seek.
func decodeClipMonoPCM16(
	ctx context.Context,
	ffmpegPath, clipPath string,
	targetSampleRate, maxDurationSec int,
) ([]float32, error) {
	// Wrap the caller's context in a cancelable child so we can kill ffmpeg
	// ourselves if the stdout byte cap is hit before ffmpeg finishes writing.
	// Without this, hitting the cap leaves ffmpeg blocked on a full pipe and
	// cmd.Wait() hangs until the caller's context expires.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if ffmpegPath == "" {
		return nil, errors.Newf("ffmpeg path not configured").
			Component(errComponent).
			Category(errors.CategoryConfiguration).
			Build()
	}
	if targetSampleRate <= 0 {
		return nil, errors.Newf("invalid target sample rate: %d", targetSampleRate).
			Component(errComponent).
			Category(errors.CategoryValidation).
			Context("target_sample_rate", targetSampleRate).
			Build()
	}
	if maxDurationSec <= 0 {
		return nil, errors.Newf("invalid max duration: %d seconds", maxDurationSec).
			Component(errComponent).
			Category(errors.CategoryValidation).
			Context("max_duration_sec", maxDurationSec).
			Build()
	}

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-i", clipPath,
		"-ac", "1",
		"-ar", strconv.Itoa(targetSampleRate),
		"-f", "s16le",
		"-t", strconv.Itoa(maxDurationSec),
		"pipe:1",
	}

	//nolint:gosec // G204: ffmpegPath comes from the admin-controlled
	// Settings.Realtime.Audio.FfmpegPath, validated at startup, and clipPath is
	// derived from the authenticated detection record through
	// SecureFS.ValidateRelativePath. Neither is freeform user input.
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New(err).
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("operation", "ffmpeg_stdout_pipe").
			Build()
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.New(err).
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("operation", "ffmpeg_start").
			Build()
	}

	// Read one byte past the cap so "exceeded" is distinguishable from "exactly
	// hit". On exceed, cancel before Wait so ffmpeg dies instead of blocking on
	// a full pipe.
	byteCap := decodeByteCap(targetSampleRate, maxDurationSec)
	pcm, readErr := io.ReadAll(io.LimitReader(stdout, int64(byteCap)+1))
	exceededCap := len(pcm) > byteCap
	// Kill ffmpeg before Wait whenever we stopped reading early — on the cap, and
	// equally on a read error. Either way ffmpeg may still be writing, and Wait
	// would then block on a full stdout pipe until the request context expires,
	// holding the single reanalysis slot for the whole timeout.
	if exceededCap || readErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()

	switch {
	case exceededCap:
		return nil, errors.Newf("clip decode exceeded byte cap").
			Component(errComponent).
			Category(errors.CategoryValidation).
			Context("byte_cap", byteCap).
			Build()
	case readErr != nil:
		// Checked before waitErr: cancelling above makes Wait report a killed
		// process, which would otherwise mask the read failure that caused it.
		return nil, errors.New(readErr).
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("operation", "ffmpeg_read_stdout").
			Build()
	case waitErr != nil:
		return nil, errors.New(waitErr).
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("operation", "ffmpeg_decode").
			Context("ffmpeg_stderr", stderrBuf.String()).
			Build()
	case len(pcm) == 0:
		return nil, errors.Newf("ffmpeg produced no PCM output").
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("ffmpeg_stderr", stderrBuf.String()).
			Build()
	}

	channels, err := convert.ConvertToFloat32(pcm, 16)
	if err != nil {
		return nil, errors.New(err).
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("operation", "pcm_to_float32").
			Build()
	}
	if len(channels) == 0 {
		return nil, errors.Newf("conversion returned zero channels").
			Component(errComponent).
			Category(errors.CategoryAudio).
			Build()
	}
	return channels[0], nil
}
