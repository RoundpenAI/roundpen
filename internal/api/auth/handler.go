package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// UserHandler serves current-user and admin user-management endpoints.
type UserHandler struct {
	users storage.UserStore
}

// NewUserHandler constructs a user handler.
func NewUserHandler(users storage.UserStore) *UserHandler {
	return &UserHandler{users: users}
}

func maskAPIKey(key string) string {
	if !strings.HasPrefix(key, APIKeyPrefix) || len(key) <= 8 {
		return "rp-****"
	}
	return key[:7] + "..." + key[len(key)-4:]
}

func (h *UserHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	masked := *user
	masked.APIKey = maskAPIKey(masked.APIKey)
	writeJSON(w, http.StatusOK, struct {
		storage.User
		HasPassword bool `json:"hasPassword"`
	}{User: masked, HasPassword: user.PasswordHash != ""})
}

// UserWithFlags is a user listing row with secrets redacted.
type UserWithFlags struct {
	storage.User
	HasPassword bool `json:"hasPassword"`
}

func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.users.ListAll(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	result := make([]UserWithFlags, len(users))
	for i, u := range users {
		result[i].User = u
		result[i].APIKey = maskAPIKey(u.APIKey)
		result[i].HasPassword = u.PasswordHash != ""
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *UserHandler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string           `json:"username"`
		Email    string           `json:"email"`
		FullName string           `json:"fullname"`
		OrgName  string           `json:"orgName"`
		Role     storage.UserRole `json:"role"`
		Password string           `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validUsername.MatchString(req.Username) {
		writeErr(w, http.StatusBadRequest, "missing or invalid username")
		return
	}
	if _, err := h.users.GetByUsername(r.Context(), req.Username); err == nil {
		writeErr(w, http.StatusConflict, "username already exists")
		return
	} else if err != storage.ErrNotFound {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}

	email := ""
	if strings.TrimSpace(req.Email) != "" {
		var err error
		email, err = normalizeEmail(req.Email)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid email")
			return
		}
		if existing, err := h.users.GetByEmail(r.Context(), email); err == nil && existing.Username != req.Username {
			writeErr(w, http.StatusConflict, "email already registered")
			return
		} else if err != nil && err != storage.ErrNotFound {
			writeErr(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	apiKey, err := NewID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	user := storage.User{
		Username:     req.Username,
		Email:        email,
		FullName:     req.FullName,
		OrgName:      req.OrgName,
		Role:         req.Role,
		APIKey:       APIKeyPrefix + apiKey,
		AuthProvider: "local",
	}
	if user.Role == "" {
		user.Role = storage.RoleUser
	}
	if !user.Role.Valid() {
		writeErr(w, http.StatusBadRequest, "role must be 'user' or 'admin'")
		return
	}
	plain, err := assignLoginPassword(&user, req.Password)
	if err != nil {
		if errors.Is(err, errPasswordTooShort) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.users.Upsert(r.Context(), user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		storage.User
		Password string `json:"password"`
	}{User: user, Password: plain})
}

func (h *UserHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	user, err := h.users.GetByUsername(r.Context(), username)
	if err != nil {
		if err == storage.ErrNotFound {
			writeErr(w, http.StatusNotFound, "user not found")
		} else {
			writeErr(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	plain, err := assignLoginPassword(user, req.Password)
	if err != nil {
		if errors.Is(err, errPasswordTooShort) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.users.Upsert(r.Context(), *user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"username": user.Username,
		"password": plain,
	})
}

func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	user, err := h.users.GetByUsername(r.Context(), username)
	if err != nil {
		if err == storage.ErrNotFound {
			writeErr(w, http.StatusNotFound, "user not found")
		} else {
			writeErr(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	var req struct {
		Email    string           `json:"email"`
		FullName string           `json:"fullname"`
		OrgName  string           `json:"orgName"`
		Role     storage.UserRole `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email != "" {
		email, err := normalizeEmail(req.Email)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid email")
			return
		}
		user.Email = email
	}
	if req.FullName != "" {
		user.FullName = req.FullName
	}
	if req.OrgName != "" {
		user.OrgName = req.OrgName
	}
	if req.Role != "" {
		if !req.Role.Valid() {
			writeErr(w, http.StatusBadRequest, "role must be 'user' or 'admin'")
			return
		}
		user.Role = req.Role
	}
	if err := h.users.Upsert(r.Context(), *user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	masked := *user
	masked.APIKey = maskAPIKey(masked.APIKey)
	writeJSON(w, http.StatusOK, masked)
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if username == "admin" {
		writeErr(w, http.StatusBadRequest, "cannot delete admin")
		return
	}
	if err := h.users.Delete(r.Context(), username); err != nil {
		if err == storage.ErrNotFound {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *UserHandler) GenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}
	user, err := h.users.GetByUsername(r.Context(), username)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	id, err := NewID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	user.APIKey = APIKeyPrefix + id
	if err := h.users.Upsert(r.Context(), *user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"username": user.Username,
		"apiKey":   user.APIKey,
	})
}

// Mount registers auth and admin user routes.
func Mount(mux *http.ServeMux, users storage.UserStore, sessions storage.SessionStore, allowRegistration func() bool) {
	sh := NewSessionHandler(users, sessions, allowRegistration)
	uh := NewUserHandler(users)
	mux.HandleFunc("POST /v1/auth/register", sh.Register)
	mux.HandleFunc("POST /v1/auth/login", sh.Login)
	mux.HandleFunc("POST /v1/auth/logout", sh.Logout)
	mux.HandleFunc("GET /v1/auth/user", uh.GetCurrentUser)
	mux.HandleFunc("POST /v1/auth/password", sh.ChangePassword)
	mux.HandleFunc("POST /v1/auth/apikey/rotate", sh.RotateAPIKey)

	mux.HandleFunc("GET /v1/admin/users", RequireAdmin(uh.ListUsers))
	mux.HandleFunc("POST /v1/admin/users", RequireAdmin(uh.RegisterUser))
	mux.HandleFunc("PUT /v1/admin/users/{username}", RequireAdmin(uh.UpdateUser))
	mux.HandleFunc("DELETE /v1/admin/users/{username}", RequireAdmin(uh.DeleteUser))
	mux.HandleFunc("POST /v1/admin/users/{username}/apikey", RequireAdmin(uh.GenerateAPIKey))
	mux.HandleFunc("POST /v1/admin/users/{username}/password", RequireAdmin(uh.ResetPassword))
}
