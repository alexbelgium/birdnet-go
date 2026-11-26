package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tphakala/birdnet-go/internal/conf"
)

func TestBatSpotSettingsSection(t *testing.T) {
	t.Parallel()
	before := &conf.Settings{}
	before.Models.Enabled = []string{conf.ModelIDBatSpot}
	before.BatSpot = conf.BatSpotConfig{ModelPath: "old.onnx", LabelPath: "labels.txt", Threshold: 0.5}
	after := conf.CloneSettings(before)

	require.NoError(t, updateSettingsSection(after, conf.ModelIDBatSpot, []byte(`{"threshold":0.6}`)))

	require.NoError(t, updateSettingsSection(after, conf.ModelIDBatSpot, []byte(`{"modelPath":"new.onnx"}`)))
	// Path changes are restart-required, like BSG-BAT: they never trigger model reconciliation.
	assert.False(t, modelsEnabledChanged(before, after))
	section, err := getSettingsSection(after, conf.ModelIDBatSpot)
	require.NoError(t, err)
	assert.Equal(t, conf.BatSpotConfig{ModelPath: "new.onnx", LabelPath: "labels.txt", Threshold: 0.6}, section)
}
