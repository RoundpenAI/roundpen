package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/settings"
	"github.com/RoundpenAI/roundpen/internal/template"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type stubEnvs struct {
	list []userenv.EnvView
}

func (s stubEnvs) List(context.Context, string) ([]userenv.EnvView, error) {
	return s.list, nil
}

type stubTemplates struct {
	list []template.Record
}

func (s stubTemplates) List(context.Context) ([]template.Record, error) {
	return s.list, nil
}

type stubSessions struct {
	list []*agentsession.Session
}

func (s stubSessions) ListByUser(context.Context, string, int) ([]*agentsession.Session, error) {
	return s.list, nil
}

type stubSettings struct{}

func (stubSettings) Response() (settings.AppSettings, settings.SystemInfo) {
	return settings.AppSettings{DefaultImage: "img"}, settings.SystemInfo{HTTPAddr: ":1"}
}

func TestRoundpenBinder_ListEnvironmentsScrubsSandbox(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterRoundpen(reg, &tools.RoundpenBinder{
		Envs: stubEnvs{list: []userenv.EnvView{
			{Slot: "agent", Status: "absent", SandboxID: "sb-secret"},
		}},
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice", Role: "user"}, "ListEnvironments", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"environments"`) {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(out, "sb-secret") || strings.Contains(out, "sandboxId") {
		t.Fatalf("leaked sandbox fields: %q", out)
	}
	if _, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "roundpen_ensure_agent", nil); err == nil {
		t.Fatal("ensure must be gone")
	}
}

func TestRoundpenBinder_GetSettingsAdminOnly(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterRoundpen(reg, &tools.RoundpenBinder{Settings: stubSettings{}})
	if _, err := reg.Call(context.Background(), tools.Actor{Username: "u", Role: "user"}, "GetSettings", nil); err == nil {
		t.Fatal("expected admin required")
	}
	out, err := reg.Call(context.Background(), tools.Actor{Username: "a", Role: "admin"}, "GetSettings", nil)
	if err != nil {
		t.Fatal(err)
	}
	var wrap map[string]any
	if err := json.Unmarshal([]byte(out), &wrap); err != nil {
		t.Fatal(err)
	}
	if _, ok := wrap["settings"]; !ok {
		t.Fatalf("missing settings: %q", out)
	}
}

func TestRoundpenBinder_ListSessions(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterRoundpen(reg, &tools.RoundpenBinder{
		Sessions: stubSessions{list: []*agentsession.Session{{
			ID: "s1", UserID: "alice", Title: "t", ProviderID: "sysadmin",
			SandboxID: "hide-me", Status: "active",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}}},
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "ListSessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hide-me") || strings.Contains(out, "sandboxId") {
		t.Fatalf("leaked sandbox: %q", out)
	}
	if !strings.Contains(out, `"s1"`) {
		t.Fatalf("got %q", out)
	}
}

func TestRoundpenBinder_ListTemplates(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterRoundpen(reg, &tools.RoundpenBinder{
		Templates: stubTemplates{list: []template.Record{{
			TemplateID: "tid", Name: "code-agent", Slot: "agent", BuildStatus: template.BuildReady,
		}}},
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "ListTemplates", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "code-agent") {
		t.Fatalf("got %q", out)
	}
}
