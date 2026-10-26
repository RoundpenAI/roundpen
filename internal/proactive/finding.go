// Package proactive finds quiet-period work signals and surfaces a single
// rate-limited recall digest (session event + optional Web Push).
package proactive

import (
	"fmt"
	"strings"
	"time"
)

const (
	SourceIssues   = "issues"
	SourceGitHub   = "github"
	SourceGmail    = "gmail"
	SourceOnThisDay = "on_this_day"
)

// Finding is one actionable signal for the digest.
type Finding struct {
	Source  string `json:"source"`
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	URL     string `json:"url,omitempty"`
	Skipped string `json:"skipped,omitempty"` // non-empty = source stub / unavailable reason
}

// Digest is the user-visible recall payload.
type Digest struct {
	UserID    string    `json:"userId"`
	Findings  []Finding `json:"findings"`
	CreatedAt time.Time `json:"createdAt"`
}

// FormatText renders a short Chinese digest for chat / push body.
func (d Digest) FormatText(maxItems int) string {
	if maxItems <= 0 {
		maxItems = 8
	}
	var b strings.Builder
	b.WriteString("安静一会儿了，帮你看了一眼手头的事：\n")
	n := 0
	for _, f := range d.Findings {
		if strings.TrimSpace(f.Skipped) != "" {
			continue
		}
		if strings.TrimSpace(f.Title) == "" {
			continue
		}
		n++
		if n > maxItems {
			b.WriteString(fmt.Sprintf("…还有 %d 条，打开控制台继续看。\n", len(d.Findings)-maxItems))
			break
		}
		line := fmt.Sprintf("%d. [%s] %s", n, sourceLabel(f.Source), f.Title)
		if d := strings.TrimSpace(f.Detail); d != "" {
			line += " — " + truncateRunes(d, 80)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if n == 0 {
		return ""
	}
	b.WriteString("需要我动手时，直接在对话里说一声即可。")
	return strings.TrimSpace(b.String())
}

func sourceLabel(src string) string {
	switch src {
	case SourceIssues:
		return "议题"
	case SourceGitHub:
		return "GitHub"
	case SourceGmail:
		return "邮件"
	case SourceOnThisDay:
		return "今日回忆"
	default:
		return src
	}
}

func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
