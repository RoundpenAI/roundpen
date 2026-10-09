package connectors

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GmailMessageSummary is a light list row from GmailSearch.
type GmailMessageSummary struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	Subject  string `json:"subject,omitempty"`
	From     string `json:"from,omitempty"`
	Date     string `json:"date,omitempty"`
}

// GmailMessage is a fuller message from GmailGet.
type GmailMessage struct {
	ID      string `json:"id"`
	Subject string `json:"subject,omitempty"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Date    string `json:"date,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Body    string `json:"body,omitempty"`
}

// CalendarEvent is a calendar event summary.
type CalendarEvent struct {
	ID          string `json:"id"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Status      string `json:"status,omitempty"`
	HTMLLink    string `json:"htmlLink,omitempty"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	AllDay      bool   `json:"allDay,omitempty"`
}

// SearchGmail lists messages matching q (Gmail search syntax).
func (s *Service) SearchGmail(ctx context.Context, userID, q string, maxResults int) ([]GmailMessageSummary, error) {
	token, err := s.AccessToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	maxResults = clampMax(maxResults)
	u := gmailAPIBase + "/users/me/messages?" + url.Values{
		"q":          {q},
		"maxResults": {fmt.Sprintf("%d", maxResults)},
	}.Encode()
	var list struct {
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"messages"`
	}
	if err := s.googleGET(ctx, token, u, &list); err != nil {
		return nil, err
	}
	out := make([]GmailMessageSummary, 0, len(list.Messages))
	for _, m := range list.Messages {
		sum := GmailMessageSummary{ID: m.ID, ThreadID: m.ThreadID}
		meta, err := s.getGmailMetadata(ctx, token, m.ID)
		if err == nil {
			sum.Snippet = meta.Snippet
			sum.Subject = meta.Subject
			sum.From = meta.From
			sum.Date = meta.Date
		}
		out = append(out, sum)
	}
	return out, nil
}

// GetGmail returns one message with a truncated plain-text body.
func (s *Service) GetGmail(ctx context.Context, userID, messageID string) (*GmailMessage, error) {
	token, err := s.AccessToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil, fmt.Errorf("message id is required")
	}
	u := gmailAPIBase + "/users/me/messages/" + url.PathEscape(messageID) + "?format=full"
	var raw gmailMessageRaw
	if err := s.googleGET(ctx, token, u, &raw); err != nil {
		return nil, err
	}
	msg := &GmailMessage{
		ID:      raw.ID,
		Snippet: raw.Snippet,
		Subject: header(raw.Payload.Headers, "Subject"),
		From:    header(raw.Payload.Headers, "From"),
		To:      header(raw.Payload.Headers, "To"),
		Date:    header(raw.Payload.Headers, "Date"),
		Body:    truncateBody(extractPlainBody(raw.Payload)),
	}
	return msg, nil
}

func (s *Service) getGmailMetadata(ctx context.Context, token, id string) (*GmailMessageSummary, error) {
	u := gmailAPIBase + "/users/me/messages/" + url.PathEscape(id) +
		"?format=metadata&metadataHeaders=Subject&metadataHeaders=From&metadataHeaders=Date"
	var raw gmailMessageRaw
	if err := s.googleGET(ctx, token, u, &raw); err != nil {
		return nil, err
	}
	return &GmailMessageSummary{
		ID:      raw.ID,
		Snippet: raw.Snippet,
		Subject: header(raw.Payload.Headers, "Subject"),
		From:    header(raw.Payload.Headers, "From"),
		Date:    header(raw.Payload.Headers, "Date"),
	}, nil
}

// ListCalendarEvents lists events on calendarId (default primary) in [timeMin, timeMax).
func (s *Service) ListCalendarEvents(ctx context.Context, userID, calendarID, timeMin, timeMax string, maxResults int) ([]CalendarEvent, error) {
	token, err := s.AccessToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(calendarID) == "" {
		calendarID = "primary"
	}
	maxResults = clampMax(maxResults)
	q := url.Values{}
	q.Set("singleEvents", "true")
	q.Set("orderBy", "startTime")
	q.Set("maxResults", fmt.Sprintf("%d", maxResults))
	if timeMin != "" {
		q.Set("timeMin", timeMin)
	} else {
		q.Set("timeMin", s.now().Format(time.RFC3339))
	}
	if timeMax != "" {
		q.Set("timeMax", timeMax)
	}
	u := calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) + "/events?" + q.Encode()
	var list struct {
		Items []calendarEventRaw `json:"items"`
	}
	if err := s.googleGET(ctx, token, u, &list); err != nil {
		return nil, err
	}
	out := make([]CalendarEvent, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, mapCalendarEvent(it))
	}
	return out, nil
}

// GetCalendarEvent fetches one event.
func (s *Service) GetCalendarEvent(ctx context.Context, userID, calendarID, eventID string) (*CalendarEvent, error) {
	token, err := s.AccessToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(calendarID) == "" {
		calendarID = "primary"
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("event id is required")
	}
	u := calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) +
		"/events/" + url.PathEscape(eventID)
	var raw calendarEventRaw
	if err := s.googleGET(ctx, token, u, &raw); err != nil {
		return nil, err
	}
	ev := mapCalendarEvent(raw)
	return &ev, nil
}

func (s *Service) googleGET(ctx context.Context, token, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("google api %s: %s", resp.Status, truncateErr(body))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}

type gmailMessageRaw struct {
	ID      string `json:"id"`
	Snippet string `json:"snippet"`
	Payload gmailPart `json:"payload"`
}

type gmailPart struct {
	MimeType string         `json:"mimeType"`
	Headers  []gmailHeader  `json:"headers"`
	Body     gmailBody      `json:"body"`
	Parts    []gmailPart    `json:"parts"`
}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailBody struct {
	Data string `json:"data"`
}

type calendarEventRaw struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Status      string `json:"status"`
	HTMLLink    string `json:"htmlLink"`
	Start       struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		Date     string `json:"date"`
	} `json:"end"`
}

func mapCalendarEvent(it calendarEventRaw) CalendarEvent {
	ev := CalendarEvent{
		ID: it.ID, Summary: it.Summary, Description: it.Description,
		Location: it.Location, Status: it.Status, HTMLLink: it.HTMLLink,
	}
	if it.Start.DateTime != "" {
		ev.Start = it.Start.DateTime
	} else if it.Start.Date != "" {
		ev.Start = it.Start.Date
		ev.AllDay = true
	}
	if it.End.DateTime != "" {
		ev.End = it.End.DateTime
	} else if it.End.Date != "" {
		ev.End = it.End.Date
	}
	return ev
}

func header(hs []gmailHeader, name string) string {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func extractPlainBody(p gmailPart) string {
	if strings.EqualFold(p.MimeType, "text/plain") && p.Body.Data != "" {
		return decodeB64(p.Body.Data)
	}
	for _, part := range p.Parts {
		if body := extractPlainBody(part); body != "" {
			return body
		}
	}
	// Fall back to any text/html stripped lightly, or raw body.
	if strings.HasPrefix(strings.ToLower(p.MimeType), "text/") && p.Body.Data != "" {
		return decodeB64(p.Body.Data)
	}
	for _, part := range p.Parts {
		if strings.HasPrefix(strings.ToLower(part.MimeType), "text/") && part.Body.Data != "" {
			return decodeB64(part.Body.Data)
		}
	}
	return ""
}

func decodeB64(data string) string {
	// Gmail uses URL-safe base64 without padding.
	b, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(data)
	}
	if err != nil {
		return ""
	}
	return string(b)
}

func truncateBody(s string) string {
	if len(s) <= maxMessageBytes {
		return s
	}
	return s[:maxMessageBytes] + "\n…[truncated]"
}

func clampMax(n int) int {
	if n <= 0 {
		return defaultMaxResults
	}
	if n > hardMaxResults {
		return hardMaxResults
	}
	return n
}
