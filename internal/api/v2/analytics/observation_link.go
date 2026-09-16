package analytics

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/net/html"
)

var observationSpeciesPath = regexp.MustCompile(`^/species/(\d+)/$`)

// GetObservationLink resolves an exact scientific name on the fixed observation provider.
// Maps cannot be embedded: the provider sends X-Frame-Options: DENY.
func (c *Handler) GetObservationLink(ctx echo.Context) error {
	name := strings.TrimSpace(ctx.QueryParam("species"))
	if name == "" || len(name) > 200 {
		return c.HandleError(ctx, nil, "Invalid species name", http.StatusBadRequest)
	}
	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodGet,
		"https://observations.be/species/search/?q="+url.QueryEscape(name), http.NoBody)
	if err != nil {
		return c.HandleError(ctx, err, "Unable to create species lookup", http.StatusInternalServerError)
	}
	req.Header.Set("User-Agent", "BirdNET-Go species lookup")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return c.HandleError(ctx, err, "Species map lookup unavailable", http.StatusBadGateway)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return c.HandleError(ctx, nil, "Species map lookup unavailable", http.StatusBadGateway)
	}
	const maxBody = 2 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return c.HandleError(ctx, err, "Invalid species lookup response", http.StatusBadGateway)
	}
	path := observationMapPath(string(body), name)
	if path == "" {
		return c.HandleError(ctx, nil, "Species map not found", http.StatusNotFound)
	}
	return ctx.JSON(http.StatusOK, map[string]string{"url": "https://observations.be" + path})
}

func observationMapPath(body, name string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(body))
	href, scientific := "", false
	var label strings.Builder
	for {
		switch tokenizer.Next() { //nolint:exhaustive // only anchor structure and text matter
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			token := tokenizer.Token()
			if token.Data == "a" {
				href = ""
				label.Reset()
				scientific = false
			}
			for _, attr := range token.Attr {
				if token.Data == "a" && attr.Key == "href" {
					href = attr.Val
				}
				if attr.Key == "class" && strings.Contains(" "+attr.Val+" ", " species-scientific-name ") {
					scientific = true
				}
			}
		case html.TextToken:
			if scientific {
				label.Write(tokenizer.Text())
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if token.Data == "i" || token.Data == "span" {
				scientific = false
			}
			if token.Data == "a" {
				if strings.EqualFold(strings.TrimSpace(label.String()), name) && observationSpeciesPath.MatchString(href) {
					return href + "maps/"
				}
				href = ""
				label.Reset()
				scientific = false
			}
		}
	}
}
