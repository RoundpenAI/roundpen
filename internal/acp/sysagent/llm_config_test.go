package sysagent

import (
	"context"
	"errors"
	"testing"
)

func TestChatStreamRequiresBaseURL(t *testing.T) {
	_, _, err := LLMConfig{APIKey: "k", Model: "m"}.chat(context.Background(), []chatMessage{
		{Role: "user", Content: "hi"},
	}, nil)
	if !errors.Is(err, ErrLLMNotConfigured) {
		t.Fatalf("got %v, want ErrLLMNotConfigured", err)
	}
}
