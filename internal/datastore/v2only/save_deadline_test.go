package v2only

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/datastore"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository/mocks"
	"github.com/tphakala/birdnet-go/internal/errors"
)

// TestSaveDetection_DeadlineBoundsReferenceLookups pins that a reference lookup
// stalled on an unresponsive database ends at saveDetectionTimeout, even when
// the caller's context has no deadline (the detection pipeline detaches its
// cancellation), and that the failure is marked retry safe because nothing was
// written.
func TestSaveDetection_DeadlineBoundsReferenceLookups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		models := mocks.NewMockModelRepository(t)
		models.EXPECT().
			GetOrCreate(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(ctx context.Context, _, _, _ string, _ entities.ModelType, _ *string) (*entities.AIModel, error) {
				<-ctx.Done() // a server that never answers
				return nil, ctx.Err()
			}).
			Once()
		ds := &Datastore{model: models}

		start := time.Now()
		id, err := ds.saveDetection(t.Context(), &detectionWrite{})

		require.Error(t, err)
		assert.Zero(t, id)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.True(t, errors.Is(err, datastore.ErrDetectionSaveRetrySafe), "a save that stored nothing must be retry safe")
		assert.Equal(t, saveDetectionTimeout, time.Since(start))
	})
}
