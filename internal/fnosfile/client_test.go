package fnosfile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckACL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":[{"path":"/vol1/1000/Roundpen","readable":true,"writable":true,"deletable":false}]}`))
	}))
	defer srv.Close()

	c := &Client{AppName: "roundpen", BaseURL: srv.URL, HTTP: srv.Client(), Token: func() string { return "tok" }}
	acl, err := c.CheckACL(context.Background(), 1000, "/vol1/1000/Roundpen")
	if err != nil {
		t.Fatal(err)
	}
	if !acl.Readable || !acl.Writable {
		t.Fatalf("acl = %+v", acl)
	}
}

func TestDisabled(t *testing.T) {
	c := &Client{Token: func() string { return "" }}
	if c.Enabled() {
		t.Fatal("expected disabled")
	}
	if _, err := c.CheckACL(context.Background(), 1, "/vol1/1"); err == nil {
		t.Fatal("expected error")
	}
}
