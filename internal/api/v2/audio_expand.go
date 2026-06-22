// internal/api/v2/audio_expand.go
//
// Self-contained "audible bat playback" feature (issue #3572).
//
// Bat echolocation calls are ultrasonic and therefore inaudible in the raw
// recording. This file adds a derived, time-expanded review copy: the audio is
// slowed by a fixed factor and resampled to 48 kHz so the calls drop into the
// human hearing range (e.g. a 45 kHz call at 5x becomes ~9 kHz). The original
// clip is never modified and the AI pipeline is untouched.
//
// Everything required for the feature lives in this single file plus one route
// registration call wired from initMediaRoutes; no schema or shared-struct
// changes are needed.
package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tphakala/birdnet-go/internal/audiocore/ffmpeg"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// Time-expansion constants for audible bat playback.
const (
	// expandOutputSampleRate is the playback sample rate of the derived clip.
	expandOutputSampleRate = 48000
	// expandDefaultFactor is the default time-expansion factor.
	expandDefaultFactor = 5
	// expandProcessTimeout bounds the FFmpeg time-expansion run.
	expandProcessTimeout = 60 * time.Second
)

// expandAllowedFactors lists the fixed time-expansion factors offered to users.
// Kept as an ordered slice (for UI) plus a set (for O(1) validation).
var (
	expandAllowedFactors = []int{5, 10, 16, 20}
	expandFactorSet      = map[int]bool{5: true, 10: true, 16: true, 20: true}
)

// initAudioExpandRoutes registers the audible-bat-playback routes. It is invoked
// from initMediaRoutes after the datastore-dependent routes are confirmed
// available, so both handlers may safely dereference c.DS.
func (c *Controller) initAudioExpandRoutes() {
	// Capability info (sourceRate, gating) for the detection detail page.
	c.Echo.GET("/api/v2/audio/:id/expand", c.GetAudioExpandInfo)
	// Generate the slowed, audible review copy (protected, like /process).
	c.Echo.POST("/api/v2/audio/:id/expand", c.ExpandBatAudioByID, c.authMiddleware)
}

// AudioExpandInfo describes whether a detection supports audible bat playback
// and exposes the source sample rate without altering the detection schema.
type AudioExpandInfo struct {
	Supported     bool  `json:"supported"`
	IsBat         bool  `json:"isBat"`
	SourceRate    int   `json:"sourceRate"`
	MinSourceRate int   `json:"minSourceRate"`
	OutputRate    int   `json:"outputRate"`
	DefaultFactor int   `json:"defaultFactor"`
	Factors       []int `json:"factors"`
}

// isBatModelType reports whether the stored model type denotes a bat model.
func isBatModelType(modelType string) bool {
	return strings.EqualFold(modelType, string(entities.ModelTypeBat))
}

// GetAudioExpandInfo returns the capability metadata for the audible bat
// playback control on the detection detail page.
//
// GET /api/v2/audio/:id/expand
func (c *Controller) GetAudioExpandInfo(ctx echo.Context) error {
	if err := c.requireDatastore(ctx); err != nil {
		return err
	}

	noteID := ctx.Param("id")
	if noteID == "" {
		return c.HandleError(ctx, fmt.Errorf("missing ID"), "Note ID is required", http.StatusBadRequest)
	}

	// Default to non-bat on lookup failure (mirrors spectrogram handlers).
	modelType, mtErr := c.DS.GetNoteModelType(noteID)
	if mtErr != nil {
		c.logDebugIfEnabled("GetNoteModelType failed for expand info, treating as non-bat",
			logger.String("note_id", noteID), logger.Error(mtErr))
	}

	info := AudioExpandInfo{
		IsBat:         isBatModelType(modelType),
		MinSourceRate: ffmpeg.MinBatSampleRate,
		OutputRate:    expandOutputSampleRate,
		DefaultFactor: expandDefaultFactor,
		Factors:       expandAllowedFactors,
	}

	// Probe the source sample rate best-effort so the UI can disable the
	// control with an explanatory tooltip when the recording is below 96 kHz.
	if info.IsBat {
		if absPath, ok := c.resolveExpandSourcePath(noteID); ok {
			if sr, err := c.probeSourceSampleRate(ctx.Request().Context(), absPath); err == nil {
				info.SourceRate = sr
			} else {
				c.logDebugIfEnabled("expand info: source probe failed",
					logger.String("note_id", noteID), logger.Error(err))
			}
		}
	}

	info.Supported = info.IsBat && info.SourceRate >= ffmpeg.MinBatSampleRate
	return ctx.JSON(http.StatusOK, info)
}

