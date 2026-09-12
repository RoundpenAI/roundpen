package authz

import (
	"context"
	"testing"
)

func TestFromRequiresUsername(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Fatal("expected no actor")
	}
	if _, ok := From(WithActor(context.Background(), Actor{})); ok {
		t.Fatal("empty username is not an actor")
	}
	got, ok := From(WithActor(context.Background(), Actor{Username: "alice"}))
	if !ok || got.Username != "alice" || got.Admin {
		t.Fatalf("%+v ok=%v", got, ok)
	}
}

func TestCanAccess(t *testing.T) {
	user := Actor{Username: "alice"}
	admin := Actor{Username: "root", Admin: true}
	if !user.CanAccess("alice") {
		t.Fatal("owner should access own resource")
	}
	if user.CanAccess("bob") {
		t.Fatal("user must not access another owner")
	}
	if user.CanAccess("") {
		t.Fatal("unowned is admin-only")
	}
	if !admin.CanAccess("bob") || !admin.CanAccess("") {
		t.Fatal("admin should access any owner")
	}
}
