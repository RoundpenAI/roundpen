package httpx_test

import (
	"net/http"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/httpx"
)

func TestCheckSameOrigin_Basic(t *testing.T) {
	req := func(origin, host string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, "http://"+host+"/ws", nil)
		r.Host = host
		r.RemoteAddr = "127.0.0.1:1234"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !httpx.CheckSameOrigin(req("", "127.0.0.1:9527"), "") {
		t.Fatal("empty origin allowed")
	}
	if !httpx.CheckSameOrigin(req("http://nas.local", "nas.local"), "") {
		t.Fatal("same host allowed")
	}
	if httpx.CheckSameOrigin(req("http://evil.example", "nas.local"), "") {
		t.Fatal("cross origin rejected")
	}
	if !httpx.CheckSameOrigin(req("https://console.example", "127.0.0.1:9527"), "https://console.example") {
		t.Fatal("publicURL host allowed")
	}
}

func TestCheckSameOrigin_FnosGatewayForwardedHost(t *testing.T) {
	// Browser Origin is the NAS web UI; the Unix-socket peer forwards a
	// different Host but sets X-Forwarded-Host + X-Trim-Userid.
	r, _ := http.NewRequest(http.MethodGet, "http://backend/app/roundpen/v1/agent-sessions/x/ws", nil)
	r.Host = "backend"
	r.RemoteAddr = "@" // unix abstract
	r.Header.Set("Origin", "https://nas.example:8000")
	r.Header.Set("X-Forwarded-Host", "nas.example:8000")
	r.Header.Set("X-Trim-Userid", "1000")

	if !httpx.CheckSameOrigin(r, "http://127.0.0.1:9527") {
		t.Fatal("fnOS gateway WS origin should be allowed via X-Forwarded-Host")
	}
}

func TestCheckSameOrigin_FnosSandboxedIframeNullOrigin(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://backend/ws", nil)
	r.Host = "backend"
	r.RemoteAddr = "@"
	r.Header.Set("Origin", "null")
	r.Header.Set("X-Trim-Userid", "1000")
	if !httpx.CheckSameOrigin(r, "http://127.0.0.1:9527") {
		t.Fatal("Origin null with fnOS gateway identity should be allowed")
	}

	r2, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9527/ws", nil)
	r2.Host = "127.0.0.1:9527"
	r2.RemoteAddr = "203.0.113.9:1"
	r2.Header.Set("Origin", "null")
	if httpx.CheckSameOrigin(r2, "") {
		t.Fatal("Origin null on TCP without X-Trim must be rejected")
	}
}

func TestCheckSameOrigin_FnosGatewayAnyOriginWithTrim(t *testing.T) {
	// Desktop iframe Origin host:port often disagrees with the Host the
	// gateway forwards; X-Trim is enough to trust the hop.
	r, _ := http.NewRequest(http.MethodGet, "http://backend/ws", nil)
	r.Host = "backend"
	r.RemoteAddr = "@"
	r.Header.Set("Origin", "http://10.10.1.144:5666")
	r.Header.Set("X-Trim-Userid", "1000")
	if !httpx.CheckSameOrigin(r, "") {
		t.Fatal("any Origin with X-Trim-Userid should be allowed")
	}
}

func TestCheckSameOrigin_HostPortMismatchWithoutTrim(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://10.10.1.144:9527/ws", nil)
	r.Host = "10.10.1.144:9527"
	r.RemoteAddr = "127.0.0.1:9"
	r.Header.Set("Origin", "http://10.10.1.144:5666")
	if !httpx.CheckSameOrigin(r, "") {
		t.Fatal("same hostname with differing port should be allowed")
	}
}

func TestCheckSameOrigin_ForwardedHostForgedOnTCPRejected(t *testing.T) {
	// TCP client forging X-Forwarded-Host without X-Trim (stripped on TCP)
	// must not pass when Host differs.
	r, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9527/ws", nil)
	r.Host = "127.0.0.1:9527"
	r.RemoteAddr = "203.0.113.9:9999"
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("X-Forwarded-Host", "evil.example")

	if httpx.CheckSameOrigin(r, "") {
		t.Fatal("forged X-Forwarded-Host on TCP must not allow foreign Origin")
	}
}

func TestTrust_UnixRemoteHonorsForwardedHost(t *testing.T) {
	trust, err := httpx.ParseTrust("") // empty CIDR list
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(http.MethodGet, "http://backend/v1", nil)
	r.RemoteAddr = "/var/apps/roundpen/target/app.sock"
	r.Host = "backend"
	r.Header.Set("X-Forwarded-Host", "nas.example")
	if !trust.TrustsRemote(r) {
		t.Fatal("unix peer should be trusted")
	}
	if got := trust.Host(r); got != "nas.example" {
		t.Fatalf("host=%q", got)
	}
}
