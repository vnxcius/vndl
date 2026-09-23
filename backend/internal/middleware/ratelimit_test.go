package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPPrefersCFConnectingIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.24.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 172.24.0.1")
	req.Header.Set("CF-Connecting-IP", "198.51.100.7")

	if got := ClientIP(req); got != "198.51.100.7" {
		t.Errorf("ClientIP() = %q, want CF-Connecting-IP value %q", got, "198.51.100.7")
	}
}

// Regression: with cloudflared in front of Caddy, the X-Forwarded-For
// chain's rightmost entry is cloudflared's own address, not the real
// visitor's — CF-Connecting-IP must win whenever it's present.
func TestClientIPFallsBackToXForwardedForWithoutCFHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.24.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 192.168.1.10")

	if got := ClientIP(req); got != "192.168.1.10" {
		t.Errorf("ClientIP() = %q, want rightmost X-Forwarded-For entry %q", got, "192.168.1.10")
	}
}

func TestClientIPFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.20:12345"

	if got := ClientIP(req); got != "192.168.1.20" {
		t.Errorf("ClientIP() = %q, want RemoteAddr host %q", got, "192.168.1.20")
	}
}
