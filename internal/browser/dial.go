package browser

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/RoundpenAI/roundpen/internal/config"
)

// PortDialer opens a TCP connection to a port inside a sandbox (Backend.Dial).
type PortDialer interface {
	Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)
}

func startCDPProxy(dial PortDialer, sandboxID string, port int) (localURL string, stop func(), err error) {
	if dial == nil {
		return "", nil, fmt.Errorf("docker CDP requires a sandbox dialer")
	}
	if port <= 0 {
		port = config.DefaultCDPPort
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
