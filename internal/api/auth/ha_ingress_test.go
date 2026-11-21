// ha_ingress_test.go: Tests that the Home Assistant ingress bypass applies only
// to real ingress traffic and never to a forged header on another access path.

package auth

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/conf/conftest"
	"github.com/tphakala/birdnet-go/internal/security/securitytest"
)

const testIngressPath = "/api/hassio_ingress/token"

// TestIsAuthRequiredHomeAssistantIngress checks the bypass against the access
// paths an add-on sees, with basic auth enabled so authentication is otherwise
// required. Not parallel: t.Setenv and the global settings snapshot.
func TestIsAuthRequiredHomeAssistantIngress(t *testing.T) {
	settings := &conf.Settings{}
	settings.Security.BasicAuth.Enabled = true
	settings.Security.BasicAuth.ClientID = "admin"
	settings.Security.BasicAuth.Password = "secret"
	settings.Security.BasicAuth.AuthCodeExp = time.Minute
	conftest.SetTestSettings(settings)
	t.Cleanup(func() { conftest.SetTestSettings(nil) })

	adapter := NewSecurityAdapter(securitytest.NewOAuth2ServerForTesting(t, settings))
	e := echo.New()

	tests := []struct {
		name         string
		optedIn      bool
		remoteAddr   string
		ingressPath  string
		forwardedFor string
		wantRequired bool
	}{
		{"ingress via add-on nginx", true, "127.0.0.1:41234", testIngressPath, "172.30.32.2", false},
		{"ingress via add-on nginx over IPv6 loopback", true, "[::1]:41234", testIngressPath, "", false},
		{"direct port from LAN with forged header", true, "192.168.1.50:51000", testIngressPath, "", true},
		{"direct port with forged header and forged XFF", true, "192.168.1.50:51000", testIngressPath, "127.0.0.1", true},
		{"loopback without ingress header", true, "127.0.0.1:41234", "", "", true},
		{"option off", false, "127.0.0.1:41234", testIngressPath, "", true},
		{"unparseable peer address", true, "127.0.0.1", testIngressPath, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ingressSkipAuthEnv, strconv.FormatBool(tt.optedIn))

			req := httptest.NewRequest(http.MethodGet, "/api/v2/settings", http.NoBody)
			req.RemoteAddr = tt.remoteAddr
			if tt.ingressPath != "" {
				req.Header.Set(headerIngressPath, tt.ingressPath)
			}
			if tt.forwardedFor != "" {
				req.Header.Set(echo.HeaderXForwardedFor, tt.forwardedFor)
			}
			c := e.NewContext(req, httptest.NewRecorder())

			assert.Equal(t, tt.wantRequired, adapter.IsAuthRequired(c))
			assert.Equal(t, !tt.wantRequired, adapter.IsAuthenticated(c),
				"IsAuthenticated drives the frontend login prompt and must agree")
		})
	}
}
