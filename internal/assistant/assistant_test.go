package assistant

import (
	"encoding/json"
	"testing"
)

func TestApplyPreset_BrowserOnByDefault(t *testing.T) {
	for _, preset := range []string{"writing", "code", "code_browser", "custom", ""} {
		c := ApplyPreset(preset)
		if !c.Browser {
			t.Fatalf("preset %q: want browser on, got %+v", preset, c)
		}
	}
	writing := ApplyPreset("writing")
	if writing.Shell {
		t.Fatalf("writing should leave shell off: %+v", writing)
	}
	code := ApplyPreset("code")
	if !code.Shell {
		t.Fatalf("code should keep shell on: %+v", code)
	}
}

func TestApplyPreset_CodeBrowser(t *testing.T) {
	c := ApplyPreset("code_browser")
	if !c.Shell || !c.Browser {
		t.Fatalf("want shell+browser: %+v", c)
	}
	if c.Mobile || c.Desktop {
		t.Fatalf("mobile/desktop should be off: %+v", c)
	}
}

func TestCapabilitiesUnmarshal_MissingBrowserDefaultsOn(t *testing.T) {
	var c Capabilities
	if err := json.Unmarshal([]byte(`{"shell":true}`), &c); err != nil {
		t.Fatal(err)
	}
	if !c.Browser || !c.Shell {
		t.Fatalf("missing browser key should default on: %+v", c)
	}
}

func TestCapabilitiesUnmarshal_ExplicitBrowserFalse(t *testing.T) {
	var c Capabilities
	if err := json.Unmarshal([]byte(`{"shell":true,"browser":false}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Browser {
		t.Fatalf("explicit false must stay off: %+v", c)
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
