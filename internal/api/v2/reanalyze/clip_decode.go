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

// maxDecodeBytes caps the PCM byte buffer read back from ffmpeg to keep memory
// bounded under hostile input. At 48 kHz mono 16-bit this is ~62 seconds of
// audio — comfortably longer than the longest detection clip a default-configured
// BirdNET-Go install saves, and longer than decodeMaxDurationSec allows anyway.
const maxDecodeBytes = 6 * 1024 * 1024

// decodeClipMonoPCM16 invokes ffmpeg to decode the clip at clipPath into raw
// signed 16-bit little-endian PCM, downmixed to mono and resampled to
// targetSampleRate, and returns it as the float32-normalised sample stream
// Orchestrator.PredictModel expects.
//
// maxDurationSec caps the input duration handed to ffmpeg (-t), which bounds both
// decode time and output size. maxDecodeBytes is enforced on the captured buffer
// as well, in case ffmpeg ignores -t for a container it cannot seek.
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
	// Settings.Realtime.Audio.FfmpegPath (validated at startup), and clipPath is
	// derived from the authenticated detection record and passed through
	// SecureFS.ValidateRelativePath — neither is freeform user input.
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
	pcm, readErr := io.ReadAll(io.LimitReader(stdout, maxDecodeBytes+1))
	exceededCap := len(pcm) > maxDecodeBytes
	if exceededCap {
		cancel()
	}
	waitErr := cmd.Wait()

	switch {
	case exceededCap:
		return nil, errors.Newf("clip decode exceeded byte cap").
			Component(errComponent).
			Category(errors.CategoryValidation).
			Context("byte_cap", maxDecodeBytes).
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
