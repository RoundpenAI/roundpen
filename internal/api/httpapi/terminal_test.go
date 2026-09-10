package httpapi

import (
	"net/http"
	"testing"
)

func TestCheckTerminalOrigin(t *testing.T) {
	h := &Handler{PublicURL: "https://console.example:9527"}
	req := func(origin, host string) *http.Request {
		r, _ := http.NewRequest(http.MethodGet, "http://"+host+"/v1/sandboxes/x/terminal", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !h.checkTerminalOrigin(req("", "127.0.0.1:9527")) {
		t.Fatal("empty origin should be allowed")
	}
	if !h.checkTerminalOrigin(req("http://127.0.0.1:9527", "127.0.0.1:9527")) {
		t.Fatal("same host should be allowed")
	}
	if !h.checkTerminalOrigin(req("https://console.example:9527", "127.0.0.1:9527")) {
		t.Fatal("configured public URL host should be allowed")
	}
	if h.checkTerminalOrigin(req("https://evil.example", "127.0.0.1:9527")) {
		t.Fatal("cross-origin should be rejected")
	}
}
