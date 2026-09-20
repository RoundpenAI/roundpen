package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsPublicPath(t *testing.T) {
	public := []struct{ method, path string }{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/v1/ready"},
		{http.MethodPost, "/v1/auth/login"},
		{http.MethodPost, "/v1/auth/register"},
		{http.MethodGet, "/v1/auth/oauth/providers"},
		{http.MethodGet, "/v1/auth/oauth/gitea-git-eaxi-com/start"},
		{http.MethodGet, "/v1/auth/oauth/gitea-git-eaxi-com/callback"},
		{http.MethodGet, "/settings/accounts"},
	}
	for _, tc := range public {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if !isPublicPath(req) {
			t.Errorf("%s %s should be public", tc.method, tc.path)
		}
	}

	private := []struct{ method, path string }{
		{http.MethodGet, "/v1/auth/user"},
		{http.MethodPost, "/v1/auth/logout"},
		{http.MethodPost, "/v1/auth/oauth/providers"},
		{http.MethodGet, "/v1/me/identities"},
		{http.MethodPost, "/v1/me/identities/link/gitea-git-eaxi-com"},
		{http.MethodDelete, "/v1/me/identities/id-1"},
		{http.MethodGet, "/v1/admin/oauth/providers"},
		{http.MethodPut, "/v1/admin/oauth/providers"},
		{http.MethodDelete, "/v1/admin/oauth/providers/gitea-git-eaxi-com"},
	}
	for _, tc := range private {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if isPublicPath(req) {
			t.Errorf("%s %s must stay authenticated", tc.method, tc.path)
		}
	}
}
