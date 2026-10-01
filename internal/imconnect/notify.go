package imconnect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// Notify pushes text to the assistant's existing IM chats. It does not open a
// new chat: if this assistant has never talked on a platform, that platform is skipped.
func (s *Supervisor) Notify(ctx context.Context, assistantID, text string) error {
	if s == nil {
		return fmt.Errorf("imconnect: not running")
	}
	s.mu.Lock()
	eng := s.engines[assistantID]
	s.mu.Unlock()
	if eng == nil {
		return fmt.Errorf("imconnect: assistant %s has no IM engine", assistantID)
	}
	return eng.Notify(ctx, text)
}

// Notify sends text through each platform that already has a session.
func (e *Engine) Notify(ctx context.Context, text string) error {
	if e == nil {
		return fmt.Errorf("imconnect: no engine")
	}
	return pushText(ctx, e.platforms, e.sessions, text)
}

func pushText(ctx context.Context, platforms []core.Platform, sessionPath, text string) error {
	if sessionPath == "" {
		return fmt.Errorf("imconnect: no session store")
	}
	sm := core.NewSessionManager(sessionPath)
	idToKey, _ := sm.SessionKeyMap()
	latest := map[string]struct {
		key string
		at  time.Time
	}{}
	for id, key := range idToKey {
		sess := sm.FindByID(id)
		if sess == nil || key == "" {
			continue
		}
		platform, _, _ := core.ParseSessionKey(key)
		cur := latest[platform]
		if cur.key == "" || sess.UpdatedAt.After(cur.at) {
			latest[platform] = struct {
				key string
				at  time.Time
			}{key: key, at: sess.UpdatedAt}
		}
	}
	if len(latest) == 0 {
		return fmt.Errorf("imconnect: no previous IM conversation")
	}
	var errs []string
	sent := 0
	for _, p := range platforms {
		slot, ok := latest[p.Name()]
		if !ok {
			continue
		}
		rebuilder, ok := p.(core.ReplyContextReconstructor)
		if !ok {
			errs = append(errs, p.Name()+": cannot address an existing chat")
			continue
		}
		replyCtx, err := rebuilder.ReconstructReplyCtx(slot.key)
		if err != nil {
			errs = append(errs, p.Name()+": "+err.Error())
			continue
		}
		if err := p.Send(ctx, replyCtx, text); err != nil {
			errs = append(errs, p.Name()+": "+err.Error())
			continue
		}
		sent++
	}
	if sent == 0 {
		if len(errs) == 0 {
			return fmt.Errorf("imconnect: no platform matched an existing chat")
		}
		return fmt.Errorf("imconnect: %s", strings.Join(errs, "; "))
	}
	return nil
}
