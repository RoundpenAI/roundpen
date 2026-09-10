package browser

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
)

type scriptDial struct {
	body string
}

func (s *scriptDial) Dial(context.Context, string, int) (net.Conn, error) {
	a, b := net.Pipe()
	go func() {
		defer b.Close()
		buf := make([]byte, 256)
		n, _ := b.Read(buf)
		for n > 0 && !bytes.Contains(buf[:n], []byte("\r\n\r\n")) {
			more, err := b.Read(buf[n:])
			if err != nil {
				break
			}
			n += more
		}
		if s.body != "" {
			_, _ = io.WriteString(b, s.body)
		}
	}()
	return a, nil
}

func TestProbeGuestCDP(t *testing.T) {
	ok := &scriptDial{body: "HTTP/1.1 200 OK\r\nConnection: close\r\n\r\n{\"Browser\":\"Chrome\",\"webSocketDebuggerUrl\":\"ws://127.0.0.1:9333/devtools/browser/x\"}"}
	if err := probeGuestCDP(t.Context(), ok, "sb", 9222); err != nil {
		t.Fatal(err)
	}
	empty := &scriptDial{body: ""}
	err := probeGuestCDP(t.Context(), empty, "sb", 9222)
	if err == nil || !cdpRetryable(err) {
		t.Fatalf("empty: %v", err)
	}
}

func TestCDPRetryableEmptyChrome(t *testing.T) {
	if !cdpRetryable(errChromeDown()) {
		t.Fatal("expected retry")
	}
}

func TestProbeGuestCDPKeepAlive(t *testing.T) {
	body := `{"Browser":"Chrome","webSocketDebuggerUrl":"ws://127.0.0.1:9333/devtools/browser/x"}`
	ok := &holdDial{body: "HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body}
	if err := probeGuestCDP(t.Context(), ok, "sb", 9222); err != nil {
		t.Fatal(err)
	}
}

// holdDial writes a complete HTTP response and leaves the connection open,
// like Chrome DevTools on :9333.
type holdDial struct {
	body string
}

func (s *holdDial) Dial(context.Context, string, int) (net.Conn, error) {
	a, b := net.Pipe()
	go func() {
		buf := make([]byte, 256)
		n, _ := b.Read(buf)
		for n > 0 && !bytes.Contains(buf[:n], []byte("\r\n\r\n")) {
			more, err := b.Read(buf[n:])
			if err != nil {
				_ = b.Close()
				return
			}
			n += more
		}
		_, _ = io.WriteString(b, s.body)
		// keep b open until the client closes
		_, _ = io.Copy(io.Discard, b)
	}()
	return a, nil
}

func errChromeDown() error {
	return probeGuestCDP(context.Background(), &scriptDial{body: ""}, "sb", 9222)
}
