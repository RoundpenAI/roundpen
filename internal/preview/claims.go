package preview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// ErrNameTaken reports a label another user already claimed.
var ErrNameTaken = errors.New("name is already taken")

// reservedLabels cannot be claimed: names an operator commonly points at other
// services under the same wildcard, or that clients resolve specially. An
// operator whose zone carries other names should serve previews from a
// dedicated sub-zone (pv.example.com) instead of widening this list.
var reservedLabels = map[string]bool{
	"www": true, "api": true, "console": true, "app": true, "admin": true,
	"static": true, "cdn": true, "assets": true, "mail": true, "smtp": true,
	"imap": true, "pop": true, "ns": true, "ns1": true, "ns2": true,
	"dns": true, "ftp": true, "vpn": true, "status": true,
}

// Claim is one booked preview subdomain: name.{Domain} serves sandboxID:port
// until the owner releases it or claims the name again for another port.
type Claim struct {
	Name      string    `json:"name"`
	SandboxID string    `json:"sandboxID"`
	Port      int       `json:"port"`
	Owner     string    `json:"owner,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ClaimStore persists claims in PostgreSQL.
type ClaimStore struct {
	DB *sql.DB
}

// Resolve looks a label up for the vhost router. Claims win over the automatic
// {id}-{port} labels: they are explicit, and only one of the two can answer.
func (s *ClaimStore) Resolve(ctx context.Context, name string) (sandboxID string, port int, ok bool) {
	if s == nil || s.DB == nil {
		return "", 0, false
	}
	err := s.DB.QueryRowContext(ctx,
		`SELECT sandbox_id, port FROM preview_domains WHERE name = $1`, name).
		Scan(&sandboxID, &port)
	if err != nil {
		return "", 0, false
	}
	return sandboxID, port, true
}

// List returns claims owned by owner, or every claim when owner is empty
// (admin paths).
func (s *ClaimStore) List(ctx context.Context, owner string) ([]Claim, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	query := `SELECT name, sandbox_id, port, owner, created_at FROM preview_domains`
	args := []any{}
	if owner != "" {
		query += ` WHERE owner = $1`
		args = append(args, owner)
	}
	query += ` ORDER BY name`
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Claim
	for rows.Next() {
		var c Claim
		if err := rows.Scan(&c.Name, &c.SandboxID, &c.Port, &c.Owner, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns one claim regardless of owner (routing and admin paths).
func (s *ClaimStore) Get(ctx context.Context, name string) (*Claim, error) {
	if s == nil || s.DB == nil {
		return nil, sql.ErrNoRows
	}
	var c Claim
	err := s.DB.QueryRowContext(ctx,
		`SELECT name, sandbox_id, port, owner, created_at FROM preview_domains WHERE name = $1`, name).
		Scan(&c.Name, &c.SandboxID, &c.Port, &c.Owner, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Claim books name for target. Claiming a name you already hold moves it to
// the new target (the address survives a rebuilt workspace); a name held by
// somebody else is refused with ErrNameTaken.
func (s *ClaimStore) Claim(ctx context.Context, c Claim) error {
	if s == nil || s.DB == nil {
		return errors.New("preview domains are not configured")
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO preview_domains (name, sandbox_id, port, owner) VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE
			SET sandbox_id = EXCLUDED.sandbox_id, port = EXCLUDED.port
			WHERE preview_domains.owner = EXCLUDED.owner`,
		c.Name, c.SandboxID, c.Port, c.Owner)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNameTaken
	}
	return nil
}

