package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func (h *Handler) checkTerminalOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if h.PublicURL != "" {
		if pu, err := url.Parse(h.PublicURL); err == nil && strings.EqualFold(u.Host, pu.Host) {
			return true
		}
	}
	return false
}

// MountTerminal registers the interactive PTY WebSocket.
func (h *Handler) MountTerminal(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sandboxes/{id}/terminal", h.terminal)
}

func (h *Handler) terminal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     h.checkTerminalOrigin,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	rows, cols := uint16(24), uint16(80)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	mt, data, err := conn.ReadMessage()
	_ = conn.SetReadDeadline(time.Time{})
	var first []byte
	if err == nil && mt == websocket.TextMessage && strings.HasPrefix(string(data), `{"rows"`) {
		var sz struct {
			Rows uint16 `json:"rows"`
			Cols uint16 `json:"cols"`
		}
		if json.Unmarshal(data, &sz) == nil {
			if sz.Rows > 0 {
				rows = sz.Rows
			}
			if sz.Cols > 0 {
				cols = sz.Cols
			}
		}
	} else if err == nil {
		first = data
	}

	sessionKey := uuid.NewString()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	defer stdinW.Close()
	defer stdoutW.Close()

	ctx := r.Context()
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.Manager.AttachTerminal(ctx, id, sessionKey, sandbox.TerminalOpts{
			Rows: rows, Cols: cols,
		}, stdinR, stdoutW)
		_ = stdoutW.Close()
	}()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := stdoutR.Read(buf)
			if n > 0 {
				_ = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}()

	if len(first) > 0 {
		if strings.HasPrefix(string(first), `{"rows"`) {
			h.handleResize(id, sessionKey, first)
		} else {
			_, _ = stdinW.Write(first)
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			}
		}
	}()

	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			_ = stdinW.Close()
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if mt == websocket.TextMessage && strings.HasPrefix(string(data), `{"rows"`) {
			h.handleResize(id, sessionKey, data)
			continue
		}
		if _, err := stdinW.Write(data); err != nil {
			break
		}
	}
	<-errCh
}

func (h *Handler) handleResize(sandboxID, sessionKey string, data []byte) {
	var sz struct {
		Rows uint16 `json:"rows"`
		Cols uint16 `json:"cols"`
	}
	if json.Unmarshal(data, &sz) != nil || sz.Rows == 0 || sz.Cols == 0 {
		return
	}
	_ = h.Manager.ResizeTerminal(context.Background(), sandboxID, sessionKey, sz.Rows, sz.Cols)
}
