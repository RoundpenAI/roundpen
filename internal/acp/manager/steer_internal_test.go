package manager

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeSteerWithoutCapability(t *testing.T) {
	rt := &Runtime{}
	if rt.SteerCapable() {
		t.Fatal("a runtime without a steer func must not report capability")
	}
	if err := rt.Steer("hello"); !errors.Is(err, ErrSteerUnsupported) {
		t.Fatalf("err = %v, want ErrSteerUnsupported", err)
	}
}

func TestManagerSteerUnknownSession(t *testing.T) {
	m := New(nil, nil, nil, SysDeps{})
	if err := m.Steer(context.Background(), "nope", "hi"); err == nil {
		t.Fatal("steering an unknown session must fail")
	}
}
