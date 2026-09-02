package memory

import "testing"

func TestContentFromMessages(t *testing.T) {
	got := contentFromMessages([]Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "  "},
	})
	want := "user: hi\nassistant: hello"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseFilters(t *testing.T) {
	f := parseFilters(map[string]any{
		"agent_id": "a1",
		"user_id":  "u1",
		"run_id":   "r1",
		"kind":     "fact",
	})
	if f.AgentID != "a1" || f.UserID != "u1" || f.RunID != "r1" || f.Kind != LongFact {
		t.Fatalf("%+v", f)
	}
}
