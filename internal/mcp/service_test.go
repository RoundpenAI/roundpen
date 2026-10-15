package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

type stubConnector struct {
	tools     []Tool
	listErr   error
	callText  string
	callIsErr bool
	callErr   error
	closed    bool
}

func (c *stubConnector) ListTools(context.Context) ([]Tool, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.tools, nil
}

func (c *stubConnector) CallTool(context.Context, string, json.RawMessage) (CallResult, error) {
	if c.callErr != nil {
		return CallResult{}, c.callErr
	}
	return CallResult{Text: c.callText, IsError: c.callIsErr}, nil
}

func (c *stubConnector) Close() error {
	c.closed = true
	return nil
}

type stubResult struct {
	conn *stubConnector
	err  error
}

type stubDialer struct {
	results    []stubResult
	dials      int
	endpoints  []string
	bearerSeen []string
	bearerErr  error
}

func (d *stubDialer) Dial(ctx context.Context, endpoint string, bearer func(context.Context) (string, error)) (Connector, error) {
	idx := d.dials
	d.dials++
	d.endpoints = append(d.endpoints, endpoint)
	tok, err := bearer(ctx)
	if err != nil {
		d.bearerErr = err
	} else {
		d.bearerSeen = append(d.bearerSeen, tok)
	}
	if idx < len(d.results) {
		return d.results[idx].conn, d.results[idx].err
	}
	if len(d.results) > 0 {
		return d.results[len(d.results)-1].conn, d.results[len(d.results)-1].err
	}
	return nil, errors.New("no stub result")
}

func newTestService(t *testing.T) (*Service, *stubDialer, *storage.DB) {
	t.Helper()
	store, db := pgStore(t)
	dialer := &stubDialer{}
	svc := &Service{Store: store, Dialer: dialer, HTTP: &http.Client{Timeout: 5 * time.Second}}
	return svc, dialer, db
}

func TestConnectAPIKeyStoresSnapshot(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-user")

	dialer.results = append(dialer.results, stubResult{&stubConnector{tools: []Tool{{Name: "web_search"}}}, nil})

	conn, err := svc.ConnectAPIKey(ctx, user, "exa", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if conn.AuthKind != AuthKindAPIKey || conn.Status != StatusConnected || len(conn.Tools) != 1 {
		t.Fatalf("conn = %+v", conn)
	}
	if conn.APIKey != "key-1" {
		t.Fatalf("api key = %q", conn.APIKey)
	}
	if dialer.bearerSeen[0] != "key-1" {
		t.Fatalf("bearer = %v", dialer.bearerSeen)
	}
	if dialer.endpoints[0] != "https://mcp.exa.ai/mcp" {
		t.Fatalf("endpoint = %q", dialer.endpoints[0])
	}
}

func TestConnectAPIKeyRejects(t *testing.T) {
	ctx := context.Background()
	svc, _, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-reject")

	if _, err := svc.ConnectAPIKey(ctx, user, "unknown", "k"); !errors.Is(err, ErrServerUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	// notion is OAuth-only.
	if _, err := svc.ConnectAPIKey(ctx, user, "notion", "k"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("oauth-only: %v", err)
	}
	if _, err := svc.ConnectAPIKey(ctx, user, "exa", "   "); err == nil {
		t.Fatal("empty key must fail")
	}
}

func TestConnectAPIKeyBadCredentialDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-badkey")

	dialer.results = append(dialer.results, stubResult{nil, &MCPError{Class: ClassAuth, Status: 401, Msg: "bad key"}})

	_, err := svc.ConnectAPIKey(ctx, user, "exa", "wrong")
	if err == nil || ClassOf(err) != ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.Store.GetConnection(ctx, user, "exa"); err == nil {
		t.Fatal("connection must not be stored for a failed probe")
	}
}

func TestRefreshToolsUpdatesSnapshot(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-refresh")

	dialer.results = append(dialer.results, stubResult{&stubConnector{tools: []Tool{{Name: "one"}}}, nil})
	conn, err := svc.ConnectAPIKey(ctx, user, "exa", "k")
	if err != nil {
		t.Fatal(err)
	}
	if conn.ToolsSyncedAt == nil {
		t.Fatal("tools_synced_at not set")
	}

	dialer.results = append(dialer.results, stubResult{&stubConnector{tools: []Tool{{Name: "one"}, {Name: "two"}}}, nil})
	updated, err := svc.RefreshTools(ctx, user, "exa")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Tools) != 2 {
		t.Fatalf("tools = %+v", updated.Tools)
	}

	// A refresh failure flips status to error but keeps the snapshot.
	dialer.results = append(dialer.results, stubResult{nil, &MCPError{Class: ClassNetwork, Msg: "down"}})
	if _, err := svc.RefreshTools(ctx, user, "exa"); err == nil {
		t.Fatal("expected refresh failure")
	}
	after, err := svc.Store.GetConnection(ctx, user, "exa")
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusError || len(after.Tools) != 2 {
		t.Fatalf("after = %+v", after)
	}

	// Refreshing a server that is not connected reports ErrNotConnected.
	if _, err := svc.RefreshTools(ctx, user, "stripe"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("not connected: %v", err)
	}
}

