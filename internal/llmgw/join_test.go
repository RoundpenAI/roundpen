package llmgw

import "testing"

func TestJoinUpstreamURL(t *testing.T) {
	cases := []struct {
		base, path, want string
	}{
		{"https://openrouter.ai/api/v1", "/v1/messages", "https://openrouter.ai/api/v1/messages"},
		{"https://openrouter.ai/api/v1/", "/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"https://openrouter.ai/api/v1", "/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"https://api.anthropic.com", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"https://api.openai.com/v1", "/v1", "https://api.openai.com/v1/"},
		{"http://127.0.0.1:8080", "/v1/messages", "http://127.0.0.1:8080/v1/messages"},
	}
	for _, tc := range cases {
		got := joinUpstreamURL(tc.base, tc.path)
		if got != tc.want {
			t.Fatalf("join(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}
