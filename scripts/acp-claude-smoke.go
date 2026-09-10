//go:build ignore

// ACP initialize smoke against the agent-claude QEMU image.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	acp "github.com/coder/acp-go-sdk"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/backend"
	"github.com/RoundpenAI/roundpen/internal/backend/qemu"
)

func main() {
	root, _ := os.Getwd()
	img := filepath.Join(root, "images/agent-qemu/out/agent.qcow2")
	if v := os.Getenv("ROUNDPEN_AGENT_IMAGE"); v != "" {
		img = v
	}
	data := filepath.Join(os.TempDir(), "roundpen-acp-claude-smoke")
	_ = os.RemoveAll(data)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	be, err := qemu.New(data)
	if err != nil {
		fatal("qemu.New", err)
	}
	id := "acp-claude-smoke"
	if _, err := be.Create(ctx, backend.CreateOpts{
		SandboxID:   id,
		Image:       img,
		Env:         map[string]string{"ROUNDPEN_URL": "http://127.0.0.1:9527", "ROUNDPEN_SLOT": "agent"},
		MemoryLimit: 2 << 30,
		CPULimit:    2,
	}); err != nil {
		fatal("Create", err)
	}
	defer func() { _ = be.Remove(context.Background(), id) }()
	if err := be.Start(ctx, id); err != nil {
		fatal("Start", err)
	}
	fmt.Println("==> VM started; attaching claude-agent-acp")

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	attachErr := make(chan error, 1)
	go func() {
		attachErr <- be.AttachExec(ctx, id, backend.AttachExecOpts{
			Cmd:     []string{"claude-agent-acp"},
			WorkDir: "/workspace",
		}, stdinR, stdoutW, os.Stderr)
		_ = stdinR.Close()
		_ = stdoutW.Close()
	}()

	bridge := acpclient.New(nil, nil, id, true)
	conn := acp.NewClientSideConnection(bridge, stdinW, stdoutR)
	initCtx, initCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer initCancel()
	init, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		},
	})
	if err != nil {
		fatal("Initialize", err)
	}
	fmt.Printf("==> initialize protocol=%v agent=%v\n", init.ProtocolVersion, init.AgentInfo)

	sess, err := conn.NewSession(initCtx, acp.NewSessionRequest{
		Cwd:        "/workspace",
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		fatal("NewSession", err)
	}
	fmt.Printf("==> session %s\n", sess.SessionId)
	cancel()
	select {
	case err := <-attachErr:
		if err != nil && err != context.Canceled {
			fmt.Printf("attach ended: %v\n", err)
		}
	case <-time.After(3 * time.Second):
	}
	fmt.Println("ACP SMOKE PASS: claude-agent-acp initialize + newSession")
}

func fatal(step string, err error) {
	fmt.Fprintf(os.Stderr, "ACP SMOKE FAIL %s: %v\n", step, err)
	os.Exit(1)
}
