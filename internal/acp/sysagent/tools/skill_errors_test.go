package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

// errFiles fails every read with a non-not-exist error, to prove storage
// failures surface instead of masquerading as "skill absent".
type errFiles struct{ memFiles }

func (e *errFiles) ReadFile(context.Context, string, string) (io.ReadCloser, error) {
	return nil, errors.New("disk on fire")
}

func skillArgs(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAskUserQuestionBoundsExported(t *testing.T) {
	if tools.AskUserQuestionMin() != 2 {
		t.Fatalf("min = %d", tools.AskUserQuestionMin())
	}
	if tools.AskUserQuestionMax() != 4 {
		t.Fatalf("max = %d", tools.AskUserQuestionMax())
	}
}

func TestSkillInvoke_StorageErrorSurfaces(t *testing.T) {
	binder := &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Files: &errFiles{},
	}
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	actor := tools.Actor{Username: "alice"}

	// A read failure must not silently fall back to the built-in skill.
	_, err := reg.Call(context.Background(), actor, "Skill", skillArgs(t, map[string]any{"skill": "commit"}))
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("expected storage error to surface, got %v", err)
	}
}

func TestSkillInvoke_FilesNotConfigured(t *testing.T) {
	binder := &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb"}}
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", skillArgs(t, map[string]any{"skill": "commit"}))
	if err == nil || !strings.Contains(err.Error(), "workspace files not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillInstallFromServerErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/500.md", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/huge.md", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 256<<10+1))
	})
	mux.HandleFunc("/no-name.md", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("---\ndescription: no name in frontmatter.\n---\nDo it.\n"))
	})
	mux.HandleFunc("/Has%20Space.md", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("---\ndescription: bad filename.\n---\nDo it.\n"))
	})
	mux.HandleFunc("/ok.markdown", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("---\ndescription: derived from .markdown basename.\n---\nDo it.\n"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	binder, _, _ := skillBinder()
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, tools.NewWebHTTPClient(tools.WebClientOptions{AllowLoopback: true}))
	actor := tools.Actor{Username: "alice"}
	ctx := context.Background()

	for path, want := range map[string]string{
		"/500.md":  "HTTP 500",
		"/huge.md": "exceeds",
	} {
		_, err := reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{"action": "install", "url": ts.URL + path}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v, want %q", path, err, want)
		}
	}

	// No name in frontmatter and no skill argument → derived from URL basename.
	_, err := reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{"action": "install", "url": ts.URL + "/no-name.md"}))
	if err != nil {
		t.Fatal(err)
	}
	// Reinstall path hits the overwrite confirmation.
	out, err := reg.Call(confirmCtx(t, "Cancel"), actor, "Skill", skillArgs(t, map[string]any{"action": "install", "url": ts.URL + "/no-name.md"}))
	if err != nil || !strings.Contains(out, "not installed") {
		t.Fatalf("out=%q err=%v", out, err)
	}

	// Basename with invalid skill-name characters cannot be deduced.
	_, err = reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{"action": "install", "url": ts.URL + "/Has%20Space.md"}))
	if err == nil || !strings.Contains(err.Error(), "could not determine") {
		t.Fatalf("err = %v", err)
	}

	// ".markdown" suffix is stripped when deriving the name.
	_, err = reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{"action": "install", "url": ts.URL + "/ok.markdown"}))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillInstallContentErrors(t *testing.T) {
	binder, _, _ := skillBinder()
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	actor := tools.Actor{Username: "alice"}
	ctx := context.Background()

	for name, body := range map[string]string{
		"both url and content": "",
		"empty body":           "---\nname: x\ndescription: y\n---\n",
	} {
		args := map[string]any{"action": "install", "content": body}
		if body == "" {
			args = map[string]any{"action": "install", "content": "x", "url": "http://127.0.0.1:1/x.md"}
		}
		_, err := reg.Call(ctx, actor, "Skill", skillArgs(t, args))
		if err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}

	// Missing description.
	_, err := reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{
		"action":  "install",
		"content": "---\nname: nodesc\n---\nDo things.\n",
	}))
	if err == nil || !strings.Contains(err.Error(), "missing a description") {
		t.Fatalf("err = %v", err)
	}
	// Invalid name.
	_, err = reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{
		"action":  "install",
		"skill":   "Bad Name",
		"content": "---\ndescription: x\n---\nDo things.\n",
	}))
	if err == nil || !strings.Contains(err.Error(), "invalid skill name") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillInstallOverwriteNeedsInteractor(t *testing.T) {
	binder, fs, _ := skillBinder()
	fs.data[".roundpen/skills/commit.md"] = []byte("---\ndescription: x.\n---\nDo it.\n")
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	// No interactor on the context → confirmation cannot be asked.
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", skillArgs(t, map[string]any{
		"action":  "install",
		"content": "---\nname: commit\ndescription: y.\n---\nDo it.\n",
	}))
	if err == nil || !strings.Contains(err.Error(), "interactive session not available") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillRemoveErrors(t *testing.T) {
	actor := tools.Actor{Username: "alice"}
	ctx := confirmCtx(t, "Remove")

	// Removing a built-in skill that is not shadowed.
	binder, _, _ := skillBinder()
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	_, err := reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{"action": "remove", "skill": "commit"}))
	if err == nil || !strings.Contains(err.Error(), "cannot be removed") {
		t.Fatalf("err = %v", err)
	}

	// Unknown skill.
	_, err = reg.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{"action": "remove", "skill": "ghost"}))
	if err == nil || !strings.Contains(err.Error(), "no skill") {
		t.Fatalf("err = %v", err)
	}

	// Exec not configured: the delete cannot run.
	noExec := &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb"}, Files: &memFiles{data: map[string][]byte{
		".roundpen/skills/gsync.md": []byte("---\ndescription: x.\n---\nDo it.\n"),
	}}}
	reg2 := tools.NewRegistry()
	tools.RegisterSkill(reg2, noExec, nil)
	_, err = reg2.Call(ctx, actor, "Skill", skillArgs(t, map[string]any{"action": "remove", "skill": "gsync"}))
	if err == nil || !strings.Contains(err.Error(), "workspace exec not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillListErrors(t *testing.T) {
	// Exec not configured → listing workspace skills fails.
	noExec := &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb"}, Files: &memFiles{}}
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, noExec, nil)
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", skillArgs(t, map[string]any{"action": "list"}))
	if err == nil || !strings.Contains(err.Error(), "workspace exec not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillUnknownAction(t *testing.T) {
	binder, _, _ := skillBinder()
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", skillArgs(t, map[string]any{"action": "explode"}))
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillInstallNotBothNameSources(t *testing.T) {
	binder, _, _ := skillBinder()
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", skillArgs(t, map[string]any{
		"action":  "install",
		"skill":   "one",
		"content": "---\nname: two\ndescription: x.\n---\nDo it.\n",
	}))
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("err = %v", err)
	}
}
