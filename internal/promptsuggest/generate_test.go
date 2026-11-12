package promptsuggest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

func TestGeneratorGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{
					"content": `["介绍你自己", "帮我排优先级", "本周摘要"]`,
				}},
			},
		})
	}))
	defer srv.Close()

	g := &Generator{
		Endpoint: func() (string, string) { return srv.URL, "test-model" },
	}
	got, err := g.Generate(context.Background(), Context{
		Name: "系统助手",
		Bio:  "常驻入口",
		Kind: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got=%#v", got)
	}
}

func TestServicePrefersConfigOverLLM(t *testing.T) {
	called := false
	svc := &Service{
		Settings: func() settings.SuggestedPrompts {
			return settings.SuggestedPrompts{
				ByKind: map[string][]string{"system": {"配置问题"}},
			}
		},
		LLM: &Generator{
			Endpoint: func() (string, string) {
				called = true
				return "http://example.invalid", "m"
			},
		},
	}
	res := svc.Suggest(context.Background(), AssistantInput{
		ID:   "a1",
		Name: "系统助手",
		Kind: "system",
	}, "")
	if res.Source != SourceConfig || len(res.Prompts) != 1 || res.Prompts[0] != "配置问题" {
		t.Fatalf("res=%#v", res)
	}
	if called {
		t.Fatal("LLM should not be called when config is present")
	}
}
