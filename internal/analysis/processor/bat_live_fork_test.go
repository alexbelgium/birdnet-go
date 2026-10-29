package processor

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/classifier"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
	"github.com/tphakala/birdnet-go/internal/datastore"
	"testing"
	"time"
)

func TestBatNamesAndCrossModelMergeFrench(t *testing.T) {
	settings := conftest.GetTestSettings()
	settings.BirdNET.Locale = "fr"
	settings.Models.Enabled = nil
	conftest.SetTestSettings(settings)
	t.Cleanup(func() { conf.StoreSettings(nil) })
	bn, err := classifier.NewOrchestrator(settings)
	require.NoError(t, err)
	t.Cleanup(bn.Delete)
	require.NoError(t, bn.RebuildNameResolver([]string{"Tadarida teniotis", "Nyctalus noctula"}))
	p := mergeTestProcessor(t)
	p.Bn = bn
	for _, sci := range []string{"Tadarida teniotis", "Nyctalus noctula"} {
		t.Run(sci, func(t *testing.T) {
			p.pendingDetections = map[string]PendingDetection{}
			for i, id := range []string{classifier.RegistryIDBat, classifier.RegistryIDBSGBat} {
				item := classifier.Results{ModelID: id, Source: datastore.AudioSource{ID: mergeTestSource}}
				scientific, common, _, _, _ := p.parseAndValidateSpecies(settings, datastore.Results{Species: sci + "_" + sci, Confidence: .8}, item)
				assert.Equal(t, sci, scientific)
				assert.NotEmpty(t, common)
				assert.NotEqual(t, sci, common)
				det := detectionFor(scientific, common)
				det.Result.Confidence = .8 + float64(i)/10
				p.mergePendingHit(&item, det, time.Now(), mergeTestWindow)
			}
			pending := onlyPending(t, p)
			assert.Equal(t, 2, pending.Count)
			require.Len(t, pending.ModelContributions, 2)
			assert.Equal(t, 1, pending.ModelContributions[classifier.RegistryIDBat].HitCount)
			assert.Equal(t, 1, pending.ModelContributions[classifier.RegistryIDBSGBat].HitCount)
		})
	}
}

func TestBatModelThresholdOverridesReadCurrentSettings(t *testing.T) {
	settings := &conf.Settings{}
	settings.Bat.Threshold = .7
	for _, id := range []string{classifier.RegistryIDBat, classifier.RegistryIDBatDetect2, classifier.RegistryIDBSGBat} {
		assert.InDelta(t, .7, modelGlobalConfidenceThreshold(settings, id), 1e-6)
	}
	settings.ForkModels.BatDetect2.Threshold = .4
	settings.ForkModels.BSGBat.Threshold = .9
	assert.InDelta(t, .4, modelGlobalConfidenceThreshold(settings, classifier.RegistryIDBatDetect2), 1e-6)
	assert.InDelta(t, .9, modelGlobalConfidenceThreshold(settings, classifier.RegistryIDBSGBat), 1e-6)
	settings.ForkModels.BatDetect2.Threshold = 0
	settings.Bat.Threshold = .6
	assert.InDelta(t, .6, modelGlobalConfidenceThreshold(settings, classifier.RegistryIDBatDetect2), 1e-6)
}

func TestForkBatCandidateDiagnostics(t *testing.T) {
	t.Parallel()
	for _, id := range []string{classifier.RegistryIDBatDetect2, classifier.RegistryIDBSGBat} {
		assert.True(t, recordModelPrediction(id, .4, .7))
		assert.False(t, recordModelPrediction(id, .09, .7))
	}
	assert.False(t, recordModelPrediction(classifier.RegistryIDBat, .4, .7))
	assert.False(t, recordModelPrediction(classifier.RegistryIDBirdNETV24, .4, .7))
}
