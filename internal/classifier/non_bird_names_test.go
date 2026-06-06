package classifier

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNonBirdSpeciesResolver_KnownSpecies(t *testing.T) {
	t.Parallel()

	r := newNonBirdSpeciesResolver()

	assert.Equal(t, "Red Fox", r.Resolve("Vulpes vulpes", "en"))
	assert.Equal(t, "Common Frog", r.Resolve("Rana temporaria", "en"))
	assert.Equal(t, "American Bullfrog", r.Resolve("Lithobates catesbeianus", "en"))
	assert.Equal(t, "Field Cricket", r.Resolve("Gryllus campestris", "en"))
}

func TestNonBirdSpeciesResolver_CaseInsensitive(t *testing.T) {
	t.Parallel()

	r := newNonBirdSpeciesResolver()

	assert.Equal(t, "Red Fox", r.Resolve("vulpes vulpes", "en"))
	assert.Equal(t, "Red Fox", r.Resolve("VULPES VULPES", "en"))
	assert.Equal(t, "Red Fox", r.Resolve("Vulpes Vulpes", "en"))
}

func TestNonBirdSpeciesResolver_UnknownSpecies(t *testing.T) {
	t.Parallel()

	r := newNonBirdSpeciesResolver()

	assert.Empty(t, r.Resolve("Nonexistent species", "en"))
}

func TestNonBirdSpeciesResolver_LocaleIgnored(t *testing.T) {
	t.Parallel()

	r := newNonBirdSpeciesResolver()

	// Returns English regardless of locale parameter.
	assert.Equal(t, "Red Fox", r.Resolve("Vulpes vulpes", "de"))
	assert.Equal(t, "Red Fox", r.Resolve("Vulpes vulpes", "fr"))
}

// Compile-time check that NonBirdSpeciesResolver implements NameResolver.
var _ NameResolver = (*NonBirdSpeciesResolver)(nil)
