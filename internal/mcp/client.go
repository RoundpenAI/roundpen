package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/RoundpenAI/roundpen/internal/httpx"
)

// clientVersion is reported to servers in the MCP initialize handshake.
const clientVersion = "0.1.0"

const (
	dialTimeout = 15 * time.Second
	callTimeout = 60 * time.Second
)

// Tool is the minimal projection of a server tool the rest of Roundpen uses.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// CallResult is a tool call outcome. IsError marks tool-level failures the
// model should see and may recover from.
type CallResult struct {
	Text    string
	IsError bool
}

// ErrorClass buckets failures for readable tool errors and status updates.
type ErrorClass string

const (
	// ClassNetwork covers transport failures (unreachable, timeout).
	ClassNetwork ErrorClass = "network"
	// ClassAuth covers 401/403 and invalidated grants.
	ClassAuth ErrorClass = "auth"
	// ClassRateLimit covers 429 responses.
	ClassRateLimit ErrorClass = "rate_limit"
	// ClassTool covers tool-level errors reported by the server.
	ClassTool ErrorClass = "tool"
	// ClassProtocol covers everything else (handshake, malformed responses).
	ClassProtocol ErrorClass = "protocol"
)

// MCPError is a classified remote MCP failure.
type MCPError struct {
	Class      ErrorClass
	Status     int
	RetryAfter time.Duration
	Msg        string
	err        error
}

func (e *MCPError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("%s (HTTP %d)", e.Msg, e.Status)
	}
	return e.Msg
}

func (e *MCPError) Unwrap() error { return e.err }

// ClassOf returns the error class, defaulting to ClassProtocol for foreign errors.
func ClassOf(err error) ErrorClass {
	var me *MCPError
	if errors.As(err, &me) {
		return me.Class
	}
	return ClassProtocol
}

// Connector is one live MCP connection.
type Connector interface {
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error)
	Close() error
}

// Dialer opens a Connector against an MCP endpoint. bearer is called for every
// HTTP request and returns the credential to send (may refresh internally).
type Dialer interface {
	Dial(ctx context.Context, endpoint string, bearer func(ctx context.Context) (string, error)) (Connector, error)
}

// SDKDialer connects over the Streamable HTTP transport. The standalone SSE
// stream is disabled (request/response only) and transport-level retries are
// off: the service layer owns refresh-and-retry. Outbound dials go through the
// shared SSRF guard (loopback and private ranges blocked); AllowLoopback is
// for httptest-based tests only.
type SDKDialer struct {
	AllowLoopback bool
}

func (d *SDKDialer) Dial(ctx context.Context, endpoint string, bearer func(ctx context.Context) (string, error)) (Connector, error) {
	rt := &bearerRT{base: httpx.GuardedTransport(d.AllowLoopback, false), bearer: bearer}
	hc := &http.Client{Transport: rt}
	client := sdk.NewClient(&sdk.Implementation{Name: "roundpen", Version: clientVersion}, nil)
	transport := &sdk.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           hc,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, classifyError(err, rt)
	}
	return &sdkConnector{session: session, rt: rt}, nil
}

type sdkConnector struct {
	session *sdk.ClientSession
	rt      *bearerRT
}

func (c *sdkConnector) ListTools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	cursor := ""
	for {
		res, err := c.session.ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, classifyError(err, c.rt)
		}
		for _, t := range res.Tools {
			tool := Tool{Name: t.Name, Description: t.Description}
			if t.InputSchema != nil {
				if raw, err := json.Marshal(t.InputSchema); err == nil {
					tool.InputSchema = raw
				}
			}
			out = append(out, tool)
		}
		cursor = res.NextCursor
		if cursor == "" {
			return out, nil
		}
	}
}

func (c *sdkConnector) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	var arguments any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &arguments); err != nil {
			return CallResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	res, err := c.session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return CallResult{}, classifyError(err, c.rt)
	}
	return CallResult{Text: contentText(res), IsError: res.IsError}, nil
}

func (c *sdkConnector) Close() error { return c.session.Close() }

// contentText flattens the result content to text; non-text blocks keep their
// JSON form so the model can see that an image or resource was returned.
func contentText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*sdk.TextContent); ok {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(t.Text)
			continue
		}
		if raw, err := json.Marshal(c); err == nil {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.Write(raw)
		}
	}
	if b.Len() == 0 && res.StructuredContent != nil {
		if raw, err := json.Marshal(res.StructuredContent); err == nil {
			b.Write(raw)
		}
	}
	return b.String()
}

// bearerRT injects the Authorization header and records the last response
// status so failures can be classified even though the SDK folds HTTP errors
// into strings. On 4xx/5xx it also peeks a short JSON message for the UI.
type bearerRT struct {
	base   http.RoundTripper
	bearer func(ctx context.Context) (string, error)

	mu             sync.Mutex
	lastStatus     int
	lastRetryAfter time.Duration
	lastBodyHint   string
}

func (t *bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.bearer(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	if tok != "" {
		clone.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := t.base.RoundTrip(clone)
	if err != nil {
		return nil, err
	}
	hint := ""
	if resp.StatusCode >= 400 {
		hint = peekJSONMessage(resp)
	}
	t.mu.Lock()
	t.lastStatus = resp.StatusCode
	t.lastRetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
	t.lastBodyHint = hint
	t.mu.Unlock()
	return resp, nil
}

func (t *bearerRT) snapshot() (status int, retryAfter time.Duration, bodyHint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastStatus, t.lastRetryAfter, t.lastBodyHint
}

// peekJSONMessage reads a small prefix of an error body for {"message":"..."}
// without consuming the stream for the SDK (TeeReader + NopCloser restore).
func peekJSONMessage(resp *http.Response) string {
	const max = 2048
	buf := make([]byte, max)
	n, _ := io.ReadFull(resp.Body, buf)
	buf = buf[:n]
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), resp.Body))
	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(buf, &payload); err != nil {
		return ""
	}
	if m := strings.TrimSpace(payload.Message); m != "" {
		return m
	}
	return strings.TrimSpace(payload.Error)
}

// classifyError wraps err with the best class we can infer. The recorded HTTP
// status is authoritative for auth/rate-limit; network errors are detected via
// the error chain; everything else is protocol.
func classifyError(err error, rt *bearerRT) error {
	if err == nil {
		return nil
	}
	var me *MCPError
	if errors.As(err, &me) {
		return err
	}
	status, retryAfter, bodyHint := 0, time.Duration(0), ""
	if rt != nil {
		status, retryAfter, bodyHint = rt.snapshot()
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		msg := "authentication with the MCP server failed or expired"
		if bodyHint != "" {
			msg = bodyHint
		}
		return &MCPError{
			Class:  ClassAuth,
			Status: status,
			Msg:    msg,
			err:    err,
		}
	case status == http.StatusTooManyRequests:
		return &MCPError{
			Class:      ClassRateLimit,
			Status:     status,
			RetryAfter: retryAfter,
			Msg:        "the MCP server rate limited the request",
			err:        err,
		}
	}
	if isNetworkErr(err) {
		return &MCPError{Class: ClassNetwork, Msg: "the MCP server is unreachable", err: err}
	}
	return &MCPError{Class: ClassProtocol, Msg: "MCP server error: " + truncateErr(err.Error()), err: err}
}

func isNetworkErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func truncateErr(s string) string {
	const max = 240
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max] + "…"
	}
	if s == "" {
		return "empty error"
	}
	return s
}
