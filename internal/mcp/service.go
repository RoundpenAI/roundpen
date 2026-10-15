package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

var (
	// ErrServerUnknown means the id is not in the allowlisted catalog.
	ErrServerUnknown = errors.New("unknown MCP server")
	// ErrNotConnected means there is no connection for (user, server).
	ErrNotConnected = errors.New("this MCP server is not connected; open Settings → MCP and connect it")
	// ErrNotSupported means the server does not accept the requested auth kind.
	ErrNotSupported = errors.New("this MCP server does not support that connection method")
	// ErrNeedsReauth means the stored grant no longer works and the user must reconnect.
	ErrNeedsReauth = errors.New("the MCP connection expired; open Settings → MCP and reconnect")
	// ErrStateInvalid covers missing, expired and replayed OAuth states.
	ErrStateInvalid = errors.New("authorization state is invalid or expired")
	// ErrDenied is a callback that carries an authorization error.
	ErrDenied = errors.New("authorization was denied")
	// ErrMissingCode is a callback without an authorization code.
	ErrMissingCode = errors.New("missing authorization code")
	// ErrDCRUnsupported means the server has no registration endpoint, so
	// Roundpen cannot self-register an OAuth client (yet).
	ErrDCRUnsupported = errors.New("this MCP server does not support automatic client registration; OAuth is not available yet")
)

const (
	defaultStateTTL = 10 * time.Minute
	refreshWindow   = 5 * time.Minute
	httpTimeout     = 20 * time.Second
)

// Service runs the MCP catalog, per-user connections and tool calls.
type Service struct {
	Store  *Store
	HTTP   *http.Client
	Now    func() time.Time
	TTL    time.Duration
	Dialer Dialer
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) stateTTL() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return defaultStateTTL
}

func (s *Service) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{
		Transport: httpx.GuardedTransport(false, false),
		Timeout:   httpTimeout,
	}
}

func (s *Service) dialer() Dialer {
	if s.Dialer != nil {
		return s.Dialer
	}
	return &SDKDialer{}
}

// CatalogItem is one catalog entry merged with the user's connection state.
type CatalogItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Endpoint     string   `json:"endpoint"`
	Auth         AuthMode `json:"auth"`
	Group        string   `json:"group"`
	Description  string   `json:"description"`
	DocsURL      string   `json:"docsUrl"`
	Connected    bool     `json:"connected"`
	Status       string   `json:"status,omitempty"`
	AccountLabel string   `json:"accountLabel,omitempty"`
}

// ServerTools is one connected server's tool snapshot for a session.
type ServerTools struct {
	ServerID    string
	ServerName  string
	Description string
	Tools       []Tool
}

// Catalog returns the allowlist merged with the user's connection states.
func (s *Service) Catalog(ctx context.Context, userID string) ([]CatalogItem, error) {
	conns, err := s.Store.ListConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	byServer := make(map[string]Connection, len(conns))
	for _, c := range conns {
		byServer[c.ServerID] = c
	}
	list := Catalog()
	out := make([]CatalogItem, 0, len(list))
	for _, sv := range list {
		item := CatalogItem{
			ID: sv.ID, Name: sv.Name, Endpoint: sv.Endpoint, Auth: sv.Auth,
			Group: sv.Group, Description: sv.Description, DocsURL: sv.DocsURL,
		}
		if c, ok := byServer[sv.ID]; ok {
			item.Connected = true
			item.Status = c.Status
			item.AccountLabel = c.AccountLabel
		}
		out = append(out, item)
	}
	return out, nil
}

