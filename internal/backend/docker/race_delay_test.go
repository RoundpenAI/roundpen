package docker

import (
	"testing"
	"time"
)

func TestEffectiveMirrorRaceDelayDefault(t *testing.T) {
	b := &Backend{}
	if got := b.effectiveMirrorRaceDelay(); got != defaultMirrorRaceDelay {
		t.Fatalf("unset delay = %v, want %v", got, defaultMirrorRaceDelay)
	}
}

func TestEffectiveMirrorRaceDelayDisabled(t *testing.T) {
	b := &Backend{}
	b.SetMirrorRaceDelay(0)
	if got := b.effectiveMirrorRaceDelay(); got != 0 {
		t.Fatalf("disabled delay = %v, want 0", got)
	}
}

func TestEffectiveMirrorRaceDelayCustom(t *testing.T) {
	b := &Backend{}
	b.SetMirrorRaceDelay(30 * time.Second)
	if got := b.effectiveMirrorRaceDelay(); got != 30*time.Second {
		t.Fatalf("custom delay = %v", got)
	}
}
