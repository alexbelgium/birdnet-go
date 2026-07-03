package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/httpclient"
	"github.com/tphakala/birdnet-go/internal/privacy"
)

const (
	telegramAPIBase         = "https://api.telegram.org"
	telegramCaptionMaxBytes = 1024
	// maxTelegramResponseBytes caps how much of a Telegram API response we read.
	// A successful sendPhoto response is a full Message object (photo-size array,
	// chat, caption_entities, …) that exceeds the 1 KB error-body cap; truncating
	// it would make the success JSON fail to parse and be misread as a failure.
	maxTelegramResponseBytes = 64 * 1024
)

// telegramResponse is the minimal Telegram Bot API response envelope.
type telegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// parsedTelegramChat holds the resolved bot token and a single chat ID parsed
// from a Shoutrrr Telegram URL (telegram://<TOKEN>@telegram?chats=<CHAT_ID>).
type parsedTelegramChat struct {
	token  string
	chatID string
}

// parseTelegramShoutrrrURLs parses one or more Shoutrrr Telegram URLs and returns
// one parsedTelegramChat entry per (token, chatID) combination.
//
// Shoutrrr Telegram URL format: telegram://<BOT_TOKEN>@telegram?chats=<ID1>,<ID2>
// Multiple chat IDs may be comma-separated in the chats query parameter.
// Returns nil if no URLs are valid Telegram URLs.
func parseTelegramShoutrrrURLs(urls []string) []parsedTelegramChat {
	var chats []parsedTelegramChat
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if !strings.EqualFold(parsed.Scheme, "telegram") {
			continue
		}
		username := parsed.User.Username()
		if username == "" {
			continue
		}
		token := username
		if pass, hasPass := parsed.User.Password(); hasPass {
			token += ":" + pass
		}
		for chatID := range strings.SplitSeq(parsed.Query().Get("chats"), ",") {
			chatID = strings.TrimSpace(chatID)
			if chatID != "" {
				chats = append(chats, parsedTelegramChat{token: token, chatID: chatID})
			}
		}
	}
	return chats
}

// newTelegramHTTPClient creates an HTTP client suitable for Telegram API calls.
func newTelegramHTTPClient(timeout time.Duration) *httpclient.Client {
	cfg := httpclient.DefaultConfig()
	cfg.UserAgent = "BirdNET-Go-Telegram/1.0"
	if timeout > 0 {
		cfg.DefaultTimeout = timeout
	}
	return httpclient.New(&cfg)
}

// sendTelegramPhoto sends a photo to a single Telegram chat using the sendPhoto API.
// photoURL must be a publicly reachable HTTPS URL — Telegram fetches it directly.
func sendTelegramPhoto(ctx context.Context, client *httpclient.Client, apiBase, token, chatID, photoURL, caption string) error {
	return callTelegramAPI(ctx, client, apiBase, token, "sendPhoto", url.Values{
		"chat_id":                  {chatID},
		"photo":                    {photoURL},
		"caption":                  {caption},
		"parse_mode":               {"HTML"},
		"show_caption_above_media": {"true"},
	})
}

// callTelegramAPI performs a POST to the specified Telegram Bot API method.
// The bot token is kept out of any returned error messages via privacy.WrapError.
func callTelegramAPI(ctx context.Context, client *httpclient.Client, apiBase, token, method string, params url.Values) error {
	endpoint := fmt.Sprintf("%s/bot%s/%s", apiBase, token, method)
	body := strings.NewReader(params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return privacy.WrapError(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(ctx, req)
	if err != nil {
		return privacy.WrapError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	limitedBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTelegramResponseBytes))
	if err != nil {
		return privacy.WrapError(err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return &providerError{
			Err:       errors.Newf("telegram API returned HTTP %d", resp.StatusCode).Component("notification").Category(errors.CategoryIntegration).Build(),
			Retryable: true,
		}
	}
	if resp.StatusCode != http.StatusOK {
		return &providerError{
			Err:       errors.Newf("telegram API returned HTTP %d", resp.StatusCode).Component("notification").Category(errors.CategoryIntegration).Build(),
			Retryable: false,
		}
	}

	var apiResp telegramResponse
	if err := json.Unmarshal(limitedBody, &apiResp); err != nil {
		return errors.New(err).Component("notification").Category(errors.CategoryIntegration).Build()
	}
	if !apiResp.OK {
		return &providerError{
			Err:       errors.Newf("telegram API error: %s", apiResp.Description).Component("notification").Category(errors.CategoryIntegration).Build(),
			Retryable: false,
		}
	}
	return nil
}

// extractPublicImageURL returns the bg_image_url from notification metadata if it is
// a publicly reachable HTTPS URL (not a local proxy). Returns empty string otherwise.
func extractPublicImageURL(n *Notification) string {
	raw, ok := n.Metadata["bg_image_url"]
	if !ok {
		return ""
	}
	imgURL, ok := raw.(string)
	if !ok || imgURL == "" {
		return ""
	}
	if !strings.HasPrefix(imgURL, "https://") {
		return ""
	}
	parsed, err := url.Parse(imgURL)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") || strings.HasPrefix(host, "127.") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return ""
		}
	}
	return imgURL
}

// buildTelegramCaption builds an HTML-formatted photo caption from the notification.
// imageURL, when non-empty, is stripped from the message text since the photo is
// being sent directly — showing the URL as text would be redundant.
// Only the variable body text is truncated; the title and link tags are always complete.
// Capped at telegramCaptionMaxBytes to respect Telegram API limits.
func buildTelegramCaption(n *Notification, imageURL string) string {
	title := "<b>" + htmlEscape(n.Title) + "</b>"

	var link string
	if detURL, ok := n.Metadata["bg_detection_url"].(string); ok && detURL != "" {
		link = "\n<a href=\"" + htmlEscape(detURL) + "\">View detection</a>"
	}

	msg := strings.TrimSpace(n.Message)
	if imageURL != "" {
		msg = strings.TrimSpace(strings.ReplaceAll(msg, imageURL, ""))
	}

	if msg == "" {
		return title + link
	}

	escaped := htmlEscape(msg)
	// Budget for the message body: total limit minus title, "\n" separator,
	// link, and the 3-byte "…" ellipsis appended when truncating.
	budget := telegramCaptionMaxBytes - len(title) - 1 - len(link) - 3
	if budget <= 0 {
		// Fixed parts alone fill the limit; omit the body.
		return title + link
	}
	if len([]byte(escaped)) > budget {
		runes := []rune(escaped)
		for len([]byte(string(runes))) > budget && len(runes) > 0 {
			runes = runes[:len(runes)-1]
		}
		escaped = string(runes) + "…"
	}
	return title + "\n" + escaped + link
}

// htmlEscape escapes the five HTML special characters required by Telegram HTML parse mode.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
