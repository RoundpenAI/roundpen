package webpush

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	webpushlib "github.com/SherClockHolmes/webpush-go"
)

// Payload is the JSON body delivered to the service worker.
type Payload struct {
	Type  string `json:"type"` // turn_done | permission
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// Notifier is the surface runners use to fan out notifications.
type Notifier interface {
	HasSubscription(ctx context.Context, userID string) bool
	Notify(ctx context.Context, userID string, payload Payload)
}

// Service sends Web Push messages to a user's stored subscriptions.
type Service struct {
	Store  *Store
	VAPID  *VAPID
	Log    *slog.Logger
	Client *http.Client // optional; nil uses package defaults via webpush-go
}

// Configured reports whether VAPID keys are available for subscribe/send.
func (s *Service) Configured() bool {
	return s != nil && s.VAPID != nil &&
		strings.TrimSpace(s.VAPID.PublicKey) != "" &&
		strings.TrimSpace(s.VAPID.PrivateKey) != ""
}

// HasSubscription reports whether the user can receive pushes.
func (s *Service) HasSubscription(ctx context.Context, userID string) bool {
	if s == nil || s.Store == nil || !s.Configured() {
		return false
	}
	ok, err := s.Store.HasAny(ctx, userID)
	return err == nil && ok
}

// Notify fans out to every subscription for the user. Failures are logged;
// gone endpoints are pruned.
func (s *Service) Notify(ctx context.Context, userID string, payload Payload) {
	if s == nil || s.Store == nil || !s.Configured() {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	subs, err := s.Store.ListByUser(ctx, userID)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("list push subscriptions", "user", userID, "err", err)
		}
		return
	}
	if len(subs) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, sub := range subs {
		s.sendOne(ctx, sub, body)
	}
}

func (s *Service) sendOne(ctx context.Context, sub Subscription, body []byte) {
	subOpts := &webpushlib.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpushlib.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}
	opts := &webpushlib.Options{
		Subscriber:      s.VAPID.Subject,
		VAPIDPublicKey:  s.VAPID.PublicKey,
		VAPIDPrivateKey: s.VAPID.PrivateKey,
		TTL:             60,
		Urgency:         webpushlib.UrgencyHigh,
	}
	if s.Client != nil {
		opts.HTTPClient = s.Client
	}
	resp, err := webpushlib.SendNotificationWithContext(ctx, body, subOpts, opts)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("web push send", "endpoint", truncate(sub.Endpoint, 64), "err", err)
		}
		return
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK, http.StatusAccepted:
		return
	case http.StatusGone, http.StatusNotFound:
		_ = s.Store.DeleteEndpoint(ctx, sub.Endpoint)
		if s.Log != nil {
			s.Log.Info("removed stale push subscription", "status", resp.StatusCode)
		}
	default:
		if s.Log != nil {
			s.Log.Warn("web push rejected", "status", resp.StatusCode, "endpoint", truncate(sub.Endpoint, 64))
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SessionURL builds the console deep link for a chat session.
func SessionURL(assistantID, sessionID string) string {
	assistantID = strings.TrimSpace(assistantID)
	sessionID = strings.TrimSpace(sessionID)
	if assistantID == "" {
		return "/a"
	}
	if sessionID == "" {
		return "/a/" + assistantID
	}
	return "/a/" + assistantID + "/s/" + sessionID
}

// TruncateTitle shortens a session title for notification text.
func TruncateTitle(title string, max int) string {
	title = strings.TrimSpace(title)
	if max <= 0 || len(title) <= max {
		return title
	}
	if max <= 1 {
		return title[:max]
	}
	return title[:max-1] + "…"
}

// Ensure Service implements Notifier.
var _ Notifier = (*Service)(nil)

// IdleTimeout is unused externally but documents push TTL intent for tests.
const IdleTimeout = 60 * time.Second
