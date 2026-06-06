// non_bird_names.go provides common name resolution for non-bird species
// detected by multi-taxa models such as Perch v2. The embedded data file
// uses the same "ScientificName_CommonName" format as BirdNET label files.
package classifier

import (
	_ "embed"
	"strings"
)

//go:embed data/non_bird_species.txt
var nonBirdSpeciesData []byte

// NonBirdSpeciesResolver resolves scientific names to English common names
// for non-bird species that fall outside BirdNET's label set. It acts as
// the second fallback in the resolver chain, after BirdNETLabelResolver
// and before TaxonomyResolver.
//
// Implements NameResolver.
type NonBirdSpeciesResolver struct {
	// index maps lowercase scientific name → English common name
	index map[string]string
}

// newNonBirdSpeciesResolver creates a resolver from the embedded non-bird
// species list. Lines beginning with '#' and blank lines are ignored.
// Each data line must follow the "ScientificName_CommonName" format.
func newNonBirdSpeciesResolver() *NonBirdSpeciesResolver {
	lines := strings.Split(string(nonBirdSpeciesData), "\n")
	index := make(map[string]string, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		scientific, common := SplitSpeciesName(line)
		if scientific != "" && common != "" {
			index[strings.ToLower(scientific)] = common
		}
	}
	return &NonBirdSpeciesResolver{index: index}
}

// Resolve returns the English common name for the given scientific name.
// The locale parameter is accepted for interface compliance but unused;
// only English names are available in the embedded data.
// Returns empty string if the species is not found.
func (r *NonBirdSpeciesResolver) Resolve(scientificName, _ string) string {
	if r.index == nil {
		return ""
	}
	return r.index[strings.ToLower(scientificName)]
}
