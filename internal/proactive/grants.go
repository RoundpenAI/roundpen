package proactive

import (
	"context"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/assistant"
)

// AssistantGrants collects DirectoryGrant rows across a user's assistants.
type AssistantGrants struct {
	Assistants interface {
		ListByUser(ctx context.Context, userID string) ([]*assistant.Assistant, error)
	}
}

// ListPhotoGrants implements PhotoGrantLister.
func (g AssistantGrants) ListPhotoGrants(ctx context.Context, userID string) ([]assistant.DirectoryGrant, error) {
	if g.Assistants == nil {
		return nil, nil
	}
	list, err := g.Assistants.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	var out []assistant.DirectoryGrant
	seen := map[string]struct{}{}
	for _, a := range list {
		if a == nil {
			continue
		}
		for _, d := range a.DirectoryGrants {
			p := strings.TrimSpace(d.Path)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, d)
		}
	}
	return out, nil
}
