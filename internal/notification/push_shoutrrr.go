package notification

import (
	"context"
	"io"
	"log"
	"slices"
	"strings"
	"time"

	shoutrrr "github.com/nicholas-fedor/shoutrrr"
	router "github.com/nicholas-fedor/shoutrrr/pkg/router"
	stypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
	"github.com/tphakala/birdnet-go/internal/errors"
	"github.com/tphakala/birdnet-go/internal/httpclient"
	"github.com/tphakala/birdnet-go/internal/privacy"
)

// shoutrrrDefaultTimeout is applied when no per-provider timeout is configured,
// preventing indefinite hangs on unresponsive servers.
const shoutrrrDefaultTimeout = 30 * time.Second

// ShoutrrrProvider sends via nicholas-fedor/shoutrrr
// Creates a single sender for multiple URLs.
//
// When Telegram URLs are present in the URL list, the provider automatically
// enhances delivery by using Telegram's sendPhoto API (with the bird image from
// notification metadata) instead of the plain-text sendMessage that Shoutrrr
// normally uses. This produces full-size embedded photos in Telegram chats.
// Falls back to Shoutrrr's text-only send when no image URL is available.
type ShoutrrrProvider struct {
	name              string
	enabled           bool
	urls              []string
	types             map[string]bool
	sender            *router.ServiceRouter
	nonTelegramSender *router.ServiceRouter // non-nil only in mixed Telegram+other configs
	timeout           time.Duration
	telegramChats     []parsedTelegramChat // parsed from Telegram URLs; nil for non-Telegram configs
	telegramClient    *httpclient.Client   // HTTP client for direct Telegram API calls
	// telegramAPIBase is the Telegram Bot API base URL.
	// Override in tests to point at a mock server.
	telegramAPIBase string
}

func NewShoutrrrProvider(name string, enabled bool, urls, supportedTypes []string, timeout time.Duration) *ShoutrrrProvider {
	sp := &ShoutrrrProvider{
		name:            strings.TrimSpace(name),
		enabled:         enabled,
		urls:            slices.Clone(urls),
		types:           map[string]bool{},
		timeout:         timeout,
		telegramAPIBase: telegramAPIBase,
	}
	if sp.name == "" {
		sp.name = "shoutrrr"
	}
	if len(supportedTypes) == 0 {
		sp.types["error"] = true
		sp.types["warning"] = true
		sp.types["info"] = true
		sp.types["detection"] = true
		sp.types["system"] = true
	} else {
		for _, t := range supportedTypes {
			sp.types[t] = true
		}
	}

	// Parse Telegram URLs for photo-send support.
	if chats := parseTelegramShoutrrrURLs(urls); len(chats) > 0 {
		sp.telegramChats = chats
		sp.telegramClient = newTelegramHTTPClient(timeout)
	}

	return sp
}

func (s *ShoutrrrProvider) GetName() string          { return s.name }
func (s *ShoutrrrProvider) IsEnabled() bool          { return s.enabled }
func (s *ShoutrrrProvider) SupportsType(t Type) bool { return s.types[string(t)] }

func (s *ShoutrrrProvider) ValidateConfig() error {
	if !s.enabled {
		return nil
	}
	if len(s.urls) == 0 {
		return errors.Newf("at least one URL is required").Component("notification").Category(errors.CategoryConfiguration).Build()
	}
	// Build sender to validate URLs
	sender, err := shoutrrr.CreateSender(s.urls...)
	if err != nil {
		// Wrap error to sanitize any URLs that may contain tokens/credentials
		return privacy.WrapError(err)
	}
	s.sender = sender
	// Apply configured timeout; fall back to a sane default to prevent indefinite hangs
	if s.timeout > 0 {
		s.sender.Timeout = s.timeout
	} else {
		s.sender.Timeout = shoutrrrDefaultTimeout
	}
	s.sender.SetLogger(log.New(io.Discard, "", 0))

	// For mixed Telegram+other-service configs, build a separate Shoutrrr sender
	// restricted to the non-Telegram URLs. When the Telegram photo path is taken,
	// those destinations still receive a text delivery without duplicate Telegram
	// messages.
	if len(s.telegramChats) > 0 {
		var nonTGURLs []string
		for _, u := range s.urls {
			if !strings.HasPrefix(strings.ToLower(u), "telegram://") {
				nonTGURLs = append(nonTGURLs, u)
			}
		}
		if len(nonTGURLs) > 0 {
			ntSender, ntErr := shoutrrr.CreateSender(nonTGURLs...)
			if ntErr != nil {
				return privacy.WrapError(ntErr)
			}
			ntSender.Timeout = s.sender.Timeout
			ntSender.SetLogger(log.New(io.Discard, "", 0))
			s.nonTelegramSender = ntSender
		}
	}
	return nil
}

func (s *ShoutrrrProvider) Send(ctx context.Context, n *Notification) error {
	// When Telegram URLs are configured, send a photo when an image URL is available.
	// This produces embedded photos in Telegram rather than plain-text link previews.
	if len(s.telegramChats) > 0 {
		if imgURL := extractPublicImageURL(n); imgURL != "" {
			err := s.sendTelegramPhotos(ctx, n, imgURL)
			// In mixed Telegram+other-service configs, also deliver to non-Telegram
			// destinations so they aren't silently dropped when a photo is sent.
			if s.nonTelegramSender != nil {
				if shErr := s.sendWithSender(s.nonTelegramSender, n); shErr != nil && err == nil {
					err = shErr
				}
			}
			return err
		}
	}
	// Fall back to Shoutrrr text delivery for non-Telegram URLs or when no image URL.
	return s.sendViaShoutrrr(n)
}

// sendTelegramPhotos sends a photo with caption to every configured Telegram chat.
func (s *ShoutrrrProvider) sendTelegramPhotos(ctx context.Context, n *Notification, imgURL string) error {
	if s.telegramClient == nil {
		return errors.Newf("telegram client not initialized").Component("notification").Category(errors.CategoryIntegration).Build()
	}
	caption := buildTelegramCaption(n, imgURL)
	var firstErr error
	for _, chat := range s.telegramChats {
		err := sendTelegramPhoto(ctx, s.telegramClient, s.telegramAPIBase, chat.token, chat.chatID, imgURL, caption)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// sendViaShoutrrr delivers the notification as a plain text message through the
// Shoutrrr router. Used when no image URL is available or for non-Telegram URLs.
func (s *ShoutrrrProvider) sendViaShoutrrr(n *Notification) error {
	return s.sendWithSender(s.sender, n)
}

// sendWithSender delivers a notification as plain text via the given Shoutrrr router.
func (s *ShoutrrrProvider) sendWithSender(sender *router.ServiceRouter, n *Notification) error {
	if sender == nil {
		return errors.Newf("shoutrrr sender not initialized").Component("notification").Category(errors.CategoryIntegration).Build()
	}

	body := n.Message
	params := stypes.Params{}
	if n.Title != "" {
		params.SetTitle(n.Title)
	}
	errs := sender.Send(body, &params)
	if len(errs) > 0 {
		var firstErr error
		for _, e := range errs {
			if e != nil {
				firstErr = e
				break
			}
		}
		if firstErr != nil {
			// Wrap error to sanitize any URLs that may contain tokens/credentials
			return privacy.WrapError(firstErr)
		}
	}
	return nil
}
