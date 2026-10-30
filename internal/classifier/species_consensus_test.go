package classifier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	birdNETModelID  = "BirdNET_V2.4"
	greatTitLabel   = "Parus major_Great Tit"
	blackbirdLabel  = "Turdus merula_Common Blackbird"
	greatTitSpecies = "Parus major"
)

// newBirdModelOrchestrator builds an orchestrator where Great Tit is known to
// both bird models, Blackbird only to BirdNET, and the bat model knows a bat.
func newBirdModelOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	return newTestOrchestrator(t,
		&mockModelInstance{id: birdNETModelID, labels: []string{greatTitLabel, blackbirdLabel}},
		&mockModelInstance{id: RegistryIDPerchV2, labels: []string{greatTitSpecies}},
		&mockModelInstance{id: RegistryIDBat, labels: []string{"Pipistrellus pipistrellus"}},
	)
}

func TestSpeciesSharedByBirdModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		species    string
		restrictTo []string
		minModels  int
		want       bool
	}{
		{
			name:      "species every bird model knows",
			species:   greatTitSpecies,
			minModels: 2,
			want:      true,
		},
		{
			name:      "species only one bird model knows",
			species:   "Turdus merula",
			minModels: 2,
			want:      false,
		},
		{
			name:      "bat species is known to no bird model",
			species:   "Pipistrellus pipistrellus",
			minModels: 2,
			want:      false,
		},
		{
			name:       "only one bird model runs on this source",
			species:    greatTitSpecies,
			restrictTo: []string{birdNETModelID},
			minModels:  2,
			want:       false,
		},
		{
			name:       "the bat model alone is not a quorum",
			species:    greatTitSpecies,
			restrictTo: []string{RegistryIDBat},
			minModels:  2,
			want:       false,
		},
		{
			name:       "a single model satisfies a quorum of one",
			species:    greatTitSpecies,
			restrictTo: []string{birdNETModelID},
			minModels:  1,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shared, ok := newBirdModelOrchestrator(t).SpeciesSharedByBirdModels(tt.species, tt.restrictTo, tt.minModels)

			require.True(t, ok, "support must be evaluable for a fully loaded orchestrator")
			assert.Equal(t, tt.want, shared)
		})
	}
}

// TestSpeciesSharedByBirdModels_Unevaluable covers the inputs the caller must
// treat as "cannot be evaluated reliably" and fail open on, rather than reading
// as shared=false.
func TestSpeciesSharedByBirdModels_Unevaluable(t *testing.T) {
	t.Parallel()

	t.Run("nil orchestrator", func(t *testing.T) {
		t.Parallel()
		var o *Orchestrator
		_, ok := o.SpeciesSharedByBirdModels(greatTitSpecies, nil, 2)
		assert.False(t, ok)
	})

	t.Run("empty species name", func(t *testing.T) {
		t.Parallel()
		o := newTestOrchestrator(t, &mockModelInstance{id: birdNETModelID, labels: []string{greatTitLabel}})
		_, ok := o.SpeciesSharedByBirdModels("", nil, 2)
		assert.False(t, ok)
	})

	t.Run("model mid-reload has no readable labels", func(t *testing.T) {
		t.Parallel()
		o := newTestOrchestrator(t, &mockModelInstance{id: birdNETModelID, labels: []string{greatTitLabel}})
		// An entry whose instance was torn down by a reload in flight.
		o.models[RegistryIDPerchV2] = &modelEntry{}
		_, ok := o.SpeciesSharedByBirdModels(greatTitSpecies, nil, 2)
		assert.False(t, ok)
	})
}

