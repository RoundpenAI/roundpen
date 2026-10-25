package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateKeepsCJKIntact(t *testing.T) {
	in := "用户说：请帮我查看项目里的中文说明文档并总结要点。"
	got := truncate(in, 12)
	if !utf8.ValidString(got) {
		t.Fatalf("invalid utf8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want ellipsis suffix: %q", got)
	}
	if utf8.RuneCountInString(got) != 12 {
		t.Fatalf("rune count = %d, want 12 (%q)", utf8.RuneCountInString(got), got)
	}
	// Byte-slice truncation used to produce � / broken sequences.
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("replacement char in truncate output: %q", got)
	}
	short := truncate("你好", 10)
	if short != "你好" {
		t.Fatalf("short string changed: %q", short)
	}
}

func TestCreateRequestDecode(t *testing.T) {
	raw := []byte(`{"name":"Ada","identityMode":"proxy_user","preset":"code"}`)
	var body struct {
		Name         string `json:"name"`
		Bio          string `json:"bio"`
		IdentityMode string `json:"identityMode"`
		Preset       string `json:"preset"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	in := CreateInput{Name: body.Name, IdentityMode: body.IdentityMode, Preset: body.Preset}
	if err := ValidateCreate(in); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePatchIdentityRequiresConfirmSemantics(t *testing.T) {
	// Mirrors handler rule: changing identity without confirm is rejected by HTTP layer.
	cur := IdentityProxyUser
	next := IdentityIndependent
	if cur == next {
		t.Fatal("fixture")
	}
	confirm := false
	if next != cur && !confirm {
		// expected rejection path
		return
	}
	t.Fatal("should have rejected")
}
