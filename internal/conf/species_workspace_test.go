package conf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSpeciesWorkspaceLayout(t *testing.T) {
	t.Parallel()

	t.Run("empty layout yields defaults", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, DefaultSpeciesWorkspaceLayout(), NormalizeSpeciesWorkspaceLayout(SpeciesWorkspaceLayout{}))
	})

	t.Run("keeps order, drops unknown and duplicates, forces mandatory visible", func(t *testing.T) {
		t.Parallel()
		got := NormalizeSpeciesWorkspaceLayout(SpeciesWorkspaceLayout{
			Columns: []SpeciesWorkspaceColumn{
				{ID: "lastSeen", Visible: true},
				{ID: "bogus", Visible: true},
				{ID: "species", Visible: false},
				{ID: "lastSeen", Visible: false},
				{ID: "actions", Visible: false},
			},
			Sort: SpeciesWorkspaceSort{Column: "lastSeen", Direction: "asc"},
		})
		assert.Equal(t, "lastSeen", got.Columns[0].ID)
		assert.Equal(t, SpeciesWorkspaceColumn{ID: "species", Visible: true}, got.Columns[1])
		assert.Equal(t, SpeciesWorkspaceColumn{ID: "actions", Visible: true}, got.Columns[2])
		assert.Len(t, got.Columns, len(speciesWorkspaceColumns))
		assert.Equal(t, SpeciesWorkspaceSort{Column: "lastSeen", Direction: "asc"}, got.Sort)
	})

	t.Run("invalid sort falls back", func(t *testing.T) {
		t.Parallel()
		got := NormalizeSpeciesWorkspaceLayout(SpeciesWorkspaceLayout{
			Sort: SpeciesWorkspaceSort{Column: "bestRecording", Direction: "sideways"},
		})
		assert.Equal(t, DefaultSpeciesWorkspaceLayout().Sort, got.Sort)
	})
}
