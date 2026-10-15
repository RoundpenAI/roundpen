package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   ErrorClass
	}{
		{"unauthorized", 401, errors.New("boom"), ClassAuth},
		{"forbidden", 403, errors.New("boom"), ClassAuth},
		{"rate limited", 429, errors.New("boom"), ClassRateLimit},
		{"protocol", 500, errors.New("boom"), ClassProtocol},
		{"no status", 0, errors.New("boom"), ClassProtocol},
		{"deadline", 0, context.DeadlineExceeded, ClassNetwork},
	}
	for _, tc := range cases {
		rt := &bearerRT{lastStatus: tc.status}
		got := classifyError(tc.err, rt)
		if ClassOf(got) != tc.want {
			t.Errorf("%s: class = %s, want %s", tc.name, ClassOf(got), tc.want)
		}
	}
}

func TestClassifyErrorAlreadyClassified(t *testing.T) {
	orig := &MCPError{Class: ClassRateLimit, Msg: "slow down"}
	got := classifyError(orig, &bearerRT{lastStatus: 500})
	var me *MCPError
	if !errors.As(got, &me) || me.Class != ClassRateLimit {
		t.Fatalf("classified errors must pass through: %v", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("30"); d != 30*time.Second {
		t.Errorf("seconds: %v", d)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("empty: %v", d)
	}
	if d := parseRetryAfter("garbage"); d != 0 {
		t.Errorf("garbage: %v", d)
	}
	future := time.Now().Add(20 * time.Second).UTC()
	if d := parseRetryAfter(future.Format(http.TimeFormat)); d <= 0 || d > 20*time.Second {
		t.Errorf("http date: %v", d)
	}
}

func TestBearerRTInjectsAuthorization(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	rt := &bearerRT{
		base:   http.DefaultTransport,
		bearer: func(context.Context) (string, error) { return "tok-123", nil },
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got != "Bearer tok-123" {
		t.Fatalf("authorization = %q", got)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("original request must not be mutated")
	}
}

func TestBearerRTRecordsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	rt := &bearerRT{
		base:   http.DefaultTransport,
		bearer: func(context.Context) (string, error) { return "t", nil },
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	status, retryAfter := rt.snapshot()
	if status != 429 || retryAfter != 7*time.Second {
		t.Fatalf("status=%d retryAfter=%v", status, retryAfter)
	}
}

func TestContentText(t *testing.T) {
	res := &sdk.CallToolResult{Content: []sdk.Content{
		&sdk.TextContent{Text: "first"},
		&sdk.TextContent{Text: "second"},
	}}
	if got := contentText(res); got != "first\nsecond" {
		t.Fatalf("got %q", got)
	}
	structured := &sdk.CallToolResult{StructuredContent: map[string]any{"k": "v"}}
	if got := contentText(structured); got != `{"k":"v"}` {
		t.Fatalf("structured got %q", got)
	}
	empty := &sdk.CallToolResult{}
	if got := contentText(empty); got != "" {
		t.Fatalf("empty got %q", got)
	}
}

// fakeMCPHandler serves a real SDK server so the dialer speaks the actual
// Streamable HTTP protocol.
func fakeMCPHandler(t *testing.T, authOK func(r *http.Request) bool) http.Handler {
	t.Helper()
	server := sdk.NewServer(&sdk.Implementation{Name: "fake-mcp", Version: "test"}, nil)
	server.AddTool(&sdk.Tool{
		Name:        "echo",
		Description: "echo the message",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var args struct {
			Msg string `json:"msg"`
		}
		_ = json.Unmarshal(req.Params.Arguments, &args)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "echo: " + args.Msg}}}, nil
	})
	server.AddTool(&sdk.Tool{
		Name:        "fail",
		Description: "always fails at the tool level",
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{
			IsError: true,
			Content: []sdk.Content{&sdk.TextContent{Text: "tool exploded"}},
		}, nil
	})
	inner := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK != nil && !authOK(r) {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://example.test/prm"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

func TestSDKDialerListAndCall(t *testing.T) {
	authHeader := ""
	srv := httptest.NewServer(fakeMCPHandler(t, func(r *http.Request) bool {
		authHeader = r.Header.Get("Authorization")
		return r.Header.Get("Authorization") == "Bearer good-key"
	}))
	defer srv.Close()

	d := &SDKDialer{AllowLoopback: true}
	ctx := context.Background()
	conn, err := d.Dial(ctx, srv.URL, func(context.Context) (string, error) { return "good-key", nil })
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	tools, err := conn.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	if tools[0].Name != "echo" && tools[1].Name != "echo" {
		t.Fatalf("echo missing: %+v", tools)
	}
	for _, tool := range tools {
		if tool.Name == "echo" && len(tool.InputSchema) == 0 {
			t.Fatal("input schema not captured")
		}
	}

	res, err := conn.CallTool(ctx, "echo", json.RawMessage(`{"msg":"hi"}`))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.IsError || res.Text != "echo: hi" {
		t.Fatalf("result = %+v", res)
	}

	res, err = conn.CallTool(ctx, "fail", nil)
	if err != nil {
		t.Fatalf("tool-level failure must not be a transport error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Text, "tool exploded") {
		t.Fatalf("result = %+v", res)
	}
	if authHeader != "Bearer good-key" {
		t.Fatalf("auth header = %q", authHeader)
	}
}

func TestSDKDialerAuthFailure(t *testing.T) {
	srv := httptest.NewServer(fakeMCPHandler(t, func(*http.Request) bool { return false }))
	defer srv.Close()

	d := &SDKDialer{AllowLoopback: true}
	_, err := d.Dial(context.Background(), srv.URL, func(context.Context) (string, error) { return "bad", nil })
	if err == nil {
		t.Fatal("expected auth failure")
	}
	if ClassOf(err) != ClassAuth {
		t.Fatalf("class = %s, err = %v", ClassOf(err), err)
	}
}

func TestSDKDialerDialGuard(t *testing.T) {
	// Without AllowLoopback the shared SSRF guard must refuse 127.0.0.1.
	srv := httptest.NewServer(fakeMCPHandler(t, nil))
	defer srv.Close()

	d := &SDKDialer{}
	_, err := d.Dial(context.Background(), srv.URL, func(context.Context) (string, error) { return "k", nil })
	if err == nil {
		t.Fatal("loopback dial must be blocked by default")
	}
	if ClassOf(err) != ClassNetwork {
		t.Fatalf("class = %s, err = %v", ClassOf(err), err)
	}
}

func TestSDKDialerUnreachable(t *testing.T) {
	// A closed port on loopback with AllowLoopback=true surfaces a network error.
	d := &SDKDialer{AllowLoopback: true}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := d.Dial(ctx, "http://127.0.0.1:1/mcp", func(context.Context) (string, error) { return "k", nil })
	if err == nil {
		t.Fatal("expected network failure")
	}
	if ClassOf(err) != ClassNetwork {
		t.Fatalf("class = %s, err = %v", ClassOf(err), err)
	}
}

func TestMCPErrorString(t *testing.T) {
	e := &MCPError{Class: ClassAuth, Status: 401, Msg: "nope"}
	if !strings.Contains(e.Error(), "401") {
		t.Fatalf("error = %q", e.Error())
	}
	if !errors.Is(e, e) || e.Unwrap() != nil {
		t.Fatal("unwrap of a bare MCPError must be nil")
	}
}
