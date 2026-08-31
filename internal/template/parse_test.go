package template_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/template"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in             string
		ns, name, tag  string
		buildID        string
	}{
		{"host", "default", "host", "default", ""},
		{"My-Template", "default", "my-template", "default", ""},
		{"acme/tool", "acme", "tool", "default", ""},
		{"acme/tool:staging", "acme", "tool", "staging", ""},
		{"python:1.0", "default", "python", "1.0", ""},
	}
	for _, tc := range tests {
		ref := template.ParseRef(tc.in)
		if ref.Namespace != tc.ns || ref.Name != tc.name || ref.Tag != tc.tag || ref.BuildID != tc.buildID {
			t.Fatalf("ParseRef(%q) = %+v, want ns=%q name=%q tag=%q build=%q",
				tc.in, ref, tc.ns, tc.name, tc.tag, tc.buildID)
		}
	}
}

func TestValidateName(t *testing.T) {
	if !template.ValidateName("code-agent") {
		t.Fatal("expected valid name")
	}
	if template.ValidateName("bad name") {
		t.Fatal("expected invalid name with space")
	}
}
