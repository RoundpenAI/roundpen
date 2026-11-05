package proactive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/photos"
)

// PhotoGrantLister returns directory grants that may hold photos for a user.
type PhotoGrantLister interface {
	ListPhotoGrants(ctx context.Context, userID string) ([]assistant.DirectoryGrant, error)
}

// OnThisDay walks granted directories for files whose name or mod-time month/day
// matches "today" in prior years. Lightweight P0: filename patterns and mtime;
// not a full EXIF index.
type OnThisDay struct {
	Grants PhotoGrantLister
	// MaxFiles caps filesystem walks per user.
	MaxFiles int
}

// Scan implements OnThisDayScanner.
func (o *OnThisDay) Scan(ctx context.Context, userID string, now time.Time) ([]Finding, error) {
	if o == nil || o.Grants == nil {
		return []Finding{{Source: SourceOnThisDay, Skipped: "on-this-day extension point not wired"}}, nil
	}
	grants, err := o.Grants.ListPhotoGrants(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return []Finding{{Source: SourceOnThisDay, Skipped: "no directory grants — authorize a photos folder first"}}, nil
	}
	roots := make([]string, 0, len(grants))
	for _, g := range grants {
		if p := strings.TrimSpace(g.Path); p != "" {
			roots = append(roots, p)
		}
	}
	max := o.MaxFiles
	if max <= 0 {
		max = 2000
	}
	hits, err := photos.ScanOnThisDay(ctx, photos.ScanOptions{
		Roots: roots, Now: now, MaxFiles: max, MaxHits: 3,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Finding, 0, len(hits))
	for _, h := range hits {
		detail := h.Path
		if h.YearsAgo > 0 {
			detail = fmt.Sprintf("%d 年前今日 · %s", h.YearsAgo, h.Path)
		}
		out = append(out, Finding{
			Source: SourceOnThisDay,
			Title:  h.Name,
			Detail: detail,
		})
	}
	return out, nil
}
