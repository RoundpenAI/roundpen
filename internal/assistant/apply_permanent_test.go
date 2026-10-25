package assistant

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/assistticket"
)

func TestCapabilityGrantNamePrefersTarget(t *testing.T) {
	if got := capabilityGrantName("browser", "shell"); got != "browser" {
		t.Fatalf("got %q, want browser", got)
	}
	if got := capabilityGrantName("", "Browser"); got != "browser" {
		t.Fatalf("legacy cap alias: got %q", got)
	}
	if got := capabilityGrantName("  ", ""); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestApplyPermanent_CapabilityTargetEnablesBrowser(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := "cap-" + uuid.NewString()[:8]
	insertUser(t, db, user)

	store := &Store{DB: db.SQL}
	a, err := store.Create(ctx, user, CreateInput{
		Name:         "NoBrowser",
		IdentityMode: IdentityProxyUser,
		Preset:       "code", // shell only
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.Capabilities.Browser {
		t.Fatal("fixture should start without browser")
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM assistants WHERE id=$1`, a.ID)
	})

	tickets := &assistticket.Store{DB: db.SQL}
	tk, err := tickets.Create(ctx, user, assistticket.CreateInput{
		AssistantID: a.ID,
		Kind:        assistticket.KindPolicyApply,
		Title:       "申请打开浏览器",
		Payload: map[string]any{
			"dimension": "capability",
			"target":    "browser",
		},
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	h := &Handler{Store: store, Tickets: tickets}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
	if err := h.applyPermanent(req, tk); err != nil {
		t.Fatalf("applyPermanent: %v", err)
	}

	got, err := store.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Capabilities.Browser {
		t.Fatalf("browser still off after permanent grant; caps=%+v", got.Capabilities)
	}
	if !got.Capabilities.Shell {
		t.Fatalf("shell should remain on; caps=%+v", got.Capabilities)
	}
}

func TestApplyPermanent_CapabilityLegacyField(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := "capleg-" + uuid.NewString()[:8]
	insertUser(t, db, user)

	store := &Store{DB: db.SQL}
	a, err := store.Create(ctx, user, CreateInput{
		Name:         "Legacy",
		IdentityMode: IdentityProxyUser,
		Preset:       "writing",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM assistants WHERE id=$1`, a.ID)
	})

	tickets := &assistticket.Store{DB: db.SQL}
	tk, err := tickets.Create(ctx, user, assistticket.CreateInput{
		AssistantID: a.ID,
		Kind:        assistticket.KindPolicyApply,
		Title:       "申请终端",
		Payload: map[string]any{
			"dimension":  "capability",
			"capability": "shell",
		},
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	h := &Handler{Store: store, Tickets: tickets}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
	if err := h.applyPermanent(req, tk); err != nil {
		t.Fatalf("applyPermanent: %v", err)
	}
	got, err := store.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Capabilities.Shell {
		t.Fatalf("shell still off; caps=%+v", got.Capabilities)
	}
}

func TestApplyPermanent_CapabilityMissingTarget(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user := "capmiss-" + uuid.NewString()[:8]
	insertUser(t, db, user)

	store := &Store{DB: db.SQL}
	a, err := store.Create(ctx, user, CreateInput{
		Name:         "Miss",
		IdentityMode: IdentityProxyUser,
		Preset:       "code",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM assistants WHERE id=$1`, a.ID)
	})

	tickets := &assistticket.Store{DB: db.SQL}
	tk, err := tickets.Create(ctx, user, assistticket.CreateInput{
		AssistantID: a.ID,
		Kind:        assistticket.KindPolicyApply,
		Title:       "空目标",
		Payload:     map[string]any{"dimension": "capability"},
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	h := &Handler{Store: store, Tickets: tickets}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
	if err := h.applyPermanent(req, tk); err == nil {
		t.Fatal("expected error for missing capability target")
	}
}
