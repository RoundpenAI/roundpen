package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when an assistant does not exist.
var ErrNotFound = errors.New("assistant not found")

// Store persists assistants.
type Store struct {
	DB *sql.DB
}

// Create inserts a new assistant.
func (s *Store) Create(ctx context.Context, userID string, in CreateInput) (*Assistant, error) {
	if err := ValidateCreate(in); err != nil {
		return nil, err
	}
	caps := ApplyPreset(in.Preset)
	if in.Capabilities != nil {
		caps = *in.Capabilities
	}
	caps.Mobile = false
	caps.Desktop = false

	now := time.Now().UTC()
	a := &Assistant{
		ID:               uuid.NewString(),
		UserID:           userID,
		Name:             strings.TrimSpace(in.Name),
		Bio:              strings.TrimSpace(in.Bio),
		IdentityMode:     in.IdentityMode,
		Capabilities:     caps,
		NetworkTier:      NetworkDevSites,
		NetworkAllowlist: []string{},
		DirectoryGrants:  []DirectoryGrant{},
		Status:           StatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	capRaw, err := capsJSON(caps)
	if err != nil {
		return nil, err
	}
	allowRaw, err := json.Marshal(a.NetworkAllowlist)
	if err != nil {
		return nil, err
	}
	dirRaw, err := json.Marshal(a.DirectoryGrants)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO assistants (
			id, user_id, name, bio, identity_mode, capabilities,
			network_tier, network_allowlist, directory_grants, status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		a.ID, a.UserID, a.Name, a.Bio, a.IdentityMode, capRaw,
		a.NetworkTier, allowRaw, dirRaw, a.Status, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Get returns an assistant by id.
func (s *Store) Get(ctx context.Context, id string) (*Assistant, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, name, bio, identity_mode, capabilities,
			network_tier, network_allowlist, directory_grants, status, created_at, updated_at
		FROM assistants WHERE id=$1`, id)
	a, err := scanAssistant(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return a, nil
}

// ListByUser returns assistants for a user (newest first).
func (s *Store) ListByUser(ctx context.Context, userID string) ([]*Assistant, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, name, bio, identity_mode, capabilities,
			network_tier, network_allowlist, directory_grants, status, created_at, updated_at
		FROM assistants
		WHERE user_id=$1 AND status <> $2
		ORDER BY updated_at DESC`, userID, StatusDisabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Assistant
	for rows.Next() {
		a, err := scanAssistant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateInput holds optional fields for PATCH.
type UpdateInput struct {
	Name             *string
	Bio              *string
	IdentityMode     *string
	Capabilities     *Capabilities
	NetworkTier      *string
	NetworkAllowlist *[]string
	DirectoryGrants  *[]DirectoryGrant
	Status           *string
}

// Update applies a partial update.
func (s *Store) Update(ctx context.Context, id string, in UpdateInput) (*Assistant, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		cur.Name = name
	}
	if in.Bio != nil {
		cur.Bio = strings.TrimSpace(*in.Bio)
	}
	if in.IdentityMode != nil {
		switch *in.IdentityMode {
		case IdentityProxyUser, IdentityIndependent:
			cur.IdentityMode = *in.IdentityMode
		default:
			return nil, fmt.Errorf("identityMode must be proxy_user or independent")
		}
	}
	if in.Capabilities != nil {
		caps := *in.Capabilities
		caps.Mobile = false
		caps.Desktop = false
		cur.Capabilities = caps
	}
	if in.NetworkTier != nil {
		switch *in.NetworkTier {
		case NetworkNone, NetworkDevSites, NetworkAll:
			cur.NetworkTier = *in.NetworkTier
		default:
			return nil, fmt.Errorf("networkTier must be none, dev_sites, or all")
		}
	}
	if in.NetworkAllowlist != nil {
		cur.NetworkAllowlist = *in.NetworkAllowlist
		if cur.NetworkAllowlist == nil {
			cur.NetworkAllowlist = []string{}
		}
	}
	if in.DirectoryGrants != nil {
		cur.DirectoryGrants = *in.DirectoryGrants
		if cur.DirectoryGrants == nil {
			cur.DirectoryGrants = []DirectoryGrant{}
		}
	}
	if in.Status != nil {
		switch *in.Status {
		case StatusActive, StatusDisabled:
			cur.Status = *in.Status
		default:
			return nil, fmt.Errorf("status must be active or disabled")
		}
	}
	cur.UpdatedAt = time.Now().UTC()
	capRaw, err := capsJSON(cur.Capabilities)
	if err != nil {
		return nil, err
	}
	allowRaw, err := json.Marshal(cur.NetworkAllowlist)
	if err != nil {
		return nil, err
	}
	dirRaw, err := json.Marshal(cur.DirectoryGrants)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE assistants SET
			name=$2, bio=$3, identity_mode=$4, capabilities=$5,
			network_tier=$6, network_allowlist=$7, directory_grants=$8,
			status=$9, updated_at=$10
		WHERE id=$1`,
		cur.ID, cur.Name, cur.Bio, cur.IdentityMode, capRaw,
		cur.NetworkTier, allowRaw, dirRaw, cur.Status, cur.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return cur, nil
}

// AttachOrphanSessions sets assistant_id on sessions that lack one for this user.
func (s *Store) AttachOrphanSessions(ctx context.Context, userID, assistantID string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE agent_sessions SET assistant_id=$1, updated_at=$2
		WHERE user_id=$3 AND (assistant_id IS NULL OR assistant_id='')`,
		assistantID, time.Now().UTC(), userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanAssistant(row interface{ Scan(dest ...any) error }) (*Assistant, error) {
	var a Assistant
	var capRaw, allowRaw, dirRaw []byte
	if err := row.Scan(
		&a.ID, &a.UserID, &a.Name, &a.Bio, &a.IdentityMode, &capRaw,
		&a.NetworkTier, &allowRaw, &dirRaw, &a.Status, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(capRaw) > 0 {
		if err := json.Unmarshal(capRaw, &a.Capabilities); err != nil {
			return nil, err
		}
	}
	if len(allowRaw) > 0 {
		if err := json.Unmarshal(allowRaw, &a.NetworkAllowlist); err != nil {
			return nil, err
		}
	}
	if a.NetworkAllowlist == nil {
		a.NetworkAllowlist = []string{}
	}
	if len(dirRaw) > 0 {
		if err := json.Unmarshal(dirRaw, &a.DirectoryGrants); err != nil {
			return nil, err
		}
	}
	if a.DirectoryGrants == nil {
		a.DirectoryGrants = []DirectoryGrant{}
	}
	return &a, nil
}
