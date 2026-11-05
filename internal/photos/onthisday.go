// Package photos finds anniversary photos under granted host directories.
package photos

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Hit is one matching photo.
type Hit struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	YearsAgo int    `json:"yearsAgo,omitempty"`
	Match    string `json:"match"` // filename | mtime
}

// ScanOptions controls an on-this-day walk.
type ScanOptions struct {
	Roots    []string
	Now      time.Time
	MaxFiles int // walk budget; default 5000
	MaxHits  int // default 50
	// YearsAgo when >0 only returns that anniversary (e.g. 1 = last year).
	YearsAgo int
}

// ScanOnThisDay walks roots for images whose name or mtime matches month/day
// in a prior year. Filename patterns: YYYY-MM-DD, YYYYMMDD / IMG_YYYYMMDD.
func ScanOnThisDay(ctx context.Context, opt ScanOptions) ([]Hit, error) {
	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	maxFiles := opt.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 5000
	}
	maxHits := opt.MaxHits
	if maxHits <= 0 {
		maxHits = 50
	}
	month, day := int(now.Month()), now.Day()
	var hits []Hit
	budget := maxFiles
	for _, root := range opt.Roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || !IsImageName(name) {
				return nil
			}
			budget--
			if budget < 0 {
				return fs.SkipAll
			}
			if y, ok := MatchAnniversaryName(name, month, day, now.Year()); ok {
				if opt.YearsAgo > 0 && y != opt.YearsAgo {
					return nil
				}
				hits = append(hits, Hit{Path: p, Name: name, YearsAgo: y, Match: "filename"})
			} else {
				info, ierr := d.Info()
				if ierr != nil {
					return nil
				}
				mt := info.ModTime()
				if mt.Year() < now.Year() && int(mt.Month()) == month && mt.Day() == day {
					years := now.Year() - mt.Year()
					if opt.YearsAgo > 0 && years != opt.YearsAgo {
						return nil
					}
					hits = append(hits, Hit{Path: p, Name: name, YearsAgo: years, Match: "mtime"})
				}
			}
			if len(hits) >= maxHits {
				return fs.SkipAll
			}
			return nil
		})
		if err != nil && err != fs.SkipAll && ctx.Err() == nil {
			return hits, err
		}
		if len(hits) >= maxHits {
			break
		}
	}
	if len(hits) > maxHits {
		hits = hits[:maxHits]
	}
	return hits, nil
}

// IsImageName reports common photo extensions.
func IsImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif":
		return true
	}
	return false
}

// MatchAnniversaryName returns years-ago when the basename encodes month/day
// with a prior year.
func MatchAnniversaryName(name string, month, day, thisYear int) (yearsAgo int, ok bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	md := fmt.Sprintf("%02d-%02d", month, day)
	compactMD := fmt.Sprintf("%02d%02d", month, day)
	if !strings.Contains(base, "-"+md) && !strings.Contains(base, compactMD) {
		return 0, false
	}
	for y := thisYear - 1; y >= thisYear-40; y-- {
		ymd := fmt.Sprintf("%04d-%02d-%02d", y, month, day)
		ymdC := fmt.Sprintf("%04d%02d%02d", y, month, day)
		if strings.Contains(base, ymd) || strings.Contains(base, ymdC) {
			return thisYear - y, true
		}
	}
	return 0, false
}

// TakenDateFromName extracts a calendar date from common camera filenames.
func TakenDateFromName(name string) (time.Time, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	// YYYY-MM-DD or YYYYMMDD
	for i := 0; i+10 <= len(base); i++ {
		chunk := base[i : i+10]
		if t, err := time.Parse("2006-01-02", chunk); err == nil {
			return t, true
		}
	}
	for i := 0; i+8 <= len(base); i++ {
		chunk := base[i : i+8]
		if !allDigits(chunk) {
			continue
		}
		if t, err := time.Parse("20060102", chunk); err == nil {
			y := t.Year()
			if y >= 1990 && y <= time.Now().Year()+1 {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FileInfoModTime is a tiny hook for tests that want to avoid touching disk beyond Walk.
var _ = os.ErrNotExist
