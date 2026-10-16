package v2

import (
	"time"

	"github.com/tphakala/birdnet-go/internal/logger"
	"gorm.io/gorm"
)

// SpeciesWorkspaceIndexName is the partial index behind the species workspace's
// best-recording and max-confidence lookups. It only covers detections with a
// clip, so the planner never chooses it for existing queries (which lack that
// predicate): a full (label_id, confidence) index measurably slowed the
// species + date search from 40 ms to 1.3 s on a 3M-row database.
const SpeciesWorkspaceIndexName = "idx_detections_label_confidence_clip"

// ensureSpeciesWorkspaceIndex creates the SQLite partial index if missing.
// Failure is logged, not returned: the workspace still works without it.
func ensureSpeciesWorkspaceIndex(db *gorm.DB, log logger.Logger) {
	start := time.Now()
	err := db.Exec("CREATE INDEX IF NOT EXISTS " + SpeciesWorkspaceIndexName +
		" ON detections (label_id, confidence DESC, id) WHERE clip_name IS NOT NULL AND clip_name <> ''").Error
	if log == nil {
		return
	}
	if err != nil {
		log.Warn("failed to create species workspace index", logger.Error(err))
		return
	}
	log.Debug("species workspace index ready", logger.Duration("duration", time.Since(start)))
}
