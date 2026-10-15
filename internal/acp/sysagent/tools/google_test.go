package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/connectors"
)

type stubGoogle struct {
	err error
}

func (s stubGoogle) SearchGmail(ctx context.Context, userID, q string, maxResults int) ([]connectors.GmailMessageSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []connectors.GmailMessageSummary{{ID: "m1", Subject: "hi", From: "a@b.c"}}, nil
}

func (s stubGoogle) GetGmail(ctx context.Context, userID, messageID string) (*connectors.GmailMessage, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &connectors.GmailMessage{ID: messageID, Body: "body"}, nil
}

func (s stubGoogle) ListCalendarEvents(ctx context.Context, userID, calendarID, timeMin, timeMax string, maxResults int) ([]connectors.CalendarEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []connectors.CalendarEvent{{ID: "e1", Summary: "meet"}}, nil
}

func (s stubGoogle) GetCalendarEvent(ctx context.Context, userID, calendarID, eventID string) (*connectors.CalendarEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &connectors.CalendarEvent{ID: eventID, Summary: "meet"}, nil
}

func TestGoogleToolsRequireConnection(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterGoogle(reg, &tools.GoogleBinder{API: stubGoogle{err: connectors.ErrNotConnected}})
	tool, ok := reg.Get("GmailSearch")
	if !ok {
		t.Fatal("GmailSearch not registered")
	}
	_, err := tool.Call(context.Background(), tools.Actor{Username: "u"}, json.RawMessage(`{"q":"in:inbox"}`))
	if !errors.Is(err, connectors.ErrNotConnected) {
		t.Fatalf("err = %v", err)
	}
}

func TestGoogleToolsHappyPath(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterGoogle(reg, &tools.GoogleBinder{API: stubGoogle{}})
	for _, name := range []string{"GmailSearch", "GmailGet", "CalendarListEvents", "CalendarGetEvent"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if tool.Mutating {
			t.Fatalf("%s should be read-only", name)
		}
	}
	tool, ok := reg.Get("GmailSearch")
	if !ok {
		t.Fatal("missing GmailSearch")
	}
	out, err := tool.Call(context.Background(), tools.Actor{Username: "u"}, json.RawMessage(`{"q":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("invalid json: %s", out)
	}
}

func TestRegisterGoogleNilSkipped(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterGoogle(reg, nil)
	tools.RegisterGoogle(reg, &tools.GoogleBinder{})
	if _, ok := reg.Get("GmailSearch"); ok {
		t.Fatal("should not register without API")
	}
}
