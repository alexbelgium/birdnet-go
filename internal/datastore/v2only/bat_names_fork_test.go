package v2only

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/openfauna"
	"github.com/tphakala/birdnet-go/internal/speciesindex"
	"testing"
)

func TestHistoricBatNamesFrench(t *testing.T) {
	t.Parallel()
	r := openfauna.NewResolver()
	// Historical species need not belong to any currently loaded model.
	require.NoError(t, r.Rebuild(nil, "fr"))
	index := speciesindex.New(r)
	index.Rebuild(nil, "")
	ds := &Datastore{}
	ds.SetSpeciesIndex(index)
	assert.Equal(t, "European free-tailed bat", ds.resolveCommonName("Tadarida teniotis"))
	assert.Equal(t, "Common noctule", ds.resolveCommonName("Nyctalus noctula"))
}
