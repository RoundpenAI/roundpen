package httpx_test

import (
	"net/http"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/httpx"
)

func TestParseConsoleOrigin(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"http://127.0.0.1:19000", "http://127.0.0.1:19000", true},
		{"http://192.168.1.8:9527/settings", "http://192.168.1.8:9527", true},
		{"https://app.example.com", "https://app.example.com", true},
		{"http://app.example.com", "", false},
		{"https://user:pass@evil.test", "", false},
		{"", "", false},
		{"not-a-url", "", false},
	}
	for _, tc := range cases {
		got, err := httpx.ParseConsoleOrigin(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("ParseConsoleOrigin(%q)=%q,%v want %q", tc.in, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("ParseConsoleOrigin(%q) unexpectedly ok: %q", tc.in, got)
		}
	}
}

func TestConsoleOriginPrefersBrowserFacing(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:19001/v1/me/connectors/google/connect", nil)
	r.Host = "127.0.0.1:19001"
	r.Header.Set("Origin", "http://192.168.1.8:19000")

	scheme, host, err := httpx.ConsoleOrigin(r, "")
	if err != nil {
		t.Fatal(err)
	}
	if scheme != "http" || host != "192.168.1.8:19000" {
		t.Fatalf("got %s://%s", scheme, host)
	}

	scheme, host, err = httpx.ConsoleOrigin(r, "http://10.10.1.3:9527")
	if err != nil {
		t.Fatal(err)
	}
	if scheme != "http" || host != "10.10.1.3:9527" {
		t.Fatalf("explicit return ignored: %s://%s", scheme, host)
	}
}

func TestConsoleOriginRejectsPublicHTTP(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "http://api/v1", nil)
	r.Host = "api.example.com"
	_, _, err := httpx.ConsoleOrigin(r, "http://evil.example.com")
	if err == nil {
		t.Fatal("expected error for public http")
	}
}
