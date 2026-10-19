package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/connectors"
)

// GoogleAPI is the subset of connectors.Service the mail/calendar tools need.
type GoogleAPI interface {
	SearchGmail(ctx context.Context, userID, q string, maxResults int) ([]connectors.GmailMessageSummary, error)
	GetGmail(ctx context.Context, userID, messageID string) (*connectors.GmailMessage, error)
	ListCalendarEvents(ctx context.Context, userID, calendarID, timeMin, timeMax string, maxResults int) ([]connectors.CalendarEvent, error)
	GetCalendarEvent(ctx context.Context, userID, calendarID, eventID string) (*connectors.CalendarEvent, error)
}

// GoogleBinder exposes read-only Gmail and Calendar tools.
type GoogleBinder struct {
	API GoogleAPI
}

// RegisterGoogle adds Gmail/Calendar tools when a binder is configured.
func RegisterGoogle(r *Registry, b *GoogleBinder) {
	if r == nil || b == nil || b.API == nil {
		return
	}
	r.Register(Tool{
		Name: "GmailSearch",
		Description: "Search the user's connected Gmail (read-only). Uses Gmail search syntax " +
			"(e.g. from:alice newer_than:7d). Returns message ids, subjects, and snippets. " +
			"If Google is not connected, tell the user to open Settings → Connectors.",
		Parameters: objectSchema(map[string]any{
			"q":          map[string]any{"type": "string", "description": "Gmail search query"},
			"maxResults": map[string]any{"type": "integer", "description": "Max messages (default 10, max 50)"},
		}, "q"),
		Mutating: false,
		Call:     b.gmailSearch,
	})
	r.Register(Tool{
		Name: "GmailGet",
		Description: "Read one Gmail message by id (read-only). Prefer after GmailSearch. " +
			"Returns headers and a truncated plain-text body.",
		Parameters: objectSchema(map[string]any{
			"id": map[string]any{"type": "string", "description": "Gmail message id"},
		}, "id"),
		Mutating: false,
		Call:     b.gmailGet,
	})
	r.Register(Tool{
		Name: "CalendarListEvents",
		Description: "List events on the user's Google Calendar (read-only). " +
			"timeMin/timeMax are RFC3339 timestamps. Default calendar is primary. " +
			"If Google is not connected, tell the user to open Settings → Connectors.",
		Parameters: objectSchema(map[string]any{
			"timeMin":    map[string]any{"type": "string", "description": "RFC3339 start (default: now)"},
			"timeMax":    map[string]any{"type": "string", "description": "RFC3339 end (optional)"},
			"calendarId": map[string]any{"type": "string", "description": "Calendar id (default primary)"},
			"maxResults": map[string]any{"type": "integer", "description": "Max events (default 10, max 50)"},
		}),
		Mutating: false,
		Call:     b.calendarList,
	})
	r.Register(Tool{
		Name:        "CalendarGetEvent",
		Description: "Get one Google Calendar event by id (read-only).",
		Parameters: objectSchema(map[string]any{
			"id":         map[string]any{"type": "string", "description": "Event id"},
			"calendarId": map[string]any{"type": "string", "description": "Calendar id (default primary)"},
		}, "id"),
		Mutating: false,
		Call:     b.calendarGet,
	})
}

func (b *GoogleBinder) gmailSearch(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
	var in struct {
		Q          string `json:"q"`
		MaxResults int    `json:"maxResults"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Q) == "" {
		return "", fmt.Errorf("q is required")
	}
	rows, err := b.API.SearchGmail(ctx, actor.Username, in.Q, in.MaxResults)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(map[string]any{"messages": rows})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (b *GoogleBinder) gmailGet(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	msg, err := b.API.GetGmail(ctx, actor.Username, in.ID)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (b *GoogleBinder) calendarList(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
	var in struct {
		TimeMin    string `json:"timeMin"`
		TimeMax    string `json:"timeMax"`
		CalendarID string `json:"calendarId"`
		MaxResults int    `json:"maxResults"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	rows, err := b.API.ListCalendarEvents(ctx, actor.Username, in.CalendarID, in.TimeMin, in.TimeMax, in.MaxResults)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(map[string]any{"events": rows})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (b *GoogleBinder) calendarGet(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
	var in struct {
		ID         string `json:"id"`
		CalendarID string `json:"calendarId"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	ev, err := b.API.GetCalendarEvent(ctx, actor.Username, in.CalendarID, in.ID)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
