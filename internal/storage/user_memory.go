package storage

import (
	"context"
	"strings"
	"sync"
	"time"
)

// MemoryUserStore is an in-memory UserStore for tests.
type MemoryUserStore struct {
	mu    sync.RWMutex
	users map[string]User // keyed by username
}

// NewMemoryUserStore returns an empty in-memory user store.
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{users: make(map[string]User)}
}

func (m *MemoryUserStore) GetByAPIKey(_ context.Context, apiKey string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.APIKey == apiKey || u.APIKey == HashAPIKey(apiKey) {
			cp := u
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryUserStore) GetByUsername(_ context.Context, username string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[username]
	if !ok {
		return nil, ErrNotFound
	}
	cp := u
	return &cp, nil
}

func (m *MemoryUserStore) GetByEmail(_ context.Context, email string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	want := strings.ToLower(strings.TrimSpace(email))
	if want == "" {
		return nil, ErrNotFound
	}
	for _, u := range m.users {
		if strings.ToLower(u.Email) == want {
			cp := u
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryUserStore) Upsert(_ context.Context, user User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.users[user.Username]; ok {
		if user.PasswordHash == "" {
			user.PasswordHash = existing.PasswordHash
		}
		if user.AuthProvider == "" {
			user.AuthProvider = existing.AuthProvider
		}
		if user.CreatedAt.IsZero() {
			user.CreatedAt = existing.CreatedAt
		}
	} else if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}
	if user.AuthProvider == "" {
		user.AuthProvider = "local"
	}
	if user.Role == "" {
		user.Role = RoleUser
	}
	user.UpdatedAt = time.Now().UTC()
	user.APIKey = storedAPIKey(user.APIKey)
	m.users[user.Username] = user
	return nil
}

func (m *MemoryUserStore) ListAll(_ context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]User, 0, len(m.users))
	for _, u := range m.users {
		res = append(res, u)
	}
	return res, nil
}

func (m *MemoryUserStore) Delete(_ context.Context, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[username]; !ok {
		return ErrNotFound
	}
	delete(m.users, username)
	return nil
}

// MemorySessionStore is an in-memory SessionStore for tests.
type MemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session // keyed by id
}

// NewMemorySessionStore returns an empty in-memory session store.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: make(map[string]Session)}
}

func (m *MemorySessionStore) Create(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *MemorySessionStore) GetByTokenHash(_ context.Context, hash string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, s := range m.sessions {
		if s.TokenHash == hash && s.ExpiresAt.After(now) {
			cp := s
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemorySessionStore) Touch(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	s.LastSeenAt = time.Now()
	s.ExpiresAt = time.Now().Add(sessionTTL)
	m.sessions[id] = s
	return nil
}

func (m *MemorySessionStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (m *MemorySessionStore) DeleteByUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, id)
		}
	}
	return nil
}

var _ UserStore = (*MemoryUserStore)(nil)
var _ SessionStore = (*MemorySessionStore)(nil)
