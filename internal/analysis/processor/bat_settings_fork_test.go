package processor

import (
	"github.com/stretchr/testify/assert"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
	"testing"
)

func TestBatFamilySharedFilter(t *testing.T) {
	s := &conf.Settings{}
	for level := range 6 {
		s.Bat.FalsePositiveFilter.Level = level
		vt := precomputeVisibilityThresholds(s, 0)
		for _, id := range []string{classifier.RegistryIDBat, classifier.RegistryIDBatDetect2, classifier.RegistryIDBSGBat} {
			assert.Equal(t, calculateBatMinDetections(s), MinDetectionsForModel(s, id, 0))
			assert.Equal(t, vt.getThreshold(classifier.RegistryIDBat), vt.getThreshold(id))
		}
	}
}
func TestUltrasonicPCMRate(t *testing.T) {
	for _, id := range []string{classifier.RegistryIDBat, classifier.RegistryIDBatDetect2, classifier.RegistryIDBSGBat} {
		assert.Equal(t, classifier.ModelRegistry[id].Spec.EffectiveSampleRate(), ultrasonicPCMRate(id, 192000))
	}
	assert.Equal(t, 44100, ultrasonicPCMRate("unknown", 44100))
}