func TestCallToolAndToolsForSession(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-call")

	dialer.results = append(dialer.results, stubResult{&stubConnector{tools: []Tool{{Name: "web_search", Description: "search"}}}, nil})
	if _, err := svc.ConnectAPIKey(ctx, user, "exa", "k"); err != nil {
		t.Fatal(err)
	}

	sessions, err := svc.ToolsForSession(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ServerID != "exa" || sessions[0].ServerName != "Exa" || len(sessions[0].Tools) != 1 {
		t.Fatalf("sessions = %+v", sessions)
	}

	dialer.results = append(dialer.results, stubResult{&stubConnector{callText: "result text"}, nil})
	res, err := svc.CallTool(ctx, user, "exa", "web_search", json.RawMessage(`{"q":"x"}`))
	if err != nil || res.Text != "result text" {
		t.Fatalf("call = %+v, %v", res, err)
	}

	// Not connected → ErrNotConnected.
	if _, err := svc.CallTool(ctx, user, "stripe", "t", nil); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("not connected: %v", err)
	}
}

func TestCallToolNetworkErrorMarksConnection(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-net")

	dialer.results = append(dialer.results, stubResult{&stubConnector{tools: []Tool{{Name: "t"}}}, nil})
	if _, err := svc.ConnectAPIKey(ctx, user, "exa", "k"); err != nil {
		t.Fatal(err)
	}

	dialer.results = append(dialer.results, stubResult{nil, &MCPError{Class: ClassNetwork, Msg: "unreachable"}})
	_, err := svc.CallTool(ctx, user, "exa", "t", nil)
	if err == nil || ClassOf(err) != ClassNetwork {
		t.Fatalf("err = %v", err)
	}
	conn, err := svc.Store.GetConnection(ctx, user, "exa")
	if err != nil || conn.Status != StatusError {
		t.Fatalf("status = %+v, %v", conn, err)
	}
}

func TestCallToolRateLimitKeepsConnectionHealthy(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-rl")

	dialer.results = append(dialer.results, stubResult{&stubConnector{tools: []Tool{{Name: "t"}}}, nil})
	if _, err := svc.ConnectAPIKey(ctx, user, "exa", "k"); err != nil {
		t.Fatal(err)
	}

	dialer.results = append(dialer.results, stubResult{nil, &MCPError{Class: ClassRateLimit, Status: 429, RetryAfter: 5 * time.Second, Msg: "slow"}})
	_, err := svc.CallTool(ctx, user, "exa", "t", nil)
	if err == nil || ClassOf(err) != ClassRateLimit {
		t.Fatalf("err = %v", err)
	}
	conn, err := svc.Store.GetConnection(ctx, user, "exa")
	if err != nil || conn.Status != StatusConnected {
		t.Fatalf("rate limits must not degrade status: %+v, %v", conn, err)
	}
}

func TestCallToolRefreshesExpiredToken(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-oauth")

	// Fake token endpoint that rotates the access token.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "rt-1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-2", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	svc.HTTP = tokenSrv.Client()

	past := time.Now().Add(-time.Minute).UTC()
	conn := Connection{
		UserID: user, ServerID: "notion", AuthKind: AuthKindOAuth, Status: StatusConnected,
		AccessToken: "at-1", RefreshToken: "rt-1", TokenExpiresAt: &past,
		TokenEndpoint: tokenSrv.URL, ClientID: "cid", Resource: "https://mcp.notion.com",
	}
	if _, err := svc.Store.UpsertConnection(ctx, conn); err != nil {
		t.Fatal(err)
	}

	dialer.results = append(dialer.results, stubResult{&stubConnector{callText: "ok"}, nil})
	res, err := svc.CallTool(ctx, user, "notion", "search", nil)
	if err != nil || res.Text != "ok" {
		t.Fatalf("call = %+v, %v", res, err)
	}
	if len(dialer.bearerSeen) != 1 || dialer.bearerSeen[0] != "at-2" {
		t.Fatalf("bearer = %v", dialer.bearerSeen)
	}
	stored, err := svc.Store.GetConnection(ctx, user, "notion")
	if err != nil || stored.AccessToken != "at-2" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}

