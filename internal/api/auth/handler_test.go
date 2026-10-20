package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestRegisterUser_GeneratesPasswordForLogin(t *testing.T) {
	users := storage.NewMemoryUserStore()
	h := NewUserHandler(users)

	body, _ := json.Marshal(map[string]string{
		"username": "alice",
		"email":    "alice@example.com",
		"fullname": "Alice",
		"orgName":  "Lab",
	})
	req := httptest.NewRequest("POST", "/v1/admin/users", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		APIKey   string `json:"apiKey"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Password == "" || len(created.Password) < minPasswordLen {
		t.Fatalf("expected generated password, got %q", created.Password)
	}
	if created.APIKey == "" || created.APIKey[:3] != "rp-" {
		t.Fatalf("expected api key, got %q", created.APIKey)
	}

	stored, err := users.GetByEmail(t.Context(), "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(stored.PasswordHash, created.Password) {
		t.Fatal("stored hash does not match returned password")
	}
}

func TestRegisterUser_UsesProvidedPassword(t *testing.T) {
	users := storage.NewMemoryUserStore()
	h := NewUserHandler(users)

	body, _ := json.Marshal(map[string]string{
		"username": "bob",
		"email":    "bob@example.com",
		"password": "chosen-pass",
	})
	req := httptest.NewRequest("POST", "/v1/admin/users", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, _ := users.GetByUsername(t.Context(), "bob")
	if !CheckPassword(stored.PasswordHash, "chosen-pass") {
		t.Fatal("provided password was not stored")
	}
}

func TestRegisterUser_RequiresUsername(t *testing.T) {
	users := storage.NewMemoryUserStore()
	h := NewUserHandler(users)
	body, _ := json.Marshal(map[string]string{"email": "a@b.com"})
	req := httptest.NewRequest("POST", "/v1/admin/users", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.RegisterUser(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestResetPassword_SetsLoginPassword(t *testing.T) {
	users := storage.NewMemoryUserStore()
	if err := users.Upsert(t.Context(), storage.User{Username: "old", Email: "old@example.com", APIKey: "rp-old", Role: storage.RoleUser}); err != nil {
		t.Fatal(err)
	}
	h := NewUserHandler(users)
	req := httptest.NewRequest("POST", "/v1/admin/users/old/password", nil)
	req.SetPathValue("username", "old")
	rec := httptest.NewRecorder()
	h.ResetPassword(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	stored, _ := users.GetByUsername(t.Context(), "old")
	if !CheckPassword(stored.PasswordHash, out.Password) {
		t.Fatal("reset password not stored")
	}
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	if err := users.Upsert(t.Context(), storage.User{
		Username: "alice", APIKey: "rp-user", Role: storage.RoleUser,
	}); err != nil {
		t.Fatal(err)
	}
	if err := users.Upsert(t.Context(), storage.User{
		Username: "root", APIKey: "rp-admin", Role: storage.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Mount(mux, users, sessions, func() bool { return false }, "")
	h := Middleware(users, sessions)(mux)

	req := httptest.NewRequest("GET", "/v1/admin/users", nil)
	req.Header.Set("X-API-Key", "rp-user")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest("GET", "/v1/admin/users", nil)
	req.Header.Set("X-API-Key", "rp-admin")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProfile_SelfServeFields(t *testing.T) {
	users := storage.NewMemoryUserStore()
	if err := users.Upsert(t.Context(), storage.User{
		Username: "alice", Email: "a@b.com", APIKey: "rp-alice", Role: storage.RoleUser,
	}); err != nil {
		t.Fatal(err)
	}
	stored, _ := users.GetByUsername(t.Context(), "alice")
	h := NewUserHandler(users)
	var reinjected string
	h.OnProfileChange = func(_ context.Context, userID string) { reinjected = userID }

	body, _ := json.Marshal(map[string]any{
		"fullname":  "Alice",
		"orgName":   "Acme",
		"about":     "Prefers short replies.",
		"timezone":  "Asia/Shanghai",
		"gitName":   "Alice L",
		"gitEmail":  "alice@example.com",
		"gpgSign":   false,
	})
	req := httptest.NewRequest("PUT", "/v1/auth/profile", bytes.NewReader(body))
	req = req.WithContext(WithUser(req.Context(), stored))
	rec := httptest.NewRecorder()
	h.UpdateProfile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	got, err := users.GetByUsername(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.FullName != "Alice" || got.OrgName != "Acme" || got.About != "Prefers short replies." || got.Timezone != "Asia/Shanghai" {
		t.Fatalf("profile not saved: %+v", got)
	}
	if got.GitName != "Alice L" || got.GitEmail != "alice@example.com" {
		t.Fatalf("git identity not saved: %+v", got)
	}
	if reinjected != "alice" {
		t.Fatalf("OnProfileChange = %q", reinjected)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["hasGpgKey"] != false {
		t.Fatalf("hasGpgKey = %v", resp["hasGpgKey"])
	}
}

func TestUpdateProfile_RejectsBadTimezone(t *testing.T) {
	users := storage.NewMemoryUserStore()
	_ = users.Upsert(t.Context(), storage.User{Username: "alice", APIKey: "rp-a", Role: storage.RoleUser})
	stored, _ := users.GetByUsername(t.Context(), "alice")
	h := NewUserHandler(users)
	body, _ := json.Marshal(map[string]string{"timezone": "Not/AZone"})
	req := httptest.NewRequest("PUT", "/v1/auth/profile", bytes.NewReader(body))
	req = req.WithContext(WithUser(req.Context(), stored))
	rec := httptest.NewRecorder()
	h.UpdateProfile(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
