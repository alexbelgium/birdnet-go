package processor

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
)

func TestBatFamilyMinDetectionsUsesModelCadence(t *testing.T) {
	t.Parallel()
	s := &conf.Settings{}
	for level := 0; level <= 5; level++ {
		s.Bat.FalsePositiveFilter.Level = level
		// BatDetect2 shares BattyBirdNET's 3s clip and 1.5s step.
		assert.Equal(t, calculateBatMinDetections(s), MinDetectionsForModel(s, classifier.RegistryIDBatDetect2, 0), "level %d", level)
		// BSG-BAT runs a 1s clip with a 0.5s step.
		assert.Equal(t, minDetectionsForSegment(0.5, level), MinDetectionsForModel(s, classifier.RegistryIDBSGBat, 0), "level %d", level)
		vt := precomputeVisibilityThresholds(s, 0)
		assert.Equal(t, CalculateVisibilityThreshold(minDetectionsForSegment(0.5, level)), vt.getThreshold(classifier.RegistryIDBSGBat), "level %d", level)
		assert.Equal(t, vt.getThreshold(classifier.RegistryIDBat), vt.getThreshold(classifier.RegistryIDBatDetect2), "level %d", level)
	}
}
