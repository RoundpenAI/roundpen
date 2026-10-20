package project

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

const (
	DecisionPassed           = "passed"
	DecisionChangesRequested = "changes_requested"

	VerdictApprove         = "approve"
	VerdictRequestChanges  = "request_changes"
)

// ReviewFinding is one structured issue from a reviewer worker.
type ReviewFinding struct {
	Severity string `json:"severity"` // error | warning | info | note
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	Note     string `json:"note"`
}

// ReviewResult is the structured block a reviewer should emit.
type ReviewResult struct {
	Verdict  string          `json:"verdict"` // approve | request_changes
	Summary  string          `json:"summary,omitempty"`
	Findings []ReviewFinding `json:"findings,omitempty"`
}

var fencedBlock = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?```")

// ExtractReviewResult parses the last structured review JSON from assistant text.
func ExtractReviewResult(texts ...string) *ReviewResult {
	for i := len(texts) - 1; i >= 0; i-- {
		if r := parseReviewText(texts[i]); r != nil {
			return r
		}
	}
	return nil
}

func parseReviewText(text string) *ReviewResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	blocks := fencedBlock.FindAllStringSubmatch(text, -1)
	for i := len(blocks) - 1; i >= 0; i-- {
		if r := decodeReviewJSON(strings.TrimSpace(blocks[i][1])); r != nil {
			return r
		}
	}
	// Fallback: scan for a JSON object containing "verdict".
	if idx := strings.LastIndex(text, `"verdict"`); idx >= 0 {
		start := strings.LastIndex(text[:idx], "{")
		if start >= 0 {
			if raw, ok := balancedObject(text[start:]); ok {
				if r := decodeReviewJSON(raw); r != nil {
					return r
				}
			}
		}
	}
	if strings.HasPrefix(text, "{") {
		return decodeReviewJSON(text)
	}
	return nil
}

func balancedObject(s string) (string, bool) {
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1], true
			}
		}
	}
	return "", false
}

func decodeReviewJSON(raw string) *ReviewResult {
	var r ReviewResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil
	}
	r.Verdict = normalizeVerdict(r.Verdict)
	if r.Verdict == "" {
		return nil
	}
	for i := range r.Findings {
		r.Findings[i].Severity = normalizeSeverity(r.Findings[i].Severity)
		r.Findings[i].Note = strings.TrimSpace(r.Findings[i].Note)
		r.Findings[i].Path = strings.TrimSpace(r.Findings[i].Path)
	}
	return &r
}

func normalizeVerdict(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case VerdictApprove, "approved", "pass", "lgtm", "ok":
		return VerdictApprove
	case VerdictRequestChanges, "request-changes", "changes_requested", "changes-requested", "reject", "needs_work":
		return VerdictRequestChanges
	default:
		return ""
	}
}

func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error", "critical", "high":
		return "error"
	case "warning", "warn", "medium":
		return "warning"
	case "info", "note", "low", "":
		if strings.TrimSpace(s) == "" {
			return "info"
		}
		return "info"
	default:
		return "info"
	}
}

// FormatChangesBrief builds an implementer follow-up brief from a review.
func FormatChangesBrief(review *ReviewResult, originalTitle string) string {
	var b strings.Builder
	b.WriteString("Address review feedback for: ")
	b.WriteString(strings.TrimSpace(originalTitle))
	b.WriteString("\n\n")
	if review != nil && strings.TrimSpace(review.Summary) != "" {
		b.WriteString("Reviewer summary: ")
		b.WriteString(strings.TrimSpace(review.Summary))
		b.WriteString("\n\n")
	}
	if review != nil {
		for i, f := range review.Findings {
			b.WriteString("- [")
			b.WriteString(f.Severity)
			b.WriteString("] ")
			if f.Path != "" {
				b.WriteString(f.Path)
				if f.Line > 0 {
					b.WriteString(":")
					b.WriteString(strconv.Itoa(f.Line))
				}
				b.WriteString(" — ")
			}
			b.WriteString(f.Note)
			if i+1 < len(review.Findings) {
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\n\nFix the findings above. Keep the change small and reviewable.")
	return b.String()
}

// MarshalReview stores review as JSON text (empty when nil).
func MarshalReview(r *ReviewResult) string {
	if r == nil {
		return ""
	}
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return string(b)
}

// UnmarshalReview parses stored review_json.
func UnmarshalReview(raw string) *ReviewResult {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return decodeReviewJSON(raw)
}
