package sysagent

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/policy"
)

func TestEffectivePolicyPrefersLookup(t *testing.T) {
	stale := &policy.Profile{Capabilities: policy.Caps{Shell: true}}
	fresh := &policy.Profile{Capabilities: policy.Caps{Shell: true, Browser: true}}
	var calls atomic.Int32
	a := New(Deps{
		Policy: stale,
		PolicyLookup: func(context.Context) *policy.Profile {
			calls.Add(1)
			return fresh
		},
	})
	got := a.effectivePolicy(context.Background())
	if got == nil || !got.Capabilities.Browser {
		t.Fatalf("lookup should win: %+v", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestEffectivePolicyFallsBackToSnapshot(t *testing.T) {
	snap := &policy.Profile{Capabilities: policy.Caps{Browser: true}}
	a := New(Deps{Policy: snap})
	got := a.effectivePolicy(context.Background())
	if got != snap {
		t.Fatalf("want snapshot, got %+v", got)
	}
}
