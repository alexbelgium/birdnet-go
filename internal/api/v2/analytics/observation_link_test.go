package analytics

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestObservationMapPath(t *testing.T) {
	const results = `<a href="/species/259583/"><i class="species-scientific-name">Turdus merula merula</i></a><a href="/species/150/"><span class="species-common-name">Merle noir</span> - <i class="species-scientific-name">Turdus merula</i></a>`
	assert.Equal(t, "/species/150/maps/", observationMapPath(results, "Turdus merula"))
	assert.Empty(t, observationMapPath(results, "Turdus pilaris"))
	assert.Empty(t, observationMapPath(`<a href="https://example.com/species/150/"><i class="species-scientific-name">Turdus merula</i></a>`, "Turdus merula"))
}
