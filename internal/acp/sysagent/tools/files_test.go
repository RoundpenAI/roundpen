package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

type memFiles struct {
	data map[string][]byte
}

func (m *memFiles) ReadFile(_ context.Context, _, relPath string) (io.ReadCloser, error) {
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	b, ok := m.data[relPath]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", relPath)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memFiles) WriteFile(_ context.Context, _, relPath string, r io.Reader) error {
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.data[relPath] = b
	return nil
}

func TestReadWriteEdit(t *testing.T) {
	fs := &memFiles{data: map[string][]byte{}}
	slots := &stubAgentSlots{id: "sb1"}
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Slots: slots, Files: fs})

	actor := tools.Actor{Username: "alice"}
	_, err := reg.Call(context.Background(), actor, "Write", json.RawMessage(
		`{"file_path":"/workspace/a.txt","content":"hello world"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Call(context.Background(), actor, "Read", json.RawMessage(
		`{"file_path":"a.txt"}`))
	if err != nil || !strings.Contains(out, "hello world") {
		t.Fatalf("read=%q err=%v", out, err)
	}
	_, err = reg.Call(context.Background(), actor, "Edit", json.RawMessage(
		`{"file_path":"/workspace/a.txt","old_string":"world","new_string":"there"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(fs.data["a.txt"]) != "hello there" {
		t.Fatalf("got %q", fs.data["a.txt"])
	}
	if slots.calls < 3 {
		t.Fatalf("ensure calls=%d", slots.calls)
	}
}

func TestEditZeroMatches(t *testing.T) {
	fs := &memFiles{data: map[string][]byte{"a.txt": []byte("hello")}}
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb"}, Files: fs})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Edit", json.RawMessage(
		`{"file_path":"a.txt","old_string":"missing","new_string":"x"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEditMultipleMatches(t *testing.T) {
	fs := &memFiles{data: map[string][]byte{"a.txt": []byte("aa aa")}}
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb"}, Files: fs})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Edit", json.RawMessage(
		`{"file_path":"a.txt","old_string":"aa","new_string":"bb"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = reg.Call(context.Background(), tools.Actor{Username: "u"}, "Edit", json.RawMessage(
		`{"file_path":"a.txt","old_string":"aa","new_string":"bb","replace_all":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(fs.data["a.txt"]) != "bb bb" {
		t.Fatalf("got %q", fs.data["a.txt"])
	}
}

func TestReadPathEscape(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Files: &memFiles{data: map[string][]byte{}},
	})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Read", json.RawMessage(
		`{"file_path":"/etc/passwd"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}