// ConnectAPIKey stores an API key connection after verifying it against the
// server (a successful tools/list is required).
func (s *Service) ConnectAPIKey(ctx context.Context, userID, serverID, apiKey string) (*Connection, error) {
	sv, ok := Lookup(serverID)
	if !ok {
		return nil, ErrServerUnknown
	}
	if sv.Auth == AuthOAuth {
		return nil, ErrNotSupported
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	conn := Connection{
		UserID:   userID,
		ServerID: sv.ID,
		AuthKind: AuthKindAPIKey,
		Status:   StatusConnected,
		APIKey:   apiKey,
	}
	tools, err := s.probe(ctx, sv.Endpoint, s.staticBearer(apiKey))
	if err != nil {
		return nil, err
	}
	now := s.now()
	conn.Tools = tools
	conn.ToolsSyncedAt = &now
	saved, err := s.Store.UpsertConnection(ctx, conn)
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// Disconnect removes the connection. Revocation is best-effort (RFC 7009 is
// not universally supported); failures never block the removal.
func (s *Service) Disconnect(ctx context.Context, userID, serverID string) error {
	conn, err := s.Store.GetConnection(ctx, userID, serverID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}
	if conn.AuthKind == AuthKindOAuth {
		s.revoke(ctx, conn)
	}
	if err := s.Store.DeleteConnection(ctx, userID, serverID); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	return nil
}

// RefreshTools re-enumerates the server's tools and stores the snapshot.
func (s *Service) RefreshTools(ctx context.Context, userID, serverID string) (*Connection, error) {
	conn, err := s.Store.GetConnection(ctx, userID, serverID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrNotConnected
		}
		return nil, err
	}
	sv, ok := Lookup(conn.ServerID)
	if !ok {
		return nil, ErrServerUnknown
	}
	tools, err := s.probe(ctx, sv.Endpoint, s.bearer(ctx, conn))
	if err != nil {
		s.markError(ctx, conn, err)
		return nil, err
	}
	now := s.now()
	if err := s.Store.SaveTools(ctx, conn.ID, tools, now); err != nil {
		return nil, err
	}
	if conn.Status != StatusConnected || conn.LastError != "" {
		if err := s.Store.SetStatus(ctx, conn.ID, StatusConnected, ""); err != nil {
			return nil, err
		}
	}
	return s.Store.GetConnection(ctx, userID, serverID)
}

// ToolsForSession returns tool snapshots for the user's healthy connections.
func (s *Service) ToolsForSession(ctx context.Context, userID string) ([]ServerTools, error) {
	conns, err := s.Store.ListConnections(ctx, userID)
	if err != nil {
		return nil, err
	}
	var out []ServerTools
	for _, c := range conns {
		if c.Status != StatusConnected || len(c.Tools) == 0 {
			continue
		}
		sv, ok := Lookup(c.ServerID)
		if !ok {
			continue
		}
		out = append(out, ServerTools{
			ServerID:    sv.ID,
			ServerName:  sv.Name,
			Description: sv.Description,
			Tools:       c.Tools,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ServerID < out[j].ServerID })
	return out, nil
}

// CallTool dials the server and calls one tool. OAuth connections get one
// force-refresh retry on an auth failure.
func (s *Service) CallTool(ctx context.Context, userID, serverID, toolName string, args json.RawMessage) (CallResult, error) {
	conn, err := s.Store.GetConnection(ctx, userID, serverID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return CallResult{}, ErrNotConnected
		}
		return CallResult{}, err
	}
	if conn.Status == StatusNeedsReauth {
		return CallResult{}, ErrNeedsReauth
	}
	sv, ok := Lookup(conn.ServerID)
	if !ok {
		return CallResult{}, ErrServerUnknown
	}
	res, err := s.callOnce(ctx, sv.Endpoint, conn, toolName, args, false)
	if err != nil && ClassOf(err) == ClassAuth && conn.AuthKind == AuthKindOAuth && conn.RefreshToken != "" {
		res, retryErr := s.callOnce(ctx, sv.Endpoint, conn, toolName, args, true)
		if retryErr == nil {
			return res, nil
		}
		err = retryErr
	}
	if err != nil {
		switch ClassOf(err) {
		case ClassAuth:
			_ = s.Store.SetStatus(ctx, conn.ID, StatusNeedsReauth, err.Error())
			return CallResult{}, ErrNeedsReauth
		case ClassNetwork, ClassRateLimit:
			s.markError(ctx, conn, err)
		}
		return CallResult{}, err
	}
	if conn.Status != StatusConnected {
		_ = s.Store.SetStatus(ctx, conn.ID, StatusConnected, "")
	}
	return res, nil
}

func (s *Service) callOnce(ctx context.Context, endpoint string, conn *Connection, toolName string, args json.RawMessage, forceRefresh bool) (CallResult, error) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var bearer func(context.Context) (string, error)
	if forceRefresh && conn.AuthKind == AuthKindOAuth {
		bearer = func(context.Context) (string, error) { return s.refreshAccessToken(cctx, conn, true) }
	} else {
		bearer = s.bearer(cctx, conn)
	}
	dialer := s.dialer()
	dctx, dcancel := context.WithTimeout(cctx, dialTimeout)
	rc, err := dialer.Dial(dctx, endpoint, bearer)
	dcancel()
	if err != nil {
		return CallResult{}, err
	}
	defer rc.Close()
	return rc.CallTool(cctx, toolName, args)
}

