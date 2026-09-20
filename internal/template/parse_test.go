package template_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/template"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in     string
		ns     string
		name   string
	}{
		{"host", "default", "host"},
		{"My-Template", "default", "my-template"},
		{"acme/tool", "acme", "tool"},
		// Tags are parsed off the reference; the catalog holds one artifact
		// per name, so the tag does not reach resolution.
		{"acme/tool:staging", "acme", "tool"},
		{"python:1.0", "default", "python"},
		{"", "default", ""},
	}
	for _, tc := range tests {
		ref := template.ParseRef(tc.in)
		if ref.Namespace != tc.ns || ref.Name != tc.name {
			t.Fatalf("ParseRef(%q) = %+v, want ns=%q name=%q",
				tc.in, ref, tc.ns, tc.name)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := template.DisplayName("default", "host"); got != "host" {
		t.Fatalf("got %q", got)
	}
	if got := template.DisplayName("acme", "tool"); got != "acme/tool" {
		t.Fatalf("got %q", got)
	}
}
