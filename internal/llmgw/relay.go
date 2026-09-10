package llmgw

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func (g *Gateway) serveAnthropic(w http.ResponseWriter, r *http.Request) {
	g.forward(w, r, ProviderAnthropic)
}

func (g *Gateway) serveOpenAI(w http.ResponseWriter, r *http.Request) {
	g.forward(w, r, ProviderOpenAI)
}

func (g *Gateway) forward(w http.ResponseWriter, r *http.Request, provider string) {
	start := time.Now().UTC()
	requestID := newRequestID()
	logLimit := g.bodyLogLimit()
	ctx := r.Context()

	if !g.Enabled() {
		http.Error(w, "llm gateway disabled", http.StatusServiceUnavailable)
		return
	}

	vk, errMsg := g.authenticate(r)
	if errMsg != "" {
		g.logTransaction(r.Context(), Transaction{
			RequestID:  requestID,
			Provider:   provider,
			Method:     r.Method,
			Path:       r.URL.Path,
			StatusCode: http.StatusUnauthorized,
			Error:      errMsg,
			CreatedAt:  start,
			DurationMS: time.Since(start).Milliseconds(),
		}, nil)
		http.Error(w, errMsg, http.StatusUnauthorized)
		return
	}

	upstream, err := g.store.GetUpstream(ctx, provider)
	if errors.Is(err, storage.ErrNotFound) {
		msg := provider + " upstream not configured"
		g.logTransaction(ctx, Transaction{
			RequestID: requestID, VirtualKey: vk.Key, VirtualName: vk.Name,
			Provider: provider, Method: r.Method, Path: r.URL.Path,
			StatusCode: http.StatusServiceUnavailable, Error: msg,
			CreatedAt: start, DurationMS: time.Since(start).Milliseconds(),
		}, nil)
		http.Error(w, msg, http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		g.logger.Error("llmgw get upstream", "err", err, "provider", provider)
		http.Error(w, "upstream lookup failed", http.StatusInternalServerError)
		return
	}

	matcher, err := NewModelMatcher(upstream.ModelMap, upstream.ModelPatterns)
	if err != nil {
		g.logTransaction(ctx, Transaction{
			RequestID: requestID, VirtualKey: vk.Key, VirtualName: vk.Name,
			Provider: provider, Method: r.Method, Path: r.URL.Path,
			StatusCode: http.StatusInternalServerError, Error: "model map: " + err.Error(),
			CreatedAt: start, DurationMS: time.Since(start).Milliseconds(),
		}, nil)
		http.Error(w, "invalid model map config", http.StatusInternalServerError)
		return
	}

	prefix := "/llmgw/" + provider
	path := strings.TrimPrefix(r.URL.Path, prefix)
	if path == "" {
		path = "/"
	}

	targetURL := joinUpstreamURL(upstream.BaseURL, path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	var reqRaw []byte
	var reqLog TransactionBodies
	if r.Body != nil {
		raw, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			g.logTransaction(ctx, Transaction{
				RequestID: requestID, VirtualKey: vk.Key, VirtualName: vk.Name,
				Provider: provider, Method: r.Method, Path: r.URL.Path,
				StatusCode: http.StatusBadRequest, Error: "read request body: " + err.Error(),
				CreatedAt: start, DurationMS: time.Since(start).Milliseconds(),
			}, nil)
			http.Error(w, "read request body", http.StatusBadRequest)
			return
		}
		reqRaw = raw
		if logLimit > 0 {
			reqLog.Request, reqLog.RequestTruncated = truncateForLog(raw, logLimit)
		}
	}

	upstreamBody := reqRaw
	defaultModel := g.defaultModelName()
	if (matcher.Enabled() || defaultModel != "") && len(reqRaw) > 0 {
		mapped, err := mapModel(reqRaw, matcher, defaultModel)
		if err != nil {
			g.logTransaction(ctx, Transaction{
				RequestID: requestID, VirtualKey: vk.Key, VirtualName: vk.Name,
				Provider: provider, Method: r.Method, Path: r.URL.Path,
				StatusCode: http.StatusBadRequest, RequestBytes: int64(len(reqRaw)),
				Error:     "map model: " + err.Error(),
				CreatedAt: start, DurationMS: time.Since(start).Milliseconds(),
			}, bodiesIfLogged(logLimit, &reqLog))
			http.Error(w, "map model", http.StatusBadRequest)
			return
		}
		upstreamBody = mapped
	}

	if logLimit > 0 && len(upstreamBody) > 0 {
		reqLog.UpstreamRequest, reqLog.UpstreamRequestTruncated = truncateForLog(upstreamBody, logLimit)
	}

	upReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(upstreamBody))
	if err != nil {
		g.logTransaction(ctx, Transaction{
			RequestID: requestID, VirtualKey: vk.Key, VirtualName: vk.Name,
			Provider: provider, Method: r.Method, Path: r.URL.Path, UpstreamURL: targetURL,
			StatusCode: http.StatusInternalServerError, RequestBytes: int64(len(reqRaw)),
			Error: err.Error(), CreatedAt: start, DurationMS: time.Since(start).Milliseconds(),
		}, bodiesIfLogged(logLimit, &reqLog))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	copyHeaders(upReq.Header, r.Header)
	setUpstreamAuth(upReq.Header, provider, upstream.APIKey)

	upResp, err := g.httpClient.Do(upReq)
	if err != nil {
		g.logTransaction(ctx, Transaction{
			RequestID: requestID, VirtualKey: vk.Key, VirtualName: vk.Name,
			Provider: provider, Method: r.Method, Path: r.URL.Path, UpstreamURL: targetURL,
			StatusCode: http.StatusBadGateway, RequestBytes: int64(len(reqRaw)),
			Error: err.Error(), CreatedAt: start, DurationMS: time.Since(start).Milliseconds(),
		}, bodiesIfLogged(logLimit, &reqLog))
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer upResp.Body.Close()

	copyHeaders(w.Header(), upResp.Header)
	w.WriteHeader(upResp.StatusCode)

	capture := newBodyCapture(logLimit)
	copyErr := copyResponse(w, io.TeeReader(upResp.Body, capture))

	tx := Transaction{
		RequestID:     requestID,
		VirtualKey:    vk.Key,
		VirtualName:   vk.Name,
		Provider:      provider,
		Method:        r.Method,
		Path:          r.URL.Path,
		UpstreamURL:   targetURL,
		StatusCode:    upResp.StatusCode,
		RequestBytes:  int64(len(reqRaw)),
		ResponseBytes: capture.total,
		CreatedAt:     start,
		DurationMS:    time.Since(start).Milliseconds(),
	}
	if copyErr != nil {
		tx.Error = copyErr.Error()
	}

	bodies := bodiesIfLogged(logLimit, &reqLog)
	if bodies != nil && capture.logged {
		bodies.Response = capture.text()
		bodies.ResponseTruncated = capture.truncated
	}
	g.logTransaction(ctx, tx, bodies)
}

// copyResponse streams the upstream body to the client, flushing when possible
// so SSE / chunked LLM streams reach the browser incrementally.
func copyResponse(w http.ResponseWriter, r io.Reader) error {
	buf := make([]byte, 32*1024)
	flusher, canFlush := w.(http.Flusher)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (g *Gateway) authenticate(r *http.Request) (VirtualKey, string) {
	key := extractVirtualKey(r)
	if key == "" {
		return VirtualKey{}, "missing virtual key (Authorization: Bearer <key> or x-api-key)"
	}
	vk, err := g.store.GetVirtualKey(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		return VirtualKey{}, "invalid virtual key"
	}
	if err != nil {
		g.logger.Error("llmgw get virtual key", "err", err)
		return VirtualKey{}, "virtual key lookup failed"
	}
	return *vk, ""
}

func extractVirtualKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	return ""
}

func setUpstreamAuth(h http.Header, provider, apiKey string) {
	h.Del("Authorization")
	h.Del("x-api-key")
	switch provider {
	case ProviderAnthropic:
		h.Set("x-api-key", apiKey)
	case ProviderOpenAI:
		h.Set("Authorization", "Bearer "+apiKey)
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		// Allowlist only. Omitting Accept-Encoding lets http.Transport negotiate gzip.
		if !relayHeaderAllowed(k) {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func relayHeaderAllowed(name string) bool {
	switch strings.ToLower(name) {
	case "accept", "accept-language", "content-type", "content-length",
		"anthropic-version", "anthropic-beta", "anthropic-dangerous-direct-browser-access",
		"openai-beta", "openai-organization", "openai-project",
		"x-request-id":
		return true
	default:
		return false
	}
}

func (g *Gateway) logTransaction(ctx context.Context, tx Transaction, bodies *TransactionBodies) {
	if err := g.store.InsertTransaction(ctx, tx, bodies); err != nil {
		g.logger.Error("llmgw log transaction", "err", err, "request_id", tx.RequestID)
	}
}

func truncateForLog(data []byte, limit int) (string, bool) {
	truncated := false
	if len(data) > limit {
		data = data[:limit]
		truncated = true
	}
	return sanitizeLogBody(data), truncated
}

func bodiesIfLogged(logLimit int, partial *TransactionBodies) *TransactionBodies {
	if logLimit <= 0 {
		return nil
	}
	if partial == nil {
		return &TransactionBodies{}
	}
	return partial
}

type bodyCapture struct {
	limit     int
	buf       bytes.Buffer
	total     int64
	truncated bool
	logged    bool
}

func newBodyCapture(limit int) *bodyCapture {
	if limit <= 0 {
		return &bodyCapture{}
	}
	return &bodyCapture{limit: limit, logged: true}
}

func (c *bodyCapture) Write(p []byte) (int, error) {
	n := len(p)
	c.total += int64(n)
	if c.limit <= 0 {
		return n, nil
	}
	remain := c.limit - c.buf.Len()
	if remain > 0 {
		if n > remain {
			c.buf.Write(p[:remain])
			c.truncated = true
		} else {
			c.buf.Write(p)
		}
	} else {
		c.truncated = true
	}
	return n, nil
}

func (c *bodyCapture) text() string {
	return sanitizeLogBody(c.buf.Bytes())
}

// sanitizeLogBody makes body bytes safe for PostgreSQL TEXT (UTF-8).
// Gzip payloads (often from Accept-Encoding passthrough) are noted, not stored raw.
func sanitizeLogBody(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b {
		if plain, err := gunzipPrefix(b); err == nil {
			return sanitizeLogBody(plain)
		}
		return fmt.Sprintf("[binary gzip body, %d bytes captured]", len(b))
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), "\uFFFD")
}

func gunzipPrefix(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, 1<<20))
	// Capture buffer may truncate mid-stream; keep whatever decoded.
	if len(out) > 0 {
		return out, nil
	}
	return nil, err
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
