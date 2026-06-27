package api

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tphakala/birdnet-go/internal/conf"
)

// TestConfirmedStore exercises the self-contained sidecar persistence for the
// curation "confirmed" list: empty start, add/remove toggles, the on-disk file,
// and that a fresh store instance reads back the persisted state.
//
// It mutates the global conf.ConfigPath (so ResolveConfigDir points at a temp
// dir) and therefore must not run in parallel.
func TestConfirmedStore(t *testing.T) {
	tmp := t.TempDir()
	prev := conf.ConfigPath
	conf.ConfigPath = filepath.Join(tmp, "config.yaml")
	t.Cleanup(func() { conf.ConfigPath = prev })

	store := &confirmedStore{}

	// Nothing confirmed yet, and a missing sidecar is not an error.
	names, err := store.list()
	require.NoError(t, err)
	assert.Empty(t, names)

	// Adding returns the "added" action and member state.
	action, isMember, err := store.toggle("American Robin")
	require.NoError(t, err)
	assert.Equal(t, membershipActionAdded, action)
	assert.True(t, isMember)

	_, _, err = store.toggle("Blue Jay")
	require.NoError(t, err)

	names, err = store.list()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"American Robin", "Blue Jay"}, names)

	// The sidecar file is written next to config.yaml.
	assert.FileExists(t, filepath.Join(tmp, confirmedSpeciesFile))

	// Toggling an existing entry removes it.
	action, isMember, err = store.toggle("American Robin")
	require.NoError(t, err)
	assert.Equal(t, membershipActionRemoved, action)
	assert.False(t, isMember)

	// A fresh store reads the persisted state from disk.
	fresh := &confirmedStore{}
	names, err = fresh.list()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Blue Jay"}, names)
}
