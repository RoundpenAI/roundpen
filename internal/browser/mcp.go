package browser

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id,omitempty"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcErr `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (h *Handler) mcp(w http.ResponseWriter, r *http.Request, bc browserCtx) {
	if r.Method == http.MethodGet {
		// Streamable HTTP: GET opens an SSE stream. Tools are request/response via POST.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, ": connected\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPC(w, r, rpcResponse{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}})
		return
	}
	if req.ID == nil && strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := h.dispatchMCP(r, bc, req)
	writeJSONRPC(w, r, resp)
}

func writeJSONRPC(w http.ResponseWriter, r *http.Request, resp rpcResponse) {
	if resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
	}
	raw, _ := json.Marshal(resp)
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message\ndata: "+string(raw)+"\n\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n"))
}

func (h *Handler) dispatchMCP(r *http.Request, bc browserCtx, req rpcRequest) rpcResponse {
	out := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		out.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "roundpen-browser", "version": "0.1.0"},
		}
	case "notifications/initialized", "notifications/cancelled":
		out.ID = nil
		out.Result = nil
	case "ping":
		out.Result = map[string]any{}
	case "tools/list":
		out.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			out.Error = &rpcErr{Code: -32602, Message: "invalid params"}
			return out
		}
		text, image, err := h.callTool(r, bc, p.Name, p.Arguments)
		if err != nil {
			out.Result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			}
			return out
		}
		content := []map[string]any{}
		if text != "" {
			content = append(content, map[string]any{"type": "text", "text": text})
		}
		if len(image) > 0 {
			content = append(content, map[string]any{
				"type":     "image",
				"mimeType": "image/png",
				"data":     base64.StdEncoding.EncodeToString(image),
			})
		}
		out.Result = map[string]any{"content": content}
	default:
		out.Error = &rpcErr{Code: -32601, Message: "method not found: " + req.Method}
	}
	return out
}

func mcpTools() []map[string]any {
	return []map[string]any{
		toolSchema("browser_navigate", "Open a URL in the sandbox browser.", map[string]any{
			"url": map[string]any{"type": "string", "description": "Absolute URL to open"},
		}, []string{"url"}),
		toolSchema("browser_snapshot", "Accessibility-style snapshot of the current page (refs for click/type).", nil, nil),
		toolSchema("browser_click", "Click an element from the last snapshot.", map[string]any{
			"ref": map[string]any{"type": "string", "description": "Element ref (e.g. e3)"},
		}, []string{"ref"}),
		toolSchema("browser_type", "Type into an input from the last snapshot.", map[string]any{
			"ref":    map[string]any{"type": "string"},
			"text":   map[string]any{"type": "string"},
			"submit": map[string]any{"type": "boolean", "description": "Press Enter after typing"},
		}, []string{"ref", "text"}),
		toolSchema("browser_press", "Press a key (Enter, Tab, Escape, Backspace, or a character).", map[string]any{
			"key": map[string]any{"type": "string"},
		}, []string{"key"}),
		toolSchema("browser_screenshot", "Capture a PNG screenshot of the current page.", nil, nil),
		toolSchema("browser_set_viewport", "Resize the browser viewport (e.g. 1280x800 or 390x844).", map[string]any{
			"width":  map[string]any{"type": "integer"},
			"height": map[string]any{"type": "integer"},
		}, []string{"width", "height"}),
		toolSchema("browser_evaluate", "Run JavaScript in the page and return the result.", map[string]any{
			"expression": map[string]any{"type": "string"},
		}, []string{"expression"}),
	}
}

func toolSchema(name, desc string, props map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object"}
	if props == nil {
		props = map[string]any{}
	}
	schema["properties"] = props
	if len(required) > 0 {
		schema["required"] = required
	}
	return map[string]any{
		"name":        name,
		"description": desc,
		"inputSchema": schema,
	}
}

func (h *Handler) callTool(r *http.Request, bc browserCtx, name string, args map[string]any) (string, []byte, error) {
	ctx := r.Context()
	sess, err := h.Hub.Ensure(ctx, bc.id)
	if err != nil {
		return "", nil, err
	}
	_ = h.Sandboxes.Touch(ctx, bc.id)
	switch name {
	case "browser_navigate":
		url, _ := args["url"].(string)
		if strings.TrimSpace(url) == "" {
			return "", nil, errString("url is required")
		}
		if err := sess.Engine.Navigate(ctx, url); err != nil {
			return "", nil, err
		}
		snap, err := sess.Engine.Snapshot(ctx)
		if err != nil {
			return sess.Engine.URL(), nil, nil
		}
		return snap.Text, nil, nil
	case "browser_snapshot":
		snap, err := sess.Engine.Snapshot(ctx)
		if err != nil {
			return "", nil, err
		}
		return snap.Text, nil, nil
	case "browser_click":
		ref, _ := args["ref"].(string)
		if err := sess.Engine.Click(ctx, ref); err != nil {
			return "", nil, err
		}
		snap, err := sess.Engine.Snapshot(ctx)
		if err != nil {
			return "clicked " + ref, nil, nil
		}
		return snap.Text, nil, nil
	case "browser_type":
		ref, _ := args["ref"].(string)
		text, _ := args["text"].(string)
		submit, _ := args["submit"].(bool)
		if err := sess.Engine.Type(ctx, ref, text, submit); err != nil {
			return "", nil, err
		}
		snap, err := sess.Engine.Snapshot(ctx)
		if err != nil {
			return "typed into " + ref, nil, nil
		}
		return snap.Text, nil, nil
	case "browser_press":
		key, _ := args["key"].(string)
		if err := sess.Engine.Press(ctx, key); err != nil {
			return "", nil, err
		}
		return "pressed " + key, nil, nil
	case "browser_screenshot":
		png, err := sess.Engine.Screenshot(ctx)
		if err != nil {
			return "", nil, err
		}
		return "screenshot " + sess.Engine.URL(), png, nil
	case "browser_set_viewport":
		w := intFrom(args["width"])
		ht := intFrom(args["height"])
		if err := sess.Engine.SetViewport(ctx, w, ht); err != nil {
			return "", nil, err
		}
		sess.Width, sess.Height = w, ht
		return "viewport set", nil, nil
	case "browser_evaluate":
		expr, _ := args["expression"].(string)
		raw, err := sess.Engine.Evaluate(ctx, expr)
		if err != nil {
			return "", nil, err
		}
		return string(raw), nil, nil
	default:
		return "", nil, errString("unknown tool " + name)
	}
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func errString(s string) error { return simpleError(s) }

func intFrom(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}