func TestIsBirdSpecies(t *testing.T) {
	t.Parallel()

	t.Run("a bird is class Aves", func(t *testing.T) {
		t.Parallel()
		isBird, known := IsBirdSpecies(greatTitSpecies)
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

// TestIsBirdCapableModel pins the mapping to the model-type resolution the v2
// datastore uses, so a new registry entry is classified by that rule rather than
// by an allow-list here.
func TestIsBirdCapableModel(t *testing.T) {
	t.Parallel()

	assert.False(t, IsBirdCapableModel(RegistryIDBat))
	assert.True(t, IsBirdCapableModel(birdNETModelID))
	assert.True(t, IsBirdCapableModel(RegistryIDBirdNETV3))
	assert.True(t, IsBirdCapableModel(RegistryIDPerchV2))
	assert.True(t, IsBirdCapableModel(RegistryIDBSG))
	assert.True(t, IsBirdCapableModel("unknown-model"), "an unknown ID falls back to the BirdNET default")
}

// TestIsBatCapableModel pins IsBatCapableModel as IsBirdCapableModel's exact
// complement: the two must never both report true (or both false) for the
// same ID, or a species could end up a confirming peer under neither rule, or
// under both.
func TestIsBatCapableModel(t *testing.T) {
	t.Parallel()

	ids := []string{RegistryIDBat, birdNETModelID, RegistryIDBirdNETV3, RegistryIDPerchV2, RegistryIDBSG, "unknown-model"}
	for _, id := range ids {
		assert.NotEqual(t, IsBirdCapableModel(id), IsBatCapableModel(id), "id %q", id)
	}
	assert.True(t, IsBatCapableModel(RegistryIDBat))
}

// TestSpeciesSharedByBatModels mirrors TestSpeciesSharedByBirdModels for the
// bat-capable path. The fixture orchestrator has exactly one bat model loaded,
// which is today's real topology (RegistryIDBat is a single architectural
// slot): a quorum of one is satisfiable, so a lone bat model keeps today's
// single-model behaviour, and a quorum of two is not, so the rule would apply
// the moment a second bat model became loadable without any further change
// here.
func TestSpeciesSharedByBatModels(t *testing.T) {
	t.Parallel()

	const batSpecies = "Pipistrellus pipistrellus"

	tests := []struct {
		name      string
		species   string
		minModels int
		want      bool
	}{
		{
			name:      "the lone bat model satisfies a quorum of one",
			species:   batSpecies,
			minModels: 1,
			want:      true,
		},
		{
			name:      "the lone bat model is not a quorum of two",
			species:   batSpecies,
			minModels: 2,
			want:      false,
		},
		{
			name:      "a bird species is known to no bat model",
			species:   greatTitSpecies,
			minModels: 1,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shared, ok := newBirdModelOrchestrator(t).SpeciesSharedByBatModels(tt.species, nil, tt.minModels)

			require.True(t, ok, "support must be evaluable for a fully loaded orchestrator")
			assert.Equal(t, tt.want, shared)
		})
	}
}

// TestSpeciesSharedByBatModels_SecondBatModel proves the quorum math itself
// once two bat-capable models are loaded, independent of whether today's
// catalog can produce that topology. It registers a second, temporary
// ModelRegistry entry that resolves to ModelTypeBat so the fixture can load a
// second bat-capable instance under a distinct ID, mirroring how
// TestSpeciesSharedByBirdModels already proves the >=2 case for birds.
func TestSpeciesSharedByBatModels_SecondBatModel(t *testing.T) {
	const secondBatID = "test-second-bat-model"
	ModelRegistry[secondBatID] = ModelInfo{ID: secondBatID, Name: "BattyBirdNET", DetectionName: "BattyBirdNET", DetectionVersion: "1.0"}
	t.Cleanup(func() { delete(ModelRegistry, secondBatID) })
	require.True(t, IsBatCapableModel(secondBatID), "fixture must actually resolve as bat-capable")

	const batSpecies = "Pipistrellus pipistrellus"
	o := newTestOrchestrator(t,
		&mockModelInstance{id: RegistryIDBat, labels: []string{batSpecies}},
		&mockModelInstance{id: secondBatID, labels: []string{batSpecies}},
	)

	shared, ok := o.SpeciesSharedByBatModels(batSpecies, nil, 2)
	require.True(t, ok)
	assert.True(t, shared, "two bat models sharing the species must satisfy a quorum of two")

	// One of the two not knowing the species still settles it the way a single
	// bird model that cannot predict the species does.
	o2 := newTestOrchestrator(t,
		&mockModelInstance{id: RegistryIDBat, labels: []string{batSpecies}},
		&mockModelInstance{id: secondBatID, labels: []string{"Myotis daubentonii"}},
	)
	shared, ok = o2.SpeciesSharedByBatModels(batSpecies, nil, 2)
	require.True(t, ok)
	assert.False(t, shared)
}

// TestSpeciesSharedByBatModels_Unevaluable mirrors
// TestSpeciesSharedByBirdModels_Unevaluable for the bat-capable path.
func TestSpeciesSharedByBatModels_Unevaluable(t *testing.T) {
	t.Parallel()

	t.Run("nil orchestrator", func(t *testing.T) {
		t.Parallel()
		var o *Orchestrator
		_, ok := o.SpeciesSharedByBatModels("Pipistrellus pipistrellus", nil, 1)
		assert.False(t, ok)
	})

	t.Run("empty species name", func(t *testing.T) {
		t.Parallel()
		o := newTestOrchestrator(t, &mockModelInstance{id: RegistryIDBat, labels: []string{"Pipistrellus pipistrellus"}})
		_, ok := o.SpeciesSharedByBatModels("", nil, 1)
		assert.False(t, ok)
	})

	t.Run("model mid-reload has no readable labels", func(t *testing.T) {
		t.Parallel()
		o := newTestOrchestrator(t, &mockModelInstance{id: RegistryIDBat, labels: []string{"Pipistrellus pipistrellus"}})
		// An entry whose instance was torn down by a reload in flight.
		o.models[RegistryIDBat] = &modelEntry{}
		_, ok := o.SpeciesSharedByBatModels("Pipistrellus pipistrellus", nil, 1)
		assert.False(t, ok)
	})
}
