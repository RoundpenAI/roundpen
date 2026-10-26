package proactive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/assistant"
)

// PhotoGrantLister returns directory grants that may hold photos for a user.
type PhotoGrantLister interface {
	ListPhotoGrants(ctx context.Context, userID string) ([]assistant.DirectoryGrant, error)
}

// OnThisDay walks granted directories for files whose name or mod-time month/day
// matches "today" in prior years. Lightweight P0: filename patterns like
// 2019-10-26* / IMG_20191026* and mtime anniversary; not a full EXIF index.
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
	max := o.MaxFiles
	if max <= 0 {
		max = 2000
	}
	month, day := int(now.Month()), now.Day()
	var hits []Finding
	for _, g := range grants {
		root := strings.TrimSpace(g.Path)
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") {
				return nil
			}
			if !isImageName(name) {
				return nil
			}
			max--
			if max < 0 {
				return filepath.SkipAll
			}
			if matchAnniversaryName(name, month, day, now.Year()) {
				hits = append(hits, Finding{
					Source: SourceOnThisDay,
					Title:  name,
					Detail: p,
				})
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			mt := info.ModTime()
			if mt.Year() < now.Year() && int(mt.Month()) == month && mt.Day() == day {
				years := now.Year() - mt.Year()
				hits = append(hits, Finding{
					Source: SourceOnThisDay,
					Title:  name,
					Detail: fmtYearsAgo(years) + " · " + p,
				})
			}
			return nil
		})
		if len(hits) >= 3 {
			break
		}
	}
	if len(hits) > 3 {
		hits = hits[:3]
	}
	return hits, nil
}

func isImageName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif":
		return true
	}
	return false
}

func matchAnniversaryName(name string, month, day, thisYear int) bool {
	// YYYY-MM-DD or YYYYMMDD anywhere in the basename.
	base := strings.TrimSuffix(name, filepath.Ext(name))
	md := sprintfMD(month, day)
	compact := sprintfYMDCompact(0, month, day) // suffix MMDD
	if strings.Contains(base, "-"+md) || strings.HasSuffix(base, compact[4:]) || strings.Contains(base, compact[4:]) {
		// Prefer when a prior year is also present.
		for y := thisYear - 1; y >= thisYear-40; y-- {
			if strings.Contains(base, sprintfYMD(y, month, day)) || strings.Contains(base, sprintfYMDCompact(y, month, day)) {
				return true
			}
		}
	}
	return false
}

func sprintfMD(month, day int) string {
	return pad2(month) + "-" + pad2(day)
}

func sprintfYMD(y, month, day int) string {
	return pad4(y) + "-" + pad2(month) + "-" + pad2(day)
}

func sprintfYMDCompact(y, month, day int) string {
	if y == 0 {
		return "0000" + pad2(month) + pad2(day)
	}
	return pad4(y) + pad2(month) + pad2(day)
}

func pad2(n int) string {
	return fmt.Sprintf("%02d", n)
}

func pad4(n int) string {
	return fmt.Sprintf("%04d", n)
}

func fmtYearsAgo(n int) string {
	return fmt.Sprintf("%d 年前今日", n)
}
