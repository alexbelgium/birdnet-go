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
// The clip arrives as an io.Reader piped to ffmpeg's stdin, NOT as a path. That
// is deliberate: the caller opens it through SecureFS, whose os.Root sandbox
// refuses a symlink escaping the media root. Handing ffmpeg a plain absolute
// path instead would hand the enforcement to a process os.Root cannot constrain,
// so a symlink planted in the clips directory would be followed. Piping keeps
// the only filesystem access inside the sandbox.
//
// maxDurationSec caps the input duration handed to ffmpeg (-t), which bounds both
// decode time and output size. A derived byte cap is enforced on the captured
// buffer as well, in case ffmpeg ignores -t for a container it cannot seek.
func decodeClipMonoPCM16(
	ctx context.Context,
	ffmpegPath string,
	clip io.Reader,
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
		"-i", "pipe:0",
		"-ac", "1",
		"-ar", strconv.Itoa(targetSampleRate),
		"-f", "s16le",
		"-t", strconv.Itoa(maxDurationSec),
		"pipe:1",
	}

	//nolint:gosec // G204: ffmpegPath comes from the admin-controlled
	// Settings.Realtime.Audio.FfmpegPath, validated at startup. Every other
	// argument is a literal or an integer formatted here; the clip itself never
	// appears in the command line at all, it arrives on stdin.
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	cmd.Stdin = clip
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
	if exceededCap {
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
	case waitErr != nil:
		return nil, errors.New(waitErr).
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("operation", "ffmpeg_decode").
			Context("ffmpeg_stderr", stderrBuf.String()).
			Build()
	case readErr != nil:
		return nil, errors.New(readErr).
			Component(errComponent).
			Category(errors.CategoryAudio).
			Context("operation", "ffmpeg_read_stdout").
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
