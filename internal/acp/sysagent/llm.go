package sysagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// LLMConfig configures OpenAI-compatible chat via llmgw loopback.
type LLMConfig struct {
	BaseURL      string // e.g. http://127.0.0.1:19001/llmgw/openai
	APIKey       string
	Model        string
	DefaultModel func() string
	HTTPClient   *http.Client
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolCallFn   `json:"function"`
}

type toolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatRequest struct {
	Model    string           `json:"model"`
	Messages []chatMessage    `json:"messages"`
	Tools    []map[string]any `json:"tools,omitempty"`
	Stream   bool             `json:"stream,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c LLMConfig) model() string {
	m := strings.TrimSpace(c.Model)
	if m == "" && c.DefaultModel != nil {
		m = strings.TrimSpace(c.DefaultModel())
	}
	if m == "" {
		m = "default"
	}
	return m
}

func (c LLMConfig) chat(ctx context.Context, messages []chatMessage, tools []map[string]any) (chatMessage, string, error) {
	return c.chatStream(ctx, messages, tools, nil)
}

// chatStream calls OpenAI-compatible chat completions with stream=true.
// onContent receives each text delta (may be nil to accumulate silently).
func (c LLMConfig) chatStream(ctx context.Context, messages []chatMessage, tools []map[string]any, onContent func(string) error) (chatMessage, string, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	body, err := json.Marshal(chatRequest{
		Model:    c.model(),
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	})
	if err != nil {
		return chatMessage{}, "", err
	}
	// Upstream OpenAI base may or may not already include "/v1" (settings differ).
	paths := []string{"/v1/chat/completions", "/chat/completions"}
	var lastErr error
	for _, path := range paths {
		msg, finish, err := c.chatStreamOnce(ctx, base+path, body, onContent)
		if err == nil {
			return msg, finish, nil
		}
		lastErr = err
		if !isHTTPStatus(err, 404) {
			return chatMessage{}, "", err
		}
	}
	return chatMessage{}, "", lastErr
}

type httpStatusError struct {
	code int
	body string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("llm HTTP %d: %s", e.code, e.body)
}

func isHTTPStatus(err error, code int) bool {
	var he *httpStatusError
	return errors.As(err, &he) && he.code == code
}

func (c LLMConfig) chatStreamOnce(ctx context.Context, url string, body []byte, onContent func(string) error) (chatMessage, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return chatMessage{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.streamClient().Do(req)
	if err != nil {
		return chatMessage{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return chatMessage{}, "", &httpStatusError{code: resp.StatusCode, body: strings.TrimSpace(string(raw))}
	}
	br := bufio.NewReader(resp.Body)
	peek, _ := br.Peek(32)
	trimmed := bytes.TrimSpace(peek)
	// Some gateways ignore stream=true and return a full JSON object.
	if len(trimmed) > 0 && trimmed[0] == '{' && !bytes.Contains(peek, []byte("data:")) {
		return readChatJSON(br, onContent)
	}
	return readChatSSE(ctx, br, onContent)
}

func readChatJSON(r io.Reader, onContent func(string) error) (chatMessage, string, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return chatMessage{}, "", err
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return chatMessage{}, "", fmt.Errorf("decode llm response: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return chatMessage{}, "", fmt.Errorf("llm error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return chatMessage{}, "", fmt.Errorf("llm returned no choices")
	}
	msg := out.Choices[0].Message
	if msg.Content != "" && onContent != nil {
		if err := onContent(msg.Content); err != nil {
			return chatMessage{}, "", err
		}
	}
	return msg, out.Choices[0].FinishReason, nil
}

func (c LLMConfig) streamClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	// Rely on request context for cancel; avoid cutting long streams short.
	return &http.Client{Timeout: 0}
}

type streamDelta struct {
	Choices []struct {
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func readChatSSE(ctx context.Context, r io.Reader, onContent func(string) error) (chatMessage, string, error) {
	br := newLineReader(r)
	var content strings.Builder
	toolAcc := map[int]*toolCall{}
	var finish string
	role := "assistant"

	for {
		if err := ctx.Err(); err != nil {
			return chatMessage{}, "", err
		}
		line, err := br.ReadLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			return chatMessage{}, "", err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk streamDelta
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return chatMessage{}, "", fmt.Errorf("llm error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			finish = *ch.FinishReason
		}
		d := ch.Delta
		if d.Role != "" {
			role = d.Role
		}
		if d.Content != "" {
			content.WriteString(d.Content)
			if onContent != nil {
				if err := onContent(d.Content); err != nil {
					return chatMessage{}, "", err
				}
			}
		}
		for _, tc := range d.ToolCalls {
			acc, ok := toolAcc[tc.Index]
			if !ok {
				acc = &toolCall{Type: "function"}
				toolAcc[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Type != "" {
				acc.Type = tc.Type
			}
			if tc.Function.Name != "" {
				acc.Function.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.Function.Arguments += tc.Function.Arguments
			}
		}
	}

	msg := chatMessage{Role: role, Content: content.String()}
	if len(toolAcc) > 0 {
		// Preserve index order.
		maxIdx := -1
		for i := range toolAcc {
			if i > maxIdx {
				maxIdx = i
			}
		}
		for i := 0; i <= maxIdx; i++ {
			if tc, ok := toolAcc[i]; ok {
				msg.ToolCalls = append(msg.ToolCalls, *tc)
			}
		}
		if finish == "" {
			finish = "tool_calls"
		}
	}
	if finish == "" {
		finish = "stop"
	}
	return msg, finish, nil
}

// lineReader reads SSE lines without pulling the whole body into memory.
type lineReader struct {
	r   io.Reader
	buf []byte
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: r, buf: make([]byte, 0, 4096)}
}

func (l *lineReader) ReadLine() (string, error) {
	for {
		if i := bytes.IndexByte(l.buf, '\n'); i >= 0 {
			line := string(l.buf[:i])
			l.buf = l.buf[i+1:]
			if strings.HasSuffix(line, "\r") {
				line = line[:len(line)-1]
			}
			return line, nil
		}
		tmp := make([]byte, 4096)
		n, err := l.r.Read(tmp)
		if n > 0 {
			l.buf = append(l.buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF && len(l.buf) > 0 {
				line := string(l.buf)
				l.buf = nil
				return line, nil
			}
			return "", err
		}
	}
}