func TestCallToolAuthFailureRetriesOnceThenNeedsReauth(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-reauth")

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-2", "expires_in": 3600})
	}))
	defer tokenSrv.Close()
	svc.HTTP = tokenSrv.Client()

	future := time.Now().Add(time.Hour).UTC()
	conn := Connection{
		UserID: user, ServerID: "notion", AuthKind: AuthKindOAuth, Status: StatusConnected,
		AccessToken: "at-1", RefreshToken: "rt-1", TokenExpiresAt: &future,
		TokenEndpoint: tokenSrv.URL, ClientID: "cid",
	}
	if _, err := svc.Store.UpsertConnection(ctx, conn); err != nil {
		t.Fatal(err)
	}

	// First attempt 401s; the forced refresh retry succeeds.
	dialer.results = append(dialer.results,
		stubResult{nil, &MCPError{Class: ClassAuth, Status: 401, Msg: "expired"}},
		stubResult{&stubConnector{callText: "recovered"}, nil},
	)
	res, err := svc.CallTool(ctx, user, "notion", "search", nil)
	if err != nil || res.Text != "recovered" {
		t.Fatalf("call = %+v, %v", res, err)
	}
	if dialer.dials != 2 {
		t.Fatalf("dials = %d, want 2", dialer.dials)
	}

	// Both attempts 401 → needs_reauth.
	dialer.results = dialer.results[:0]
	dialer.results = append(dialer.results,
		stubResult{nil, &MCPError{Class: ClassAuth, Status: 401, Msg: "expired"}},
		stubResult{nil, &MCPError{Class: ClassAuth, Status: 401, Msg: "expired"}},
	)
	if _, err := svc.CallTool(ctx, user, "notion", "search", nil); !errors.Is(err, ErrNeedsReauth) {
		t.Fatalf("err = %v", err)
	}
	stored, err := svc.Store.GetConnection(ctx, user, "notion")
	if err != nil || stored.Status != StatusNeedsReauth {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	// A connection already flagged needs_reauth fails without dialing.
	if _, err := svc.CallTool(ctx, user, "notion", "search", nil); !errors.Is(err, ErrNeedsReauth) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefreshAccessTokenInvalidGrant(t *testing.T) {
	ctx := context.Background()
	svc, _, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-invalid")

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
	}))
	defer tokenSrv.Close()
	svc.HTTP = tokenSrv.Client()

	past := time.Now().Add(-time.Minute).UTC()
	saved, err := svc.Store.UpsertConnection(ctx, Connection{
		UserID: user, ServerID: "notion", AuthKind: AuthKindOAuth, Status: StatusConnected,
		AccessToken: "at-1", RefreshToken: "rt-1", TokenExpiresAt: &past,
		TokenEndpoint: tokenSrv.URL, ClientID: "cid",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.refreshAccessToken(ctx, saved, false)
	if !errors.Is(err, ErrNeedsReauth) {
		t.Fatalf("err = %v", err)
	}
	after, err := svc.Store.GetConnection(ctx, user, "notion")
	if err != nil || after.Status != StatusNeedsReauth {
		t.Fatalf("after = %+v, %v", after, err)
	}
}

func TestToolsForSessionSkipsUnhealthy(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-skip")

	// Empty snapshot: connected but nothing to register.
	if _, err := svc.Store.UpsertConnection(ctx, Connection{
		UserID: user, ServerID: "exa", AuthKind: AuthKindAPIKey, Status: StatusConnected, APIKey: "k",
	}); err != nil {
		t.Fatal(err)
	}
	// needs_reauth with tools: still skipped.
	if _, err := svc.Store.UpsertConnection(ctx, Connection{
		UserID: user, ServerID: "notion", AuthKind: AuthKindOAuth, Status: StatusNeedsReauth,
		Tools: []Tool{{Name: "x"}},
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err := svc.ToolsForSession(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %+v", sessions)
	}
	_ = dialer
}

func TestDisconnect(t *testing.T) {
	ctx := context.Background()
	svc, dialer, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-disc")

	dialer.results = append(dialer.results, stubResult{&stubConnector{tools: []Tool{{Name: "t"}}}, nil})
	if _, err := svc.ConnectAPIKey(ctx, user, "exa", "k"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Disconnect(ctx, user, "exa"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.GetConnection(ctx, user, "exa"); err == nil {
		t.Fatal("connection must be gone")
	}
	// Disconnecting an absent connection is a no-op.
	if err := svc.Disconnect(ctx, user, "exa"); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogMerge(t *testing.T) {
	ctx := context.Background()
	svc, _, db := newTestService(t)
	user := pgUser(t, db, "mcp-svc-catalog")

	if _, err := svc.Store.UpsertConnection(ctx, Connection{
		UserID: user, ServerID: "linear", AuthKind: AuthKindOAuth, Status: StatusNeedsReauth,
	}); err != nil {
		t.Fatal(err)
	}
	items, err := svc.Catalog(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 14 {
		t.Fatalf("items = %d", len(items))
	}
	for _, item := range items {
		if item.ID == "linear" {
			if !item.Connected || item.Status != StatusNeedsReauth {
				t.Fatalf("linear = %+v", item)
			}
		} else if item.Connected {
			t.Fatalf("%s must not be connected", item.ID)
		}
	}
}
