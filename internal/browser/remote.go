package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// PortDialer opens a TCP connection to a port inside a sandbox (Backend.Dial).
type PortDialer interface {
	Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)
}

func newRemoteEngine(endpoint string, width, height int) (*chromeEngine, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("cdp endpoint is required")
	}
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 800
	}
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), endpoint)
	ctx, cancel := chromedp.NewContext(allocCtx)
	eng := &chromeEngine{
		alloc:  allocCancel,
		cancel: cancel,
		ctx:    ctx,
		width:  width,
		height: height,
	}
	if err := chromedp.Run(ctx,
		emulation.SetDeviceMetricsOverride(int64(width), int64(height), 1, false),
		emulation.SetUserAgentOverride(desktopChromeUA),
		stealthInitAction(),
	); err != nil {
		eng.Close()
		return nil, fmt.Errorf("cdp attach: %w", err)
	}
	return eng, nil
}

func probeGuestCDP(ctx context.Context, dial PortDialer, sandboxID string, port int) error {
	if dial == nil {
		return fmt.Errorf("docker cdp requires a sandbox dialer")
	}
	conn, err := dial.Dial(ctx, sandboxID, port)
	if err != nil {
		return fmt.Errorf("env cdp: dial guest :%d: %w", port, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	if _, err := io.WriteString(conn, "GET /json/version HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n"); err != nil {
		return fmt.Errorf("env cdp: write /json/version: %w", err)
	}
	raw, err := readHTTPResponse(conn, 8192)
	if cdpVersionOK(raw) {
		return nil
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("env cdp: read /json/version: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("env cdp (guest :%d up, Chrome :9333 not serving DevTools)", port)
	}
	return fmt.Errorf("env cdp: unexpected /json/version (%d bytes)", len(raw))
}

func cdpVersionOK(raw []byte) bool {
	return bytes.Contains(raw, []byte("webSocketDebuggerUrl")) || bytes.Contains(raw, []byte(`"Browser"`))
}

// readHTTPResponse reads headers plus Content-Length bytes. Chrome DevTools
// keeps the TCP connection open, so io.ReadAll would block until the deadline.
func readHTTPResponse(r io.Reader, limit int) ([]byte, error) {
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 512)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if len(buf) > limit {
				buf = buf[:limit]
			}
		}
		if idx := bytes.Index(buf, []byte("\r\n\r\n")); idx >= 0 {
			head, body := buf[:idx], buf[idx+4:]
			want := httpContentLength(head)
			if want < 0 {
				if cdpVersionOK(buf) || err != nil {
					return buf, err
				}
				if n == 0 && err == nil {
					continue
				}
				if err != nil {
					return buf, err
				}
				continue
			}
			if len(body) >= want || len(buf) >= limit {
				return buf, nil
			}
		}
		if err != nil {
			return buf, err
		}
		if len(buf) >= limit {
			return buf, nil
		}
	}
}

func httpContentLength(headers []byte) int {
	for _, line := range bytes.Split(headers, []byte("\r\n")) {
		k, v, ok := bytes.Cut(line, []byte(":"))
		if !ok || !strings.EqualFold(string(bytes.TrimSpace(k)), "content-length") {
			continue
		}
		n, err := strconv.Atoi(string(bytes.TrimSpace(v)))
		if err != nil || n < 0 {
			return -1
		}
		return n
	}
	return -1
}

func startCDPProxy(dial PortDialer, sandboxID string, port int) (localURL string, stop func(), err error) {
	if dial == nil {
		return "", nil, fmt.Errorf("docker CDP requires a sandbox dialer")
	}
	if port <= 0 {
		port = 9222
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			go proxyCDPConn(dial, sandboxID, port, client)
		}
	}()
	return "http://" + ln.Addr().String(), func() { _ = ln.Close() }, nil
}

func proxyCDPConn(dial PortDialer, sandboxID string, port int, client net.Conn) {
	defer client.Close()
	up, err := dial.Dial(context.Background(), sandboxID, port)
	if err != nil {
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(up, client)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, up)
		done <- struct{}{}
	}()
	<-done
}
