package expertrun

import (
	"encoding/json"
	"regexp"
	"strings"
)

var fencedJSON = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// ParseContract extracts the return-contract JSON from the expert's final reply.
// Falls back to treating the whole reply as summary when no JSON is found.
func ParseContract(reply string) Result {
	reply = strings.TrimSpace(reply)
	out := Result{
		Status:   StatusSucceeded,
		Summary:  reply,
		PRIntent: map[string]any{"action": "none"},
	}
	if reply == "" {
		out.Status = StatusFailed
		out.Summary = "expert returned empty reply"
		return out
	}
	raw := extractJSONObject(reply)
	if raw == "" {
		// Keep prose as summary; caller may fill ChangedPaths via git.
		if len(out.Summary) > 4000 {
			out.Summary = out.Summary[:4000] + "…"
		}
		return out
	}
	var parsed struct {
		Status       string         `json:"status"`
		Summary      string         `json:"summary"`
		ChangedPaths []string       `json:"changedPaths"`
		PRIntent     map[string]any `json:"prIntent"`
		NotesForUser string         `json:"notesForUser"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return out
	}
	if s := strings.TrimSpace(parsed.Status); s != "" {
		out.Status = s
	}
	if s := strings.TrimSpace(parsed.Summary); s != "" {
		out.Summary = s
	}
	if parsed.ChangedPaths != nil {
		out.ChangedPaths = parsed.ChangedPaths
	}
	if parsed.PRIntent != nil {
		out.PRIntent = parsed.PRIntent
	}
	out.NotesForUser = strings.TrimSpace(parsed.NotesForUser)
	return out
}

func extractJSONObject(s string) string {
	if m := fencedJSON.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	// Last {...} object in the reply.
	start := strings.LastIndex(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		cand := s[start : end+1]
		if json.Valid([]byte(cand)) {
			return cand
		}
	}
	return ""
}