// ExpandBatAudioByID produces a time-expanded, audible WAV (48 kHz) for a bat
// detection. The result is cached and the original clip is left untouched.
//
// POST /api/v2/audio/:id/expand?factor=5[&download=1]
func (c *Controller) ExpandBatAudioByID(ctx echo.Context) error {
	if err := c.requireDatastore(ctx); err != nil {
		return err
	}

	noteID := ctx.Param("id")
	if noteID == "" {
		return c.HandleError(ctx, fmt.Errorf("missing ID"), "Note ID is required", http.StatusBadRequest)
	}

	// Parse and validate the expansion factor (query param, defaults to 5).
	factor := expandDefaultFactor
	if raw := ctx.QueryParam("factor"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || !expandFactorSet[parsed] {
			return c.HandleError(ctx, fmt.Errorf("invalid factor: %q", raw),
				"Factor must be one of 5, 10, 16, 20", http.StatusBadRequest)
		}
		factor = parsed
	}

	// Gate strictly on bat model type.
	modelType, mtErr := c.DS.GetNoteModelType(noteID)
	if mtErr != nil {
		return c.HandleError(ctx, mtErr, "Failed to determine detection model type", http.StatusInternalServerError)
	}
	if !isBatModelType(modelType) {
		return c.HandleError(ctx, fmt.Errorf("model type %q is not a bat model", modelType),
			"Audible playback is only available for bat detections", http.StatusBadRequest)
	}

	absolutePath, ok := c.resolveExpandSourcePath(noteID)
	if !ok {
		return c.HandleError(ctx, fmt.Errorf("no audio file found"), "No audio clip available", http.StatusNotFound)
	}

	// Probe the source rate; reject recordings that cannot carry ultrasonic content.
	sourceRate, err := c.probeSourceSampleRate(ctx.Request().Context(), absolutePath)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to probe source audio", http.StatusInternalServerError)
	}
	if sourceRate < ffmpeg.MinBatSampleRate {
		return c.HandleError(ctx, fmt.Errorf("source rate %d below minimum %d", sourceRate, ffmpeg.MinBatSampleRate),
			fmt.Sprintf("Source recording is below %d kHz and has no ultrasonic content", ffmpeg.MinBatSampleRate/1000),
			http.StatusBadRequest)
	}

	download := ctx.QueryParam("download") == "1"
	cacheKey := expandCacheKey(noteID, sourceRate, factor)

	// Serve from cache when available.
	if c.processingCache != nil {
		if cached := c.processingCache.get(cacheKey); cached != nil {
			return c.serveExpandedWAV(ctx, cached, noteID, factor, download)
		}
	}

	// Bound concurrent FFmpeg work using the shared semaphore.
	select {
	case c.processingSemaphore <- struct{}{}:
		defer func() { <-c.processingSemaphore }()
	default:
		return c.HandleError(ctx, fmt.Errorf("processing queue full"),
			"Server busy, try again later", http.StatusServiceUnavailable)
	}

	// Write to a temp file so FFmpeg can seek back and fix the WAV header sizes.
	// Use the app-managed dir (not os.TempDir) for read-only rootfs compatibility.
	tmpDir := filepath.Join(c.SFS.BaseDir(), ".tmp-processing")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return c.HandleError(ctx, err, "Failed to create temp directory", http.StatusInternalServerError)
	}
	tmpFile, err := os.CreateTemp(tmpDir, "birdnet-expand-*.wav")
	if err != nil {
		return c.HandleError(ctx, err, "Failed to create temp file", http.StatusInternalServerError)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup

	ffmpegPath := c.currentSettings().Realtime.Audio.FfmpegPath
	if err := expandAudioToFile(ctx.Request().Context(), absolutePath, ffmpegPath, sourceRate, factor, tmpPath); err != nil {
		if ctx.Request().Context().Err() != nil {
			return nil // Client disconnected
		}
		return c.HandleError(ctx, err, "Failed to generate audible playback", http.StatusInternalServerError)
	}

	wavData, err := os.ReadFile(tmpPath)
	if err != nil {
		return c.HandleError(ctx, err, "Failed to read processed audio", http.StatusInternalServerError)
	}

	// Cache the result (non-fatal on failure).
	if c.processingCache != nil {
		if err := c.processingCache.put(cacheKey, wavData); err != nil {
			c.logAPIRequest(ctx, logger.LogLevelWarn, "Failed to cache expanded audio",
				logger.String("cache_key", cacheKey), logger.Error(err))
		}
	}

	return c.serveExpandedWAV(ctx, wavData, noteID, factor, download)
}

