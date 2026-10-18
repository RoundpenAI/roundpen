package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestHTTPPluginToggle(t *testing.T) {
	svc := &Service{Settings: &memSettings{}}
	mux := http.NewServeMux()
	(&Handler{Svc: svc}).Mount(mux)

	admin := &storage.User{Username: "root", Role: storage.RoleAdmin}
	user := &storage.User{Username: "alice", Role: storage.RoleUser}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/plugins", nil)
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/admin/plugins", nil)
	req = req.WithContext(auth.WithUser(req.Context(), admin))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d body %s", rr.Code, rr.Body)
	}
	var list struct {
		Plugins []Status `json:"plugins"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Plugins) != 1 || list.Plugins[0].Enabled {
		t.Fatalf("list = %+v", list.Plugins)
	}

	req = httptest.NewRequest(http.MethodPut, "/v1/admin/plugins/"+SelfHostedGit,
		strings.NewReader(`{"enabled":true}`))
	req = req.WithContext(auth.WithUser(req.Context(), admin))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("enable status = %d body %s", rr.Code, rr.Body)
	}
	if !svc.IsEnabled(SelfHostedGit) {
		t.Fatal("plugin not enabled")
	}

	req = httptest.NewRequest(http.MethodPut, "/v1/admin/plugins/nope",
		strings.NewReader(`{"enabled":true}`))
	req = req.WithContext(auth.WithUser(req.Context(), admin))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d", rr.Code)
	}
}
