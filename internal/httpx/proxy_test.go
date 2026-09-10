package httpx_test

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/httpx"
)

func TestTrustIgnoresForwardedWhenEmpty(t *testing.T) {
	trust, err := httpx.ParseTrust("")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(http.MethodGet, "http://api.example/v1", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Host = "api.example"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Forwarded-Host", "evil.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := trust.ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("client ip=%q", got)
	}
	if got := trust.Host(r); got != "api.example" {
		t.Fatalf("host=%q", got)
	}
	if got := trust.Scheme(r); got != "http" {
		t.Fatalf("scheme=%q", got)
	}
}

func TestTrustHonorsForwardedFromCIDR(t *testing.T) {
	trust, err := httpx.ParseTrust("127.0.0.1,10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(http.MethodGet, "http://api.example/v1", nil)
	r.RemoteAddr = "10.1.2.3:80"
	r.Host = "api.example"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 10.1.2.3")
	r.Header.Set("X-Forwarded-Host", "edge.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := trust.ClientIP(r); got != "198.51.100.7" {
		t.Fatalf("client ip=%q", got)
	}
	if got := trust.Host(r); got != "edge.example" {
		t.Fatalf("host=%q", got)
	}
	if got := trust.Scheme(r); got != "https" {
		t.Fatalf("scheme=%q", got)
	}
}

func TestTrustTLSWithoutForwarded(t *testing.T) {
	trust, err := httpx.ParseTrust("")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := http.NewRequest(http.MethodGet, "https://api.example/v1", nil)
	r.TLS = &tls.ConnectionState{}
	if got := trust.Scheme(r); got != "https" {
		t.Fatalf("scheme=%q", got)
	}
}
