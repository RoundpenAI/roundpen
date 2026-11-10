package nasscene

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/assistant"
)

const AuthProviderFnOS = "fnos"

// Assistants creates and updates seed assistants.
type Assistants interface {
	Create(ctx context.Context, userID string, in assistant.CreateInput) (*assistant.Assistant, error)
	Get(ctx context.Context, id string) (*assistant.Assistant, error)
	Update(ctx context.Context, id string, in assistant.UpdateInput) (*assistant.Assistant, error)
}

// Store persists scene-pack prefs and template→assistant maps.
type Store struct {
	DB         *sql.DB
	Assistants Assistants
	Routines   Routines // optional; seeds morning OnThisDay when set
}

// Member is one template's status for the settings UI.
type Member struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	AssistantID string `json:"assistantId,omitempty"`
	Status      string `json:"status"` // missing | active | disabled
}

// Status is the pack state returned to the console.
type Status struct {
	Enabled       bool     `json:"enabled"`
	DefaultOnFnOS bool     `json:"defaultOnFnOS"`
	AuthProvider  string   `json:"authProvider"`
	Members       []Member `json:"members"`
}

// Enabled reports whether the pack should be active for this user.
// Explicit pref wins; otherwise fnOS login defaults on.
func (s *Store) Enabled(ctx context.Context, userID, authProvider string) (bool, error) {
	if s == nil || s.DB == nil {
		return false, nil
	}
	var enabled bool
	err := s.DB.QueryRowContext(ctx, `
		SELECT enabled FROM nas_scene_prefs WHERE user_id=$1`, userID).Scan(&enabled)
	if err == nil {
		return enabled, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(authProvider), AuthProviderFnOS), nil
}

// SetEnabled writes the pref and ensures or disables seed assistants.
func (s *Store) SetEnabled(ctx context.Context, userID string, enabled bool) error {
	if s == nil || s.DB == nil || s.Assistants == nil {
		return fmt.Errorf("nas scene store not configured")
	}
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO nas_scene_prefs (user_id, enabled, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET enabled=EXCLUDED.enabled, updated_at=EXCLUDED.updated_at`,
		userID, enabled, now)
	if err != nil {
		return err
	}
	if enabled {
		_, err = s.Ensure(ctx, userID)
		return err
	}
	return s.disableAll(ctx, userID)
}

// Ensure creates missing seed assistants and re-activates disabled ones.
func (s *Store) Ensure(ctx context.Context, userID string) ([]*assistant.Assistant, error) {
	if s == nil || s.DB == nil || s.Assistants == nil {
		return nil, fmt.Errorf("nas scene store not configured")
	}
	var out []*assistant.Assistant
	for _, tmpl := range Templates() {
		a, err := s.ensureOne(ctx, userID, tmpl)
		if err != nil {
			return out, err
		}
		if a != nil {
			out = append(out, a)
			if tmpl.Key == KeyPhotos {
				if err := s.ensurePhotosRoutine(ctx, userID, a.ID); err != nil {
					return out, err
				}
			}
		}
	}
	return out, nil
}

// RestoreMissing creates any template that has no mapped assistant (or whose
// assistant was deleted). Does not change the enabled pref.
func (s *Store) RestoreMissing(ctx context.Context, userID string) error {
	_, err := s.Ensure(ctx, userID)
	return err
}

// Describe builds settings status for the user.
func (s *Store) Describe(ctx context.Context, userID, authProvider string) (Status, error) {
	enabled, err := s.Enabled(ctx, userID, authProvider)
	if err != nil {
		return Status{}, err
	}
	st := Status{
		Enabled:       enabled,
		DefaultOnFnOS: true,
		AuthProvider:  authProvider,
		Members:       make([]Member, 0, len(Templates())),
	}
	for _, tmpl := range Templates() {
		m := Member{Key: tmpl.Key, Name: tmpl.Name, Status: "missing"}
		id, err := s.mappedID(ctx, userID, tmpl.Key)
		if err != nil {
			return Status{}, err
		}
		if id != "" {
			m.AssistantID = id
			a, gerr := s.Assistants.Get(ctx, id)
			if gerr != nil || a == nil {
				m.Status = "missing"
				m.AssistantID = ""
			} else {
				m.Status = a.Status
				if m.Status == "" {
					m.Status = assistant.StatusActive
				}
			}
		}
		st.Members = append(st.Members, m)
	}
	return st, nil
}

func (s *Store) ensureOne(ctx context.Context, userID string, tmpl Template) (*assistant.Assistant, error) {
	id, err := s.mappedID(ctx, userID, tmpl.Key)
	if err != nil {
		return nil, err
	}
	if id != "" {
		a, gerr := s.Assistants.Get(ctx, id)
		if gerr == nil && a != nil {
			needStatus := a.Status == assistant.StatusDisabled
			needCaps := a.Capabilities.Shell != tmpl.Caps.Shell ||
				a.Capabilities.Browser != tmpl.Caps.Browser
			if needStatus || needCaps {
				in := assistant.UpdateInput{}
				if needStatus {
					active := assistant.StatusActive
					in.Status = &active
				}
				if needCaps {
					caps := a.Capabilities
					caps.Shell = tmpl.Caps.Shell
					caps.Browser = tmpl.Caps.Browser
					in.Capabilities = &caps
				}
				return s.Assistants.Update(ctx, id, in)
			}
			return a, nil
		}
		// Stale mapping — drop and recreate.
		_, _ = s.DB.ExecContext(ctx, `
			DELETE FROM nas_scene_assistants WHERE user_id=$1 AND template_key=$2`, userID, tmpl.Key)
	}

	created, err := s.Assistants.Create(ctx, userID, assistant.CreateInput{
		Name:         tmpl.Name,
		Bio:          tmpl.Bio,
		IdentityMode: assistant.IdentityProxyUser,
		Preset:       "custom",
		Capabilities: &tmpl.Caps,
		Kind:         assistant.KindUser,
	})
	if err != nil {
		return nil, err
	}
	// Network tier is not on CreateInput — patch after create.
	tier := tmpl.NetworkTier
	if tier != "" && created.NetworkTier != tier {
		created, err = s.Assistants.Update(ctx, created.ID, assistant.UpdateInput{NetworkTier: &tier})
		if err != nil {
			return nil, err
		}
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO nas_scene_assistants (user_id, template_key, assistant_id, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, template_key) DO UPDATE SET assistant_id=EXCLUDED.assistant_id`,
		userID, tmpl.Key, created.ID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) disableAll(ctx context.Context, userID string) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT assistant_id FROM nas_scene_assistants WHERE user_id=$1`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	disabled := assistant.StatusDisabled
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, err := s.Assistants.Update(ctx, id, assistant.UpdateInput{Status: &disabled}); err != nil {
			// Already gone — ignore.
			if !errors.Is(err, assistant.ErrNotFound) {
				return err
			}
		}
	}
	return rows.Err()
}

func (s *Store) mappedID(ctx context.Context, userID, key string) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `
		SELECT assistant_id FROM nas_scene_assistants
		WHERE user_id=$1 AND template_key=$2`, userID, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
