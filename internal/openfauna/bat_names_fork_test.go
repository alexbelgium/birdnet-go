package openfauna

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBatNamesFrenchFallbackAcrossResolvers(t *testing.T) {
	t.Parallel()
	names := []string{"Tadarida teniotis", "Nyctalus noctula", "Barbastella barbastellus"}
	want := map[string]string{"Tadarida teniotis": "European free-tailed bat", "Nyctalus noctula": "Common noctule", "Barbastella barbastellus": "Barbastelle d'Europe"}
	r := NewResolver()
	require.NoError(t, r.Rebuild(names, "fr"))
	dict, err := BuildLocaleDictionary("fr")
	require.NoError(t, err)
	batch := LookupCommonNames(names, "fr")
	for _, sci := range names {
		assert.Equal(t, want[sci], r.Resolve(sci, ""))
		assert.Equal(t, want[sci], batch[sci])
		assert.Equal(t, want[sci], dict[sci])
		local, ok := r.ResolveLocal(sci)
		assert.True(t, ok)
		assert.Equal(t, want[sci], local)
	}
	reverse := LookupScientificNames([]string{want[names[0]], want[names[1]]}, "fr")
	assert.Contains(t, reverse[want[names[0]]], names[0])
	assert.Contains(t, reverse[want[names[1]]], names[1])
	// Historic species outside the working set use the same fallback.
	require.NoError(t, r.Rebuild(nil, "fr"))
	assert.Equal(t, want[names[0]], r.Resolve(names[0], ""))
}