// serveExpandedWAV returns the WAV bytes, optionally as a downloadable attachment.
func (c *Controller) serveExpandedWAV(ctx echo.Context, data []byte, noteID string, factor int, download bool) error {
	if download {
		filename := fmt.Sprintf("detection_%s_%dx_audible.wav", expandSanitizeID(noteID), factor)
		ctx.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	}
	return ctx.Blob(http.StatusOK, MimeTypeWAV, data)
}

// resolveExpandSourcePath resolves, validates and stats the clip path for a note,
// returning the absolute on-disk path. ok is false when no usable clip exists.
func (c *Controller) resolveExpandSourcePath(noteID string) (absPath string, ok bool) {
	clipPath, err := c.DS.GetNoteClipPath(noteID)
	if err != nil || clipPath == "" {
		return "", false
	}
	normalizedPath, err := c.normalizeAndValidatePathWithLogger(clipPath, c.apiLogger)
	if err != nil {
		return "", false
	}
	if _, statErr := c.SFS.StatRel(normalizedPath); statErr != nil {
		return "", false
	}
	return filepath.Join(c.SFS.BaseDir(), normalizedPath), true
}

// probeSourceSampleRate returns the sample rate (Hz) of the given local audio
// file. ffprobe accepts a filesystem path as its input URL.
func (c *Controller) probeSourceSampleRate(ctx context.Context, absPath string) (int, error) {
	info, err := ffmpeg.ProbeStreamInfo(ctx, absPath)
	if err != nil {
		return 0, fmt.Errorf("probe source sample rate: %w", err)
	}
	return info.SampleRate, nil
}

// expandSanitizeID neutralises path-traversal characters in a note ID so it is
// safe to embed in cache filenames and download names.
func expandSanitizeID(id string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(id)
}

// expandCacheKey builds a deterministic cache filename for an expanded clip.
// The source rate is included so a re-encoded source invalidates stale entries.
func expandCacheKey(noteID string, sourceRate, factor int) string {
	return fmt.Sprintf("%s_expand_%d_%dx.wav", expandSanitizeID(noteID), sourceRate, factor)
}

// expandAudioToFile runs FFmpeg to produce the time-expanded, audible WAV at
// outputPath.
//
// The asetrate filter reinterprets the samples at sourceRate/factor, slowing
// playback and dividing all frequencies by the factor; aresample then converts
// to the standard 48 kHz playback rate without changing pitch or speed. Output
// is forced to mono 16-bit PCM. Writing to a seekable file lets FFmpeg fix the
// WAV header sizes (piping to stdout produces broken headers).
func expandAudioToFile(ctx context.Context, srcPath, ffmpegPath string, sourceRate, factor int, outputPath string) error {
	if err := ffmpeg.ValidateFFmpegPath(ffmpegPath); err != nil {
		return fmt.Errorf("invalid FFmpeg path: %w", err)
	}
	if factor <= 0 || sourceRate <= 0 {
		return fmt.Errorf("invalid expansion parameters: sourceRate=%d factor=%d", sourceRate, factor)
	}

	ctx, cancel := context.WithTimeout(ctx, expandProcessTimeout)
	defer cancel()

	newRate := sourceRate / factor
	filterChain := fmt.Sprintf("asetrate=%d,aresample=%d", newRate, expandOutputSampleRate)

	cmd := exec.CommandContext(ctx, ffmpegPath, //nolint:gosec // G204: ffmpegPath validated above; remaining args are integer-derived or app-controlled paths
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", srcPath,
		"-af", filterChain,
		"-ac", "1",
		"-c:a", "pcm_s16le",
		outputPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("audio expansion cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("audio expansion failed: %w, stderr: %s", err, stderr.String())
	}
	return nil
}
