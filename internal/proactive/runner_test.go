package proactive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/issue"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/webpush"
)

type memUsers struct{ users []storage.User }

func (m memUsers) ListAll(context.Context) ([]storage.User, error) { return m.users, nil }

type memAssist struct{ a *assistant.Assistant }

func (m memAssist) EnsureSystem(context.Context, string) (*assistant.Assistant, error) {
	return m.a, nil
}

type memSessions struct {
	sess *agentsession.Session
	msgs []string
}

func (m *memSessions) ListByAssistant(context.Context, string, string, int) ([]*agentsession.Session, error) {
	if m.sess == nil {
		return nil, nil
	}
	return []*agentsession.Session{m.sess}, nil
}

func (m *memSessions) AddMessage(_ context.Context, _, role, content string, _ any) (*agentsession.Message, error) {
	m.msgs = append(m.msgs, role+":"+content)
	return &agentsession.Message{}, nil
}

type memQuiet struct {
	at time.Time
	ok bool
}

func (m memQuiet) LastUserMessageAt(context.Context, string) (time.Time, bool, error) {
	return m.at, m.ok, nil
}

type memPush struct{ n int }

func (p *memPush) Notify(context.Context, string, webpush.Payload) { p.n++ }

func TestRunnerDeliversWhenQuiet(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	sessStore := &memSessions{sess: &agentsession.Session{ID: "s1", UserID: "alice"}}
	push := &memPush{}

	r := &Runner{
		Users:      memUsers{users: []storage.User{{Username: "alice"}}},
		Assistants: memAssist{a: &assistant.Assistant{ID: "sys"}},
		Sessions:   sessStore,
		Quiet:      memQuiet{at: now.Add(-3 * time.Hour), ok: true},
		Store:      &Store{}, // nil DB: Get/MarkNotified no-op
		Scanner: &Scanner{
			Issues: stubIssues{rows: []issue.Issue{{
				Key: "ISS-1", Title: "Do thing", Status: issue.StatusInProgress,
			}}},
			GitHub: GitHubTokenFunc(func(context.Context, string) (string, error) { return "", nil }),
			Gmail:  stubGmail{},
		},
		Push:     push,
		QuietFor: 2 * time.Hour,
		Cooldown:  12 * time.Hour,
		Now:      func() time.Time { return now },
	}
	r.TickOnce(context.Background())
	if len(sessStore.msgs) != 1 {
		t.Fatalf("want 1 event message, got %v", sessStore.msgs)
	}
	if !strings.HasPrefix(sessStore.msgs[0], "event:") {
		t.Fatalf("want event role, got %q", sessStore.msgs[0])
	}
	if push.n != 1 {
		t.Fatalf("want push, got %d", push.n)
	}
}

func TestRunnerSkipsWhenRecentlyActive(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	sessStore := &memSessions{sess: &agentsession.Session{ID: "s1"}}
	r := &Runner{
		Users:      memUsers{users: []storage.User{{Username: "alice"}}},
		Assistants: memAssist{a: &assistant.Assistant{ID: "sys"}},
		Sessions:   sessStore,
		Quiet:      memQuiet{at: now.Add(-30 * time.Minute), ok: true},
		Scanner: &Scanner{
			Issues: stubIssues{rows: []issue.Issue{{
				Key: "ISS-1", Title: "Do thing", Status: issue.StatusInProgress,
			}}},
			GitHub: GitHubTokenFunc(func(context.Context, string) (string, error) { return "", nil }),
		},
		QuietFor: 2 * time.Hour,
		Now:      func() time.Time { return now },
	}
	r.TickOnce(context.Background())
	if len(sessStore.msgs) != 0 {
		t.Fatalf("should not notify while active: %v", sessStore.msgs)
	}
}
