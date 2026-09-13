package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBlockedDialAddr(t *testing.T) {
	cases := []struct {
		addr  string
		block bool
	}{
		{"127.0.0.1:80", true},
		{"[::1]:80", true},
		{"[::ffff:127.0.0.1]:80", true},
		{"169.254.169.254:80", true},
		{"[fe80::1]:80", true},
		{"[::ffff:169.254.169.254]:80", true},
		{"0.0.0.0:80", true},
		{"[::]:80", true},
		{"224.0.0.1:80", true},
		{"[ff02::1]:80", true},
		{"example.com:443", true},
		{"[fe80::1%eth0]:80", true},
		{"10.1.2.3:80", false},
		{"172.16.9.9:80", false},
		{"192.168.1.10:443", false},
		{"8.8.8.8:443", false},
		{"[2606:4700:4700::1111]:443", false},
	}
	for _, tc := range cases {
		err := blockedDialAddr(tc.addr, false)
		if tc.block && err == nil {
			t.Errorf("%s: expected blocked", tc.addr)
		}
		if !tc.block && err != nil {
			t.Errorf("%s: unexpected block: %v", tc.addr, err)
		}
	}
}

func TestBlockedDialAddrAllowLoopback(t *testing.T) {
	if err := blockedDialAddr("127.0.0.1:80", true); err != nil {
		t.Fatalf("loopback should be allowed when opted in: %v", err)
	}
	if err := blockedDialAddr("169.254.169.254:80", true); err == nil {
		t.Fatal("metadata address must stay blocked even with AllowLoopback")
	}
}

func TestWebHTTPClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := NewWebHTTPClient(WebClientOptions{})
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected loopback request to be blocked")
	}
}

func TestWebHTTPClientAllowsLoopbackForTests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := NewWebHTTPClient(WebClientOptions{AllowLoopback: true})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestWebHTTPClientBlocksRedirectToMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	client := NewWebHTTPClient(WebClientOptions{AllowLoopback: true})
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("redirect to a metadata address must be blocked")
	}
}
