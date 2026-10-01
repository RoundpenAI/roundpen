package imconnect

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

type fakePlatform struct {
	name string
	sent string
	key  string
}

func (f *fakePlatform) Name() string                             { return f.name }
func (f *fakePlatform) Start(core.MessageHandler) error          { return nil }
func (f *fakePlatform) Reply(context.Context, any, string) error { return nil }
func (f *fakePlatform) Stop() error                              { return nil }
func (f *fakePlatform) ReconstructReplyCtx(key string) (any, error) {
	f.key = key
	return key, nil
}
func (f *fakePlatform) Send(_ context.Context, _ any, content string) error {
	f.sent = content
	return nil
}

func TestPushText_UsesExistingChat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	sm := core.NewSessionManager(path)
	sm.GetOrCreateActive("telegram:chat1:user1")
	sm.Save()

	p := &fakePlatform{name: "telegram"}
	if err := pushText(context.Background(), []core.Platform{p}, path, "周报好了"); err != nil {
		t.Fatal(err)
	}
	if p.sent != "周报好了" || p.key != "telegram:chat1:user1" {
		t.Fatalf("sent %q key %q", p.sent, p.key)
	}
}

func TestPushText_NoConversation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	sm := core.NewSessionManager(path)
	sm.Save()
	err := pushText(context.Background(), []core.Platform{&fakePlatform{name: "telegram"}}, path, "hi")
	if err == nil {
		t.Fatal("expected error when there is no chat")
	}
}
