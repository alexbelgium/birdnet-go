//go:build integration

package v2

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/entities"
	"github.com/tphakala/birdnet-go/internal/datastore/v2/repository"
)

// TestMySQL_GetNewSpecies runs the lifetime-first species query on MySQL and
// MariaDB under both table layouts. It catches syntax the servers reject in its
// nested candidate subqueries, ONLY_FULL_GROUP_BY rejections and a wrong table
// name under the v2_ prefix, and checks the species-level result across models.
func TestMySQL_GetNewSpecies(t *testing.T) {
	for _, useV2Prefix := range []bool{true, false} {
		t.Run(fmt.Sprintf("useV2Prefix=%t", useV2Prefix), func(t *testing.T) {
			dropAllTables(t, coexTestDB, coexTestDBName)

			mgr := newTestMySQLManager(t, useV2Prefix)
			t.Cleanup(func() {
				_ = mgr.Delete()
				_ = mgr.Close()
			})
			require.NoError(t, mgr.Initialize())
			db := mgr.DB()

			labelType := entities.LabelType{Name: "new_species_test_species"}
			require.NoError(t, db.Create(&labelType).Error)
			modelA := entities.AIModel{Name: "NewSpeciesA", Version: "1", ModelType: entities.ModelTypeBird}
			modelB := entities.AIModel{Name: "NewSpeciesB", Version: "1", ModelType: entities.ModelTypeBird}
			require.NoError(t, db.Create(&modelA).Error)
			require.NoError(t, db.Create(&modelB).Error)

			newLabel := func(name string, modelID uint) uint {
				t.Helper()
				label := entities.Label{ScientificName: name, ModelID: modelID, LabelTypeID: labelType.ID}
				require.NoError(t, db.Create(&label).Error)
				return label.ID
			}
			newDetection := func(labelID, modelID uint, detectedAt int64) uint {
				t.Helper()
				det := entities.Detection{LabelID: labelID, ModelID: modelID, DetectedAt: detectedAt, Confidence: 0.8}
				require.NoError(t, db.Create(&det).Error)
				return det.ID
			}

			// Turdus merula: heard by model B before the window, so not new.
			newDetection(newLabel("Turdus merula", modelB.ID), modelB.ID, 100)
			newDetection(newLabel("Turdus merula", modelA.ID), modelA.ID, 1000)

			// Parus major: its only prior detection is a false positive, so it is new.
			parusA := newLabel("Parus major", modelA.ID)
			fp := newDetection(parusA, modelA.ID, 200)
			require.NoError(t, db.Create(&entities.DetectionReview{DetectionID: fp, Verified: entities.VerificationFalsePositive}).Error)
			newDetection(parusA, modelA.ID, 1500)
			newDetection(parusA, modelA.ID, 2500)
			newDetection(newLabel("Parus major", modelB.ID), modelB.ID, 8000)

			repo := repository.NewDetectionRepository(db, nil, useV2Prefix, true)
			got, err := repo.GetNewSpecies(t.Context(), 500, 5000, 100, 0)
			require.NoError(t, err)

			require.Len(t, got, 1)
			assert.Equal(t, "Parus major", got[0].ScientificName)
			assert.Equal(t, parusA, got[0].LabelID)
			assert.Equal(t, int64(1500), got[0].FirstDetected)
			assert.Equal(t, int64(8000), got[0].LastDetected)
			assert.Equal(t, 2, got[0].CountInPeriod)
		})
	}
}
