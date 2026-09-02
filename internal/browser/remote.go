package browser

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"

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
	if err := chromedp.Run(ctx, emulation.SetDeviceMetricsOverride(int64(width), int64(height), 1, false)); err != nil {
		eng.Close()
		return nil, fmt.Errorf("cdp attach: %w", err)
	}
	return eng, nil
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
