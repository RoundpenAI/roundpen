package proactive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Scanner gathers quiet-period findings from configured sources.
type Scanner struct {
	Issues   IssueLister
	Gmail    GmailSearcher
	GitHub   GitHubToken
	OnThisDay OnThisDayScanner
	HTTP     *http.Client
	Log      *slog.Logger
	// PerSourceLimit caps findings contributed by each source.
	PerSourceLimit int
}

// ScanUser runs all sources and returns a digest (possibly with only skip markers).
func (s *Scanner) ScanUser(ctx context.Context, userID string, now time.Time) (Digest, error) {
	userID = strings.TrimSpace(userID)
	d := Digest{UserID: userID, CreatedAt: now.UTC(), Findings: nil}
	if userID == "" {
		return d, nil
	}
	limit := s.PerSourceLimit
	if limit <= 0 {
		limit = 5
	}

	appendSrc := func(rows []Finding, err error, src string) {
		if err != nil {
			s.log().Warn("proactive source", "source", src, "user", userID, "err", err)
			d.Findings = append(d.Findings, Finding{Source: src, Skipped: err.Error()})
			return
		}
		d.Findings = append(d.Findings, rows...)
	}

	rows, err := scanIssues(ctx, s.Issues, userID, limit)
	appendSrc(rows, err, SourceIssues)

	rows, err = scanGitHub(ctx, s.GitHub, s.HTTP, userID, limit)
	appendSrc(rows, err, SourceGitHub)

	rows, err = scanGmail(ctx, s.Gmail, userID, limit)
	appendSrc(rows, err, SourceGmail)

	if s.OnThisDay != nil {
		rows, err = s.OnThisDay.Scan(ctx, userID, now)
		appendSrc(rows, err, SourceOnThisDay)
	} else {
		d.Findings = append(d.Findings, Finding{
			Source:  SourceOnThisDay,
			Skipped: "extension point: wire OnThisDay when photo grants are ready",
		})
	}
	return d, nil
}

// Actionable returns findings that should surface (not stubs).
func Actionable(findings []Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if strings.TrimSpace(f.Skipped) != "" {
			continue
		}
		if strings.TrimSpace(f.Title) == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// DigestHash fingerprints actionable titles so identical digests can be suppressed.
func DigestHash(findings []Finding) string {
	var b strings.Builder
	for _, f := range Actionable(findings) {
		fmt.Fprintf(&b, "%s|%s|%s\n", f.Source, f.Title, f.URL)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

func (s *Scanner) log() *slog.Logger {
	if s != nil && s.Log != nil {
		return s.Log
	}
	return slog.Default()
}