// probe dials and lists tools; used to verify credentials before storing them.
func (s *Service) probe(ctx context.Context, endpoint string, bearer func(context.Context) (string, error)) ([]Tool, error) {
	dialer := s.dialer()
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	rc, err := dialer.Dial(dctx, endpoint, bearer)
	cancel()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	lctx, lcancel := context.WithTimeout(ctx, callTimeout)
	defer lcancel()
	return rc.ListTools(lctx)
}

// staticBearer returns a bearer func for a connection that needs no refresh
// (API keys, or an already-loaded token).
func (s *Service) staticBearer(token string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		if token == "" {
			return "", ErrNotConnected
		}
		return token, nil
	}
}

// bearer resolves the credential for every HTTP request of a session.
func (s *Service) bearer(ctx context.Context, conn *Connection) func(context.Context) (string, error) {
	if conn.AuthKind == AuthKindAPIKey {
		return s.staticBearer(conn.APIKey)
	}
	return func(context.Context) (string, error) {
		return s.refreshAccessToken(ctx, conn, false)
	}
}

// refreshAccessToken returns a usable access token, refreshing within the
// window. force skips the freshness check (auth-failure retry path).
func (s *Service) refreshAccessToken(ctx context.Context, conn *Connection, force bool) (string, error) {
	if conn.AccessToken == "" {
		return "", ErrNeedsReauth
	}
	if !force && !s.needsRefresh(conn) {
		return conn.AccessToken, nil
	}
	if strings.TrimSpace(conn.RefreshToken) == "" {
		return conn.AccessToken, nil
	}
	tok, err := RefreshToken(ctx, s.httpClient(), conn.TokenEndpoint, conn.ClientID, conn.ClientSecret, conn.RefreshToken, conn.Resource)
	if err != nil {
		if IsInvalidGrant(err) {
			_ = s.Store.SetStatus(ctx, conn.ID, StatusNeedsReauth, "refresh token rejected")
			return "", ErrNeedsReauth
		}
		// Transient failure: keep using the current token when we have one.
		return conn.AccessToken, nil
	}
	if err := s.Store.SaveTokens(ctx, conn.ID, tok.AccessToken, tok.RefreshToken, tok.ExpiresAt); err != nil {
		return "", err
	}
	conn.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		conn.RefreshToken = tok.RefreshToken
	}
	conn.TokenExpiresAt = tok.ExpiresAt
	return tok.AccessToken, nil
}

func (s *Service) needsRefresh(conn *Connection) bool {
	if conn.TokenExpiresAt == nil {
		return false
	}
	return !conn.TokenExpiresAt.After(s.now().Add(refreshWindow))
}

// markError records a degraded status without flipping healthy calls to it
// (only network/rate-limit failures land here).
func (s *Service) markError(ctx context.Context, conn *Connection, err error) {
	if conn.Status == StatusNeedsReauth {
		return
	}
	var me *MCPError
	msg := err.Error()
	if errors.As(err, &me) && me.Class == ClassRateLimit {
		// Rate limits are transient; keep the connection healthy.
		return
	}
	_ = s.Store.SetStatus(ctx, conn.ID, StatusError, truncateErr(msg))
	conn.Status = StatusError
}

// revoke asks the authorization server to drop the grant (best effort).
func (s *Service) revoke(ctx context.Context, conn *Connection) {
	if conn.ClientID == "" {
		return
	}
	meta, err := FetchASMetadata(ctx, s.httpClient(), issuerFromTokenEndpoint(conn.TokenEndpoint))
	if err != nil || meta.RevocationEndpoint == "" {
		return
	}
	RevokeToken(ctx, s.httpClient(), meta.RevocationEndpoint, conn.ClientID, conn.ClientSecret, conn.AccessToken)
}
