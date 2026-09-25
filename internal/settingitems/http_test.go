package settingitems_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type fakeRebuilder struct{ calls []string }

func (f *fakeRebuilder) RebuildForSlot(_ context.Context, username, slot string) (string, any, error) {
	f.calls = append(f.calls, username+"/"+slot)
	return "recreated", map[string]any{"slot": slot}, nil
}

func proxyCatalog(t *testing.T, rb settingitems.EnvRebuilder) (*settingitems.Catalog, *http.ServeMux) {
	t.Helper()
	reg, err := settingitems.NewRegistry(
		[]settingitems.KindDef{userenv.ProxyKind()},
		userenv.ProxySlots(),
	)
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	if err := cat.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&settingitems.Handler{Cat: cat, Envs: rb}).Mount(mux)
	return cat, mux
}

func do(t *testing.T, mux *http.ServeMux, method, path, body string, user *storage.User) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req = req.WithContext(auth.WithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

var (
	adminUser = &storage.User{Username: "root", Role: storage.RoleAdmin}
	plainUser = &storage.User{Username: "alice", Role: storage.RoleUser}
)

const usProxy = `{"kind":"proxy","id":"us","name":"US egress","description":"overseas","enabled":true,` +
	`"config":{"url":"socks5://10.0.0.9:1080"}}`

func TestAdminItemCRUDMasksSecrets(t *testing.T) {
	cat, mux := proxyCatalog(t, nil)

	rec := do(t, mux, http.MethodPost, "/v1/admin/setting-items", usProxy, adminUser)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Item settingitems.Item `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Item.Config["url"] != "●●●●●●●●" {
		t.Fatalf("create response must mask the url: %+v", created.Item.Config)
	}

	rec = do(t, mux, http.MethodGet, "/v1/admin/setting-items?kind=proxy", "", adminUser)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "US egress") {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "10.0.0.9") {
		t.Fatalf("list leaked the proxy url: %s", rec.Body.String())
	}

	// Submitting the mask back keeps the stored secret.
	rec = do(t, mux, http.MethodPut, "/v1/admin/setting-items/proxy/us",
		`{"name":"US egress","enabled":true,"config":{"url":"●●●●●●●●"}}`, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	res := cat.Snapshot()
	it, ok := res.Item(settingitems.KindProxy, "us")
	if !ok || it.Secret("url") != "socks5://10.0.0.9:1080" {
		t.Fatalf("masked update dropped the secret: %+v", it.Secrets)
	}
}

func TestUserSeesSelectableFieldsOnly(t *testing.T) {
	_, mux := proxyCatalog(t, nil)
	do(t, mux, http.MethodPost, "/v1/admin/setting-items", usProxy, adminUser)

	rec := do(t, mux, http.MethodGet, "/v1/me/setting-items?kind=proxy", "", plainUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("user list: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "10.0.0.9") || strings.Contains(body, "url") {
		t.Fatalf("user list leaked config: %s", body)
	}
	if !strings.Contains(body, "US egress") {
		t.Fatalf("user list missing the item name: %s", body)
	}
}

func TestUserBindingRebuildsEnv(t *testing.T) {
	rb := &fakeRebuilder{}
	_, mux := proxyCatalog(t, rb)
	do(t, mux, http.MethodPost, "/v1/admin/setting-items", usProxy, adminUser)

	rec := do(t, mux, http.MethodPut, "/v1/me/setting-bindings/proxy.agent", `{"itemId":"us"}`, plainUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("bind: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "recreated") {
		t.Fatalf("expected rebuild status: %s", rec.Body.String())
	}
	if len(rb.calls) != 1 || rb.calls[0] != "alice/proxy.agent" {
		t.Fatalf("rebuild calls: %v", rb.calls)
	}

	rec = do(t, mux, http.MethodGet, "/v1/me/setting-bindings", "", plainUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("bindings: %d %s", rec.Code, rec.Body.String())
	}
	var view struct {
		Slots map[string]struct {
			ItemID          string `json:"itemId"`
			EffectiveItemID string `json:"effectiveItemId"`
			Source          string `json:"source"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	entry, ok := view.Slots[settingitems.SlotProxyAgent]
	if !ok || entry.ItemID != "us" || entry.EffectiveItemID != "us" || entry.Source != "user" {
		t.Fatalf("bindings view: %+v", view.Slots)
	}

	rec = do(t, mux, http.MethodDelete, "/v1/me/setting-bindings/proxy.agent", "", plainUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminDeleteBoundItemConflicts(t *testing.T) {
	_, mux := proxyCatalog(t, nil)
	do(t, mux, http.MethodPost, "/v1/admin/setting-items", usProxy, adminUser)
	do(t, mux, http.MethodPut, "/v1/me/setting-bindings/proxy.agent", `{"itemId":"us"}`, plainUser)

	rec := do(t, mux, http.MethodDelete, "/v1/admin/setting-items/proxy/us", "", adminUser)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "proxy.agent") {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, mux, http.MethodDelete, "/v1/admin/setting-items/proxy/us?force=true", "", adminUser)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("force delete: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSchemaEndpointAndGuard(t *testing.T) {
	_, mux := proxyCatalog(t, nil)

	rec := do(t, mux, http.MethodGet, "/v1/setting-schema", "", plainUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("schema: %d", rec.Code)
	}
	var schema struct {
		Kinds []settingitems.KindDef `json:"kinds"`
		Slots []settingitems.SlotDef `json:"slots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Kinds) != 1 || schema.Kinds[0].Kind != settingitems.KindProxy || len(schema.Kinds[0].Fields) == 0 {
		t.Fatalf("kinds: %+v", schema.Kinds)
	}
	if len(schema.Slots) != 2 || !schema.Slots[0].UserOverride || !schema.Slots[0].RebuildsEnv {
		t.Fatalf("slots: %+v", schema.Slots)
	}

	// Non-admins cannot reach the admin surface.
	rec = do(t, mux, http.MethodGet, "/v1/admin/setting-items", "", plainUser)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin guard: %d", rec.Code)
	}
}

// TestInvalidInputAnswersBadRequest keeps a rejected value on the 400 path:
// it used to fall through to the internal-error branch and log a warning.
// A blank display name is not an error: the item takes its id, which is what
// the console lists and what the slot pickers show.
func TestSaveWithoutNameUsesItemID(t *testing.T) {
	cat, mux := proxyCatalog(t, nil)

	rec := do(t, mux, http.MethodPut, "/v1/admin/setting-items/proxy/us",
		`{"kind":"proxy","id":"us","enabled":true,"config":{"url":"socks5://10.0.0.9:1080"}}`, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("save without a name: %d %s", rec.Code, rec.Body.String())
	}
	it, ok := cat.Snapshot().Item(settingitems.KindProxy, "us")
	if !ok || it.Name != "us" {
		t.Fatalf("want the id as the display name, got %+v", it)
	}
}

func TestInvalidInputAnswersBadRequest(t *testing.T) {
	_, mux := proxyCatalog(t, nil)

	cases := []struct {
		name     string
		method   string
		path     string
		body     string
		wantText string
	}{
		{
			name:     "invalid proxy url",
			method:   http.MethodPut,
			path:     "/v1/admin/setting-items/proxy/us",
			body:     `{"kind":"proxy","id":"us","name":"US egress","enabled":true,"config":{"url":"ftp://10.0.0.9"}}`,
			wantText: "must use http, https, socks5 or socks5h",
		},
		{
			name:     "missing url",
			method:   http.MethodPut,
			path:     "/v1/admin/setting-items/proxy/us",
			body:     `{"kind":"proxy","id":"us","name":"US egress","enabled":true,"config":{}}`,
			wantText: "url is required",
		},
		{
			name:     "unknown item bound to a slot",
			method:   http.MethodPut,
			path:     "/v1/me/setting-bindings/proxy.agent",
			body:     `{"itemId":"ghost"}`,
			wantText: "unknown proxy item",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := adminUser
			if tc.path == "/v1/me/setting-bindings/proxy.agent" {
				user = plainUser
			}
			rec := do(t, mux, tc.method, tc.path, tc.body, user)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantText) {
				t.Fatalf("body = %s, want %q", rec.Body.String(), tc.wantText)
			}
		})
	}
}
