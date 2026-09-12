package assistant

import "testing"

func TestApplyPreset_CodeBrowser(t *testing.T) {
	c := ApplyPreset("code_browser")
	if !c.Shell || !c.Browser {
		t.Fatalf("want shell+browser: %+v", c)
	}
	if c.Mobile || c.Desktop {
		t.Fatalf("mobile/desktop should be off: %+v", c)
	}
}

func TestValidateCreate_RequiresName(t *testing.T) {
	err := ValidateCreate(CreateInput{Name: "  ", IdentityMode: IdentityProxyUser})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateCreate_Identity(t *testing.T) {
	err := ValidateCreate(CreateInput{Name: "Ada", IdentityMode: "nope"})
	if err == nil {
		t.Fatal("expected bad identity")
	}
}
