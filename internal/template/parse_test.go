package template_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/template"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in            string
		ns, name, tag string
		buildID       string
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

func TestParseRef_buildUUID(t *testing.T) {
	id := uuid.NewString()
	ref := template.ParseRef(id)
	if ref.BuildID != id {
		t.Fatalf("BuildID=%q want %q", ref.BuildID, id)
	}
}

func TestValidateTag(t *testing.T) {
	if !template.ValidateTag("v2") || !template.ValidateTag("1.0.0") {
		t.Fatal("expected valid tags")
	}
	if template.ValidateTag("default") || template.ValidateTag("") || template.ValidateTag("Bad Tag") {
		t.Fatal("expected invalid tags")
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
