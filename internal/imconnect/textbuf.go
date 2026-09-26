package imconnect

import "strings"

// maxTextBatchRunes is a soft cap when coalescing several short paragraphs
// into one EventText. One paragraph may exceed this and is still flushed whole.
const maxTextBatchRunes = 800

// takeParagraphBatch pulls one or more complete paragraphs (split on blank
// lines) from buf. ok is false when buf has no completed paragraph yet.
func takeParagraphBatch(buf string, maxRunes int) (ready, rest string, ok bool) {
	if maxRunes <= 0 {
		maxRunes = maxTextBatchRunes
	}
	if strings.Index(buf, "\n\n") < 0 {
		return "", buf, false
	}

	var b strings.Builder
	for {
		idx := strings.Index(buf, "\n\n")
		if idx < 0 {
			break
		}
		para := strings.TrimRight(buf[:idx], "\n")
		next := strings.TrimLeft(buf[idx+2:], "\n")
		if para == "" {
			// Skip leading / repeated blank lines.
			buf = next
			continue
		}

		candidate := para
		if b.Len() > 0 {
			candidate = b.String() + "\n\n" + para
		}
		if b.Len() > 0 && runeLen(candidate) > maxRunes {
			// Keep what we already have; leave this paragraph for the next batch.
			return b.String(), buf, true
		}
		b.Reset()
		b.WriteString(candidate)
		buf = next

		// Prefer shipping a reasonable batch rather than waiting forever for
		// more short paragraphs.
		if runeLen(b.String()) >= maxRunes {
			break
		}
		if strings.Index(buf, "\n\n") < 0 {
			break
		}
	}
	if b.Len() == 0 {
		return "", buf, false
	}
	return b.String(), buf, true
}

func runeLen(s string) int {
	return len([]rune(s))
}
