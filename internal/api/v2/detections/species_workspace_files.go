package detections

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tphakala/birdnet-go/internal/api/v2/apicore"
	"github.com/tphakala/birdnet-go/internal/logger"
)

// spectrogramBaseRe captures the clip base name of a spectrogram render,
// "<base>_<width>px.png" or "<base>_<width>px-<suffix>.png". The greedy base
// takes the last "_<width>px"; isSpectrogramFileFor then confirms the match.
var spectrogramBaseRe = regexp.MustCompile(`^(.+)_\d+px(?:-.*)?\.png$`)

// removeClipFiles removes the audio clips and spectrogram renders of deleted
// detections. It matches removeDetectionFiles, but lists each clip directory once
// per call instead of once per clip: clip directories hold thousands of files, so
// a per-clip listing made the file cleanup of a species delete take far longer
// than the database delete itself.
func (c *Handler) removeClipFiles(clipNames []string) {
	if c.SFS == nil || len(clipNames) == 0 {
		return
	}
	clipsPrefix := c.CurrentSettings().Realtime.Audio.Export.Path
	baseDir := c.SFS.BaseDir()
	// Relative directory -> clip base names deleted from it.
	byDir := make(map[string]map[string]bool)
	clips, spectrograms := 0, 0
	for _, clipName := range clipNames {
		normalized := apicore.NormalizeClipPath(clipName, clipsPrefix)
		if normalized == "" || !filepath.IsLocal(normalized) {
			c.LogWarnIfEnabled("Refusing to remove files of an invalid clip path", logger.String("clip_name", clipName))
			continue
		}
		absClipPath := filepath.Join(baseDir, normalized)
		if err := c.SFS.Remove(absClipPath); err == nil {
			clips++
		} else if !os.IsNotExist(err) {
			c.LogWarnIfEnabled("Failed to remove audio clip file", logger.String("path", absClipPath), logger.Error(err))
		}
		dir := filepath.Dir(normalized)
		if byDir[dir] == nil {
			byDir[dir] = make(map[string]bool)
		}
		byDir[dir][strings.TrimSuffix(filepath.Base(normalized), filepath.Ext(normalized))] = true
	}
	for dir, bases := range byDir {
		entries, err := c.SFS.ReadDirRel(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				c.LogWarnIfEnabled("Failed to scan directory for spectrogram files", logger.String("dir", dir), logger.Error(err))
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			m := spectrogramBaseRe.FindStringSubmatch(name)
			if len(m) < 2 || !bases[m[1]] || !isSpectrogramFileFor(name, m[1]) {
				continue
			}
			path := filepath.Join(baseDir, dir, name)
			if err := c.SFS.Remove(path); err == nil {
				spectrograms++
			} else if !os.IsNotExist(err) {
				c.LogWarnIfEnabled("Failed to remove spectrogram file", logger.String("path", path), logger.Error(err))
			}
		}
	}
	c.LogInfoIfEnabled("Removed species workspace clip files",
		logger.Int("clips", clips), logger.Int("spectrograms", spectrograms), logger.Int("directories", len(byDir)))
}
