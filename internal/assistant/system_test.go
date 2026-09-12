package assistant

import (
	"errors"
	"testing"
)

func TestValidateDisable_SystemRejected(t *testing.T) {
	disabled := StatusDisabled
	err := validateDisable(&Assistant{Kind: KindSystem}, &disabled)
	if !errors.Is(err, ErrSystemUndeletable) {
		t.Fatalf("got %v want ErrSystemUndeletable", err)
	}
}

func TestValidateDisable_UserAllowed(t *testing.T) {
	disabled := StatusDisabled
	if err := validateDisable(&Assistant{Kind: KindUser}, &disabled); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDisable_NilStatusOK(t *testing.T) {
	if err := validateDisable(&Assistant{Kind: KindSystem}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultSystemCreateInput(t *testing.T) {
	in := DefaultSystemCreateInput()
	if in.Name != DefaultSystemName || in.Kind != KindSystem {
		t.Fatalf("%+v", in)
	}
	if err := ValidateCreate(in); err != nil {
		t.Fatal(err)
	}
}
