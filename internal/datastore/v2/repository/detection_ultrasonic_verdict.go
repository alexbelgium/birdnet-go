package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/errors"
)

// SetUltrasonicVerdict stores a re-run ultrasonic filter verdict on a saved detection,
// in one transaction, using only the existing columns:
//   - the unlikely flag is set to the verdict;
//   - comments for which isAuto reports true are the filter's automatic comments.
//     An unlikely verdict keeps exactly one of them, with its text set to comment
//     (one is added when none exists); a probable verdict removes them all.
//
// Other comments are never touched. A locked detection is refused with
// ErrDetectionLocked. changed reports whether anything was written.
//
// It is not part of the DetectionRepository interface on purpose: callers
// type-assert for it, which keeps this addition in its own file.
func (r *detectionRepository) SetUltrasonicVerdict(ctx context.Context, id uint, unlikely bool, comment string, isAuto func(string) bool) (changed bool, err error) {
	if isAuto == nil {
		return false, errors.NewStd("ultrasonic verdict: nil comment matcher")
	}
	err = datastore.RetryTransactionOnLock(ctx, r.db, "v2_set_ultrasonic_verdict", func(tx *gorm.DB) error {
		changed = false // a retried attempt starts over

		// Lock the detection row first (FOR UPDATE on MySQL/MariaDB; SQLite has a
		// single writer). Keep takes a share lock on this row before inserting its
		// lock row, and a second re-run waits here, so the lock check and the
		// comment reads below see every commit made before this one.
		var det entities.Detection
		if err := tx.Table(r.tableName()).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Select("id", "unlikely").Where("id = ?", id).Take(&det).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDetectionNotFound
			}
			return err
		}
		var locks int64
		if err := tx.Table(r.locksTable()).Where("detection_id = ?", id).Count(&locks).Error; err != nil {
			return err
		}
		if locks != 0 {
			return ErrDetectionLocked
		}

		if det.Unlikely != unlikely {
			if err := tx.Table(r.tableName()).Where("id = ?", id).Update("unlikely", unlikely).Error; err != nil {
				return err
			}
			changed = true
		}

		var comments []entities.DetectionComment
		// Lock the comment rows too, so a comment edited meanwhile is read as
		// committed and not overwritten from a stale read.
		if err := tx.Table(r.commentsTable()).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Where("detection_id = ?", id).Order("id").Find(&comments).Error; err != nil {
			return err
		}
		kept := false
		for i := range comments {
			c := &comments[i]
			if !isAuto(c.Entry) {
				continue
			}
			if unlikely && !kept {
				kept = true
				if c.Entry != comment {
					if err := tx.Table(r.commentsTable()).Where("id = ?", c.ID).
						Updates(map[string]any{"entry": comment, "updated_at": time.Now()}).Error; err != nil {
						return err
					}
					changed = true
				}
				continue
			}
			if err := tx.Table(r.commentsTable()).Where("id = ?", c.ID).Delete(&entities.DetectionComment{}).Error; err != nil {
				return err
			}
			changed = true
		}
		if unlikely && !kept {
			if err := tx.Table(r.commentsTable()).Create(&entities.DetectionComment{DetectionID: id, Entry: comment}).Error; err != nil {
				return err
			}
			changed = true
		}
		return nil
	}, r.metrics)
	if err != nil {
		return false, err // rolled back: nothing was written
	}
	return changed, nil
}
