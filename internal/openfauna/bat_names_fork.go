package openfauna

import (
	"strings"

	"github.com/tphakala/birdnet-go/internal/labels/bat"
)

// Some OpenFauna translations contain the scientific name as a placeholder.
// Replace only these missing bat translations, preserving every real localized
// name. Applying this at the stream gives ingestion, historic reads, reverse
// search and the generated browser dictionaries the same fallback.
func batTranslationName(scientific, common string) string {
	if common == "" || strings.EqualFold(strings.TrimSpace(common), strings.TrimSpace(scientific)) {
		if fallback := bat.CommonName(scientific); fallback != "" {
			return fallback
		}
	}
	return common
}
