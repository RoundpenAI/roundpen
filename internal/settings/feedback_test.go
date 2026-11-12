package settings

import "testing"

func TestValidateFeedbackFormURL(t *testing.T) {
	if err := validateFeedbackFormURL(""); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := validateFeedbackFormURL("https://example.feishu.cn/share/base/form/abc"); err != nil {
		t.Fatalf("https: %v", err)
	}
	if err := validateFeedbackFormURL("http://localhost/form"); err != nil {
		t.Fatalf("http: %v", err)
	}
	if err := validateFeedbackFormURL("ftp://bad"); err == nil {
		t.Fatal("expected scheme error")
	}
	if err := validateFeedbackFormURL("not a url"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestResolveSuggestedPrompts(t *testing.T) {
	p := SuggestedPrompts{
		Global: []string{" g1 ", "g1", "g2"},
		ByKind: map[string][]string{
			"User": {"u1"},
		},
	}
	p = normalizeSuggestedPrompts(p)
	got := ResolveSuggestedPrompts(p, "user")
	if len(got) != 1 || got[0] != "u1" {
		t.Fatalf("kind=%#v", got)
	}
	got = ResolveSuggestedPrompts(p, "coordinator")
	if len(got) != 2 || got[0] != "g1" || got[1] != "g2" {
		t.Fatalf("global=%#v", got)
	}
}
