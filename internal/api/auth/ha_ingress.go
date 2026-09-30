// ha_ingress.go: Home Assistant ingress detection for the authentication bypass.

package auth

import (
	"net"
	"net/http"
	"os"
)

const (
	// supervisorTokenEnv is set by the Home Assistant Supervisor in every add-on
	// container, and nowhere else. Its presence is what limits the ingress
	// bypass to the add-on deployment.
	supervisorTokenEnv = "SUPERVISOR_TOKEN"

	// headerIngressPath carries the ingress URL prefix. The add-on's ingress
	// proxy always sets it, overwriting any value the client sent.
	headerIngressPath = "X-Ingress-Path"
)

// isHomeAssistantIngress reports whether r arrived through Home Assistant
// ingress, which the Supervisor has already authenticated and authorized.
//
// All three conditions must hold:
//   - the process runs as a Home Assistant add-on (SUPERVISOR_TOKEN is set);
//   - the X-Ingress-Path header is present;
//   - the TCP peer is loopback: in the add-on, ingress traffic reaches this
//     server through the add-on's own nginx on localhost, while the published
//     web port is reached from the Docker network, never from loopback.
//
// The peer is read from r.RemoteAddr, the real socket address. It must not be
// echo's RealIP, which honors X-Forwarded-For from private-network peers and so
// can be forged by any LAN client.
func isHomeAssistantIngress(r *http.Request) bool {
	if os.Getenv(supervisorTokenEnv) == "" || r.Header.Get(headerIngressPath) == "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
