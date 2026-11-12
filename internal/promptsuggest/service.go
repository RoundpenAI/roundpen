package promptsuggest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/settings"
)

// AssistantInput is the assistant snapshot needed to build suggestions.
// Defined here to avoid an import cycle with package assistant.
type AssistantInput struct {
	ID           string
	Name         string
	Bio          string
	Kind         string
	Capabilities string
	Network      string
	Grants       []string
}

// Service resolves configured prompts or generates via LLM.
type Service struct {
	Settings func() settings.SuggestedPrompts
	Sessions *agentsession.Store
	LLM      *Generator

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	prompts   []string
	expiresAt time.Time
}

const cacheTTL = 2 * time.Minute

// Result is the API payload for suggested prompts.
type Result struct {
	Prompts []string `json:"prompts"`
	Source  string   `json:"source"`
}

// Suggest returns prompts for an assistant, optionally using session context for LLM.
func (s *Service) Suggest(ctx context.Context, a AssistantInput, sessionID string) Result {
	if strings.TrimSpace(a.ID) == "" && strings.TrimSpace(a.Name) == "" {
		return Result{Prompts: []string{}, Source: SourceNone}
	}
	if configured := clampPrompts(FromConfig(s.settings(), a.Kind)); len(configured) > 0 {
		return Result{Prompts: configured, Source: SourceConfig}
	}
	if s.LLM == nil {
		return Result{Prompts: []string{}, Source: SourceNone}
	}
	key := cacheKey(a.ID, sessionID)
	if hit, ok := s.cacheGet(key); ok {
		return Result{Prompts: hit, Source: SourceLLM}
	}
	in := Context{
		Name:         a.Name,
		Bio:          a.Bio,
		Kind:         a.Kind,
		Capabilities: a.Capabilities,
		Network:      a.Network,
		Grants:       a.Grants,
		Recent:       s.recentLines(ctx, sessionID),
	}
	prompts, err := s.LLM.Generate(ctx, in)
	if err != nil || len(prompts) == 0 {
		return Result{Prompts: []string{}, Source: SourceNone}
	}
	prompts = clampPrompts(prompts)
	s.cachePut(key, prompts)
	return Result{Prompts: prompts, Source: SourceLLM}
}

func (s *Service) settings() settings.SuggestedPrompts {
	if s != nil && s.Settings != nil {
		return s.Settings()
	}
	return settings.SuggestedPrompts{}
}

func (s *Service) recentLines(ctx context.Context, sessionID string) []string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || s.Sessions == nil {
		return nil
	}
	msgs, err := s.Sessions.ListMessages(ctx, sessionID, 40)
	if err != nil || len(msgs) == 0 {
		return nil
	}
	var lines []string
	for _, m := range msgs {
		if m == nil {
			continue
		}
		switch m.Role {
		case agentsession.RoleUser, agentsession.RoleAssistant:
		default:
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", m.Role, content))
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return lines
}

func cacheKey(assistantID, sessionID string) string {
	return strings.TrimSpace(assistantID) + "|" + strings.TrimSpace(sessionID)
}

func (s *Service) cacheGet(key string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		return nil, false
	}
	ent, ok := s.cache[key]
	if !ok || time.Now().After(ent.expiresAt) {
		return nil, false
	}
	out := append([]string(nil), ent.prompts...)
	return out, true
}

func (s *Service) cachePut(key string, prompts []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = map[string]cacheEntry{}
	}
	s.cache[key] = cacheEntry{
		prompts:   append([]string(nil), prompts...),
		expiresAt: time.Now().Add(cacheTTL),
	}
}
