package processor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchesUltrasonicReviewComment(t *testing.T) {
	t.Parallel()
	for locale := range unlikelyCommentTemplates {
		assert.True(t, MatchesUltrasonicReviewComment(FormatUltrasonicReviewComment(locale, 0.0123, 0.15)), locale)
	}
	// Past detections carry earlier wordings of the automatic comment.
	for _, template := range legacyUnlikelyCommentTemplates {
		assert.True(t, MatchesUltrasonicReviewComment(fmt.Sprintf(template, 0.045, 0.1)), template)
	}

	en := FormatUltrasonicReviewComment("en", 0.0123, 0.15)
	for _, entry := range []string{
		"",
		"Looks like a real pipistrelle to me",
		en + " Checked by hand: it is a bat.",
		"Note: " + en,
	} {
		assert.False(t, MatchesUltrasonicReviewComment(entry), entry)
	}
}
