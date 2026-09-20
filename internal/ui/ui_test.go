package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerSetsSecurityHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (dist must be embedded)", rec.Code)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Fatalf("CSP = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
}
