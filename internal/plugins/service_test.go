package plugins

import (
	"context"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

type memSettings struct {
	cur settings.AppSettings
}

func (m *memSettings) Current() settings.AppSettings { return m.cur }

func (m *memSettings) Update(_ context.Context, next settings.AppSettings) error {
	m.cur = next
	return nil
}

func TestServiceEnableAndDisable(t *testing.T) {
	mem := &memSettings{}
	svc := &Service{Settings: mem}

	list := svc.List()
	if len(list) != 1 || list[0].ID != SelfHostedGit || list[0].Enabled {
		t.Fatalf("default list = %+v", list)
	}
	if svc.IsEnabled(SelfHostedGit) {
		t.Fatal("plugin should start off")
	}

	if err := svc.SetEnabled(context.Background(), SelfHostedGit, true); err != nil {
		t.Fatal(err)
	}
	if !svc.IsEnabled(SelfHostedGit) {
		t.Fatal("expected enabled")
	}
	if !Enabled(mem.cur.EnabledPlugins, SelfHostedGit) {
		t.Fatalf("persisted = %v", mem.cur.EnabledPlugins)
	}

	if err := svc.SetEnabled(context.Background(), SelfHostedGit, false); err != nil {
		t.Fatal(err)
	}
	if svc.IsEnabled(SelfHostedGit) {
		t.Fatal("expected disabled")
	}
	if len(mem.cur.EnabledPlugins) != 0 {
		t.Fatalf("persisted after disable = %v", mem.cur.EnabledPlugins)
	}
}

func TestServiceRejectsUnknown(t *testing.T) {
	svc := &Service{Settings: &memSettings{}}
	if err := svc.SetEnabled(context.Background(), "nope", true); err == nil {
		t.Fatal("expected error")
	}
}
