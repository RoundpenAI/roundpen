package automode

import (
	"strings"
	"unicode/utf8"
)

// DigestMessage is one conversation turn offered to the classifier.
type DigestMessage struct {
	Role    string
	Content string
}

// RecentDigest renders the most recent user/assistant messages for classifier
// context. Tool results are deliberately excluded so content the agent read
// (files, web pages) cannot steer the classifier.
func RecentDigest(msgs []DigestMessage, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = 4000
	}
	const perMessageCap = 1500
	budget := maxRunes
	var kept []string
	for i := len(msgs) - 1; i >= 0 && budget > 0; i-- {
		role := strings.TrimSpace(msgs[i].Role)
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msgs[i].Content)
		if content == "" {
			continue
		}
		sep := 0
		if len(kept) > 0 {
			sep = 1
		}
		if budget-sep <= 0 {
			break
		}
		content = truncateRunes(content, perMessageCap)
		line := truncateRunes(role+": "+content, budget-sep)
		kept = append(kept, line)
		budget -= utf8.RuneCountInString(line) + sep
	}
	for l, r := 0, len(kept)-1; l < r; l, r = l+1, r-1 {
		kept[l], kept[r] = kept[r], kept[l]
	}
	return strings.Join(kept, "\n")
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