// Release drops a claim. owner "" releases whoever holds it (admin paths).
func (s *ClaimStore) Release(ctx context.Context, name, owner string) (bool, error) {
	if s == nil || s.DB == nil {
		return false, nil
	}
	query := `DELETE FROM preview_domains WHERE name = $1`
	args := []any{name}
	if owner != "" {
		query += ` AND owner = $2`
		args = append(args, owner)
	}
	res, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// ValidateLabel reports why name cannot be a preview subdomain. Names are DNS
// labels below the preview zone, so they stay one level deep and out of the
// reserved set.
func ValidateLabel(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 63 {
		return errors.New("name must be 63 characters or fewer")
	}
	if strings.ContainsRune(name, '.') {
		return errors.New("name must be a single label without dots")
	}
	if !validLabel(name) {
		return errors.New("name may only contain a-z, 0-9 and inner hyphens")
	}
	if reservedLabels[name] {
		return fmt.Errorf("%q is reserved", name)
	}
	return nil
}

// validLabel reports whether s is a DNS label this zone can serve.
func validLabel(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

type claimReq struct {
	Name      string `json:"name"`
	SandboxID string `json:"sandboxID"`
	Port      int    `json:"port"`
}

type claimResp struct {
	Claim
	Address string `json:"address"`
	// URL and its expiry come with a fresh claim; listing claims mints nothing,
	// so the field is absent there.
	URL       string     `json:"url,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// claimDomain books a name for one sandbox port and answers with the
// ready-to-open link, so a caller never has to mint a token separately.
func (h *Handler) claimDomain(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Claims == nil || h.Domain == "" {
		httpx.WriteErr(w, http.StatusServiceUnavailable,
			"subdomain previews are not configured; set ROUNDPEN_PREVIEW_DOMAIN")
		return
	}

	var req claimReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if err := ValidateLabel(name); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid port")
		return
	}

	ctx := r.Context()
	sb, err := h.Manager.Get(ctx, req.SandboxID)
	if err != nil {
		httpx.WriteErr(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if sb.Owner != user.Username && user.Role != storage.RoleAdmin {
		httpx.WriteErr(w, http.StatusForbidden, "forbidden")
		return
	}
	// The automatic {id}-{port} address of a live sandbox is not a name to
	// book: claiming it would break that workspace's existing links.
	if id, _, ok := autoTarget(name); ok {
		if other, err := h.Manager.Get(ctx, id); err == nil && other != nil {
			httpx.WriteErr(w, http.StatusConflict,
				fmt.Sprintf("%q is the automatic address of an existing workspace; choose another name", name))
			return
		}
	}

	claim := Claim{Name: name, SandboxID: sb.ID, Port: req.Port, Owner: user.Username}
	if err := h.Claims.Claim(ctx, claim); err != nil {
		if errors.Is(err, ErrNameTaken) {
			httpx.WriteErr(w, http.StatusConflict, fmt.Sprintf("%q is already taken", name))
			return
		}
		httpx.WriteErr(w, http.StatusInternalServerError, "claim failed")
		return
	}
	if stored, err := h.Claims.Get(ctx, name); err == nil && stored != nil {
		claim = *stored
	}

	token, expires, err := h.Tokens.Issue(claim.SandboxID, claim.Port, claim.Owner)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, claimResp{
		Claim:     claim,
		Address:   name + "." + h.Domain,
		URL:       h.claimOrigin(name, r) + "/?token=" + token,
		ExpiresAt: &expires,
	})
}

// listDomains answers the caller's own claims (all of them for an admin).
func (h *Handler) listDomains(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := user.Username
	if user.Role == storage.RoleAdmin {
		owner = ""
	}
	claims, err := h.Claims.List(r.Context(), owner)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	out := make([]claimResp, 0, len(claims))
	for _, c := range claims {
		out = append(out, claimResp{Claim: c, Address: c.Name + "." + h.Domain})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"domains": out})
}

// releaseDomain frees a name. An admin may release anyone's.
func (h *Handler) releaseDomain(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	name := strings.ToLower(strings.TrimSpace(r.PathValue("name")))
	owner := user.Username
	if user.Role == storage.RoleAdmin {
		owner = ""
	}
	released, err := h.Claims.Release(r.Context(), name, owner)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "release failed")
		return
	}
	if !released {
		httpx.WriteErr(w, http.StatusNotFound, "no such domain")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"released": name})
}

// claimOrigin is the scheme://name.domain origin a claimed preview answers on.
func (h *Handler) claimOrigin(name string, r *http.Request) string {
	scheme := h.Scheme
	if scheme == "" {
		scheme = httpx.DefaultTrust.Scheme(r)
	}
	return scheme + "://" + name + "." + h.Domain
}
