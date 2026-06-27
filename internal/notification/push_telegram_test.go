//nolint:dupl // Table-driven tests have similar structures by design
package notification

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// telegramRequest captures a single API call received by the mock server.
type telegramRequest struct {
	path   string
	values map[string]string
}

// telegramAPIHandler is a reusable mock for the Telegram Bot API.
type telegramAPIHandler struct {
	t            *testing.T
	requests     []telegramRequest
	statusCode   int
	responseBody string
}

func newTelegramAPIHandler(t *testing.T) *telegramAPIHandler {
	t.Helper()
	return &telegramAPIHandler{
		t:            t,
		statusCode:   http.StatusOK,
		responseBody: `{"ok":true,"result":{"message_id":1}}`,
	}
}

func (h *telegramAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	assert.NoError(h.t, err)

	vals := make(map[string]string)
	for pair := range strings.SplitSeq(string(body), "&") {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			key, _ := url.QueryUnescape(kv[0])
			val, _ := url.QueryUnescape(kv[1])
			vals[key] = val
		}
	}
	h.requests = append(h.requests, telegramRequest{path: r.URL.Path, values: vals})
	w.WriteHeader(h.statusCode)
	_, _ = w.Write([]byte(h.responseBody))
}

// newShoutrrrProviderWithTelegramServer creates a ShoutrrrProvider with its
// telegramAPIBase set to the given server URL, for testing photo sending.
func newShoutrrrProviderWithTelegramServer(serverURL, shoutrrrURL string) *ShoutrrrProvider {
	p := NewShoutrrrProvider("test", true, []string{shoutrrrURL}, nil, 5*time.Second)
	p.telegramAPIBase = serverURL
	return p
}

func TestParseTelegramShoutrrrURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		urls      []string
		wantLen   int
		wantToken string
		wantChat  string
	}{
		{
			name:      "single chat",
			urls:      []string{"telegram://mytoken123@telegram?chats=-100111222333"},
			wantLen:   1,
			wantToken: "mytoken123",
			wantChat:  "-100111222333",
		},
		{
			name:      "real bot token with colon",
			urls:      []string{"telegram://123456789:AAFfGGhh@telegram?chats=-100111222333"},
			wantLen:   1,
			wantToken: "123456789:AAFfGGhh",
			wantChat:  "-100111222333",
		},
		{
			name:    "multiple chats in one URL",
			urls:    []string{"telegram://tok@telegram?chats=-100111,-100222"},
			wantLen: 2,
		},
		{
			name:    "non-telegram URL ignored",
			urls:    []string{"slack://webhook/token"},
			wantLen: 0,
		},
		{
			name:    "mixed telegram and other",
			urls:    []string{"slack://webhook/token", "telegram://tok@telegram?chats=-111"},
			wantLen: 1,
		},
		{
			name:    "empty urls",
			urls:    nil,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chats := parseTelegramShoutrrrURLs(tt.urls)
			assert.Len(t, chats, tt.wantLen)
			if tt.wantToken != "" && len(chats) > 0 {
				assert.Equal(t, tt.wantToken, chats[0].token)
				assert.Equal(t, tt.wantChat, chats[0].chatID)
			}
		})
	}
}

func TestBuildTelegramCaption(t *testing.T) {
	t.Parallel()

	t.Run("full metadata", func(t *testing.T) {
		t.Parallel()
		n := &Notification{
			Title:   "Northern Cardinal",
			Message: "First detection with 87% confidence",
			Metadata: map[string]any{
				"bg_detection_url": "http://localhost:8080/ui/detections/42",
			},
		}
		caption := buildTelegramCaption(n, "")
		assert.Contains(t, caption, "<b>Northern Cardinal</b>")
		assert.Contains(t, caption, "First detection")
		assert.Contains(t, caption, "View detection")
		assert.Contains(t, caption, "http://localhost:8080/ui/detections/42")
	})

	t.Run("no detection URL", func(t *testing.T) {
		t.Parallel()
		n := &Notification{
			Title:    "Test Bird",
			Message:  "Detected",
			Metadata: map[string]any{},
		}
		caption := buildTelegramCaption(n, "")
		assert.Contains(t, caption, "<b>Test Bird</b>")
		assert.NotContains(t, caption, "View detection")
	})

	t.Run("html special chars escaped", func(t *testing.T) {
		t.Parallel()
		n := &Notification{
			Title:    "Bird & Stuff <test>",
			Message:  "Seen at <location>",
			Metadata: map[string]any{},
		}
		caption := buildTelegramCaption(n, "")
		assert.Contains(t, caption, "&amp;")
		assert.Contains(t, caption, "&lt;")
		assert.Contains(t, caption, "&gt;")
		assert.NotContains(t, caption, "<location>")
	})

	t.Run("image URL stripped from message when photo is sent", func(t *testing.T) {
		t.Parallel()
		imgURL := "https://static.avicommons.org/norcar-12345-320.jpg"
		n := &Notification{
			Title:    "Northern Cardinal",
			Message:  "Detected with 92% confidence\n" + imgURL,
			Metadata: map[string]any{},
		}
		caption := buildTelegramCaption(n, imgURL)
		assert.NotContains(t, caption, imgURL)
		assert.Contains(t, caption, "Detected with 92% confidence")
	})

	t.Run("long caption truncated", func(t *testing.T) {
		t.Parallel()
		n := &Notification{
			Title:    strings.Repeat("A", 600),
			Message:  strings.Repeat("B", 600),
			Metadata: map[string]any{},
		}
		caption := buildTelegramCaption(n, "")
		assert.LessOrEqual(t, len([]byte(caption)), telegramCaptionMaxBytes)
	})
}

func TestExtractPublicImageURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{
			name:     "AviCommons URL returned",
			metadata: map[string]any{"bg_image_url": "https://static.avicommons.org/norcar-123-320.jpg"},
			want:     "https://static.avicommons.org/norcar-123-320.jpg",
		},
		{
			name:     "external HTTPS URL passed through",
			metadata: map[string]any{"bg_image_url": "https://example.com/bird.jpg"},
			want:     "https://example.com/bird.jpg",
		},
		{
			name:     "localhost URL rejected",
			metadata: map[string]any{"bg_image_url": "https://localhost:8080/img.jpg"},
			want:     "",
		},
		{
			name:     "http URL rejected",
			metadata: map[string]any{"bg_image_url": "http://example.com/bird.jpg"},
			want:     "",
		},
		{
			name:     "loopback IP rejected",
			metadata: map[string]any{"bg_image_url": "https://127.0.0.1/img.jpg"},
			want:     "",
		},
		{
			name:     "proxy URL rejected",
			metadata: map[string]any{"bg_image_url": "http://localhost:8080/api/v2/media/species-image?scientific_name=Turdus"},
			want:     "",
		},
		{
			name:     "missing key returns empty",
			metadata: map[string]any{},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := &Notification{Metadata: tt.metadata}
			got := extractPublicImageURL(n)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShoutrrrProvider_SendPhoto_WithTelegramURL(t *testing.T) {
	handler := newTelegramAPIHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	shoutrrrURL := "telegram://testtoken@telegram?chats=-1001234567890"
	p := newShoutrrrProviderWithTelegramServer(srv.URL, shoutrrrURL)

	n := &Notification{
		Title:   "Spotted Flycatcher",
		Message: "Detected with 81% confidence",
		Type:    TypeDetection,
		Metadata: map[string]any{
			"bg_image_url":     "https://static.avicommons.org/spofly-12345-320.jpg",
			"bg_detection_url": "http://birdnet.local/ui/detections/7",
		},
	}

	err := p.sendTelegramPhotos(t.Context(), n, extractPublicImageURL(n))
	require.NoError(t, err)
	require.Len(t, handler.requests, 1)

	req := handler.requests[0]
	assert.Contains(t, req.path, "/sendPhoto")
	assert.Contains(t, req.values["photo"], "spofly-12345")
	assert.Equal(t, "-1001234567890", req.values["chat_id"])
	assert.Equal(t, "HTML", req.values["parse_mode"])
	assert.Contains(t, req.values["caption"], "Spotted")
}

func TestShoutrrrProvider_NewShoutrrrProvider_ParsesTelegramURLs(t *testing.T) {
	t.Parallel()

	t.Run("telegram URL populates telegramChats", func(t *testing.T) {
		t.Parallel()
		p := NewShoutrrrProvider("test", true, []string{"telegram://mytoken@telegram?chats=-100111"}, nil, 0)
		require.Len(t, p.telegramChats, 1)
		assert.Equal(t, "mytoken", p.telegramChats[0].token)
		assert.Equal(t, "-100111", p.telegramChats[0].chatID)
		assert.NotNil(t, p.telegramClient)
	})

	t.Run("non-telegram URL leaves telegramChats nil", func(t *testing.T) {
		t.Parallel()
		p := NewShoutrrrProvider("test", true, []string{"slack://webhook/token"}, nil, 0)
		assert.Nil(t, p.telegramChats)
		assert.Nil(t, p.telegramClient)
	})

	t.Run("multiple chats from single URL", func(t *testing.T) {
		t.Parallel()
		p := NewShoutrrrProvider("test", true, []string{"telegram://tok@telegram?chats=-100111,-100222"}, nil, 0)
		assert.Len(t, p.telegramChats, 2)
	})
}

func TestTelegramResponse_JSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		ok    bool
		desc  string
	}{
		{name: "success", input: `{"ok":true,"result":{"message_id":1}}`, ok: true, desc: ""},
		{name: "failure", input: `{"ok":false,"description":"Unauthorized"}`, ok: false, desc: "Unauthorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var resp telegramResponse
			err := json.Unmarshal([]byte(tt.input), &resp)
			require.NoError(t, err)
			assert.Equal(t, tt.ok, resp.OK)
			assert.Equal(t, tt.desc, resp.Description)
		})
	}
}
