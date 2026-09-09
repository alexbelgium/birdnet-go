package classifier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// birdModel builds a mock entry whose label set is exactly labels.
func birdModel(id string, labels ...string) *modelEntry {
	return &modelEntry{instance: &mockModelInstance{id: id, labels: labels}}
}

func TestBirdModelSpeciesSupport(t *testing.T) {
	t.Parallel()

	// Great Tit is known to both bird models, Blackbird only to BirdNET. The bat
	// model knows a bat and must never be counted.
	newOrch := func() *Orchestrator {
		return &Orchestrator{
			models: map[string]*modelEntry{
				"BirdNET_V2.4":    birdModel("BirdNET_V2.4", "Parus major_Great Tit", "Turdus merula_Common Blackbird"),
				RegistryIDPerchV2: birdModel(RegistryIDPerchV2, "Parus major"),
				RegistryIDBat:     birdModel(RegistryIDBat, "Pipistrellus pipistrellus"),
			},
		}
	}

	tests := []struct {
		name           string
		species        string
		restrictTo     map[string]struct{}
		wantRelevant   int
		wantSupporting int
	}{
		{
			name:           "species shared by every bird model",
			species:        "Parus major",
			wantRelevant:   2,
			wantSupporting: 2,
		},
		{
			name:           "species only one bird model knows",
			species:        "Turdus merula",
			wantRelevant:   2,
			wantSupporting: 1,
		},
		{
			name:           "bat species is not covered by any bird model",
			species:        "Pipistrellus pipistrellus",
			wantRelevant:   2,
			wantSupporting: 0,
		},
		{
			name:           "restricted to a single source model",
			species:        "Parus major",
			restrictTo:     map[string]struct{}{"BirdNET_V2.4": {}},
			wantRelevant:   1,
			wantSupporting: 1,
		},
		{
			name:           "restriction naming only the bat model yields nothing",
			species:        "Parus major",
			restrictTo:     map[string]struct{}{RegistryIDBat: {}},
			wantRelevant:   0,
			wantSupporting: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			relevant, supporting, ok := newOrch().BirdModelSpeciesSupport(tt.species, tt.restrictTo)

			require.True(t, ok, "support must be evaluable for a fully loaded orchestrator")
			assert.Equal(t, tt.wantRelevant, relevant)
			assert.Equal(t, tt.wantSupporting, supporting)
		})
	}
}

// TestBirdModelSpeciesSupport_Unevaluable covers the inputs the caller must treat
// as "cannot be evaluated reliably" and fail open on, rather than reading as a
// count of zero.
func TestBirdModelSpeciesSupport_Unevaluable(t *testing.T) {
	t.Parallel()

	t.Run("nil orchestrator", func(t *testing.T) {
		t.Parallel()
		var o *Orchestrator
		_, _, ok := o.BirdModelSpeciesSupport("Parus major", nil)
		assert.False(t, ok)
	})

	t.Run("empty species name", func(t *testing.T) {
		t.Parallel()
		o := &Orchestrator{models: map[string]*modelEntry{"BirdNET_V2.4": birdModel("BirdNET_V2.4", "Parus major")}}
		_, _, ok := o.BirdModelSpeciesSupport("", nil)
		assert.False(t, ok)
	})

	t.Run("model mid-reload has no readable labels", func(t *testing.T) {
		t.Parallel()
		o := &Orchestrator{models: map[string]*modelEntry{
			"BirdNET_V2.4":    birdModel("BirdNET_V2.4", "Parus major"),
			RegistryIDPerchV2: {}, // instance nil, as during a reload
		}}
		_, _, ok := o.BirdModelSpeciesSupport("Parus major", nil)
		assert.False(t, ok)
	})
}

func TestIsBirdSpecies(t *testing.T) {
	t.Parallel()

	t.Run("a bird is class Aves", func(t *testing.T) {
		t.Parallel()
		isBird, known := IsBirdSpecies("Parus major")
		assert.True(t, known)
		assert.True(t, isBird)
	})

	t.Run("an uncatalogued name is not evaluable", func(t *testing.T) {
		t.Parallel()
		isBird, known := IsBirdSpecies("Nonexistentus fabricatus")
		assert.False(t, known)
		assert.False(t, isBird)
	})

	t.Run("empty name is not evaluable", func(t *testing.T) {
		t.Parallel()
		isBird, known := IsBirdSpecies("")
		assert.False(t, known)
		assert.False(t, isBird)
	})
}

func TestIsBirdCapableModel(t *testing.T) {
	t.Parallel()

	assert.False(t, isBirdCapableModel(RegistryIDBat))
	assert.True(t, isBirdCapableModel(RegistryIDPerchV2))
	assert.True(t, isBirdCapableModel("BirdNET_V2.4"))
	assert.True(t, isBirdCapableModel(RegistryIDBSG))
}
