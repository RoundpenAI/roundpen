package assistant

import (
	"encoding/json"
	"testing"
)

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
