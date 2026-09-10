// Package authz is the control-plane actor (identity + admin bit).
// HTTP middleware attaches it; sandbox / memory / preview read it.
// It must not import api packages.
package authz

import "context"

type contextKey struct{}

// Actor is the authenticated caller.
type Actor struct {
	Username string
	Admin    bool
}

// WithActor returns ctx carrying a.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, contextKey{}, a)
}

// From returns the actor when one was attached.
func From(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(contextKey{}).(Actor)
	if !ok || a.Username == "" {
		return Actor{}, false
	}
	return a, true
}

// CanAccess reports whether a may see a resource owned by owner.
// Admin may access any resource. Empty owner is treated as unowned (admin only).
func (a Actor) CanAccess(owner string) bool {
	if a.Admin {
		return true
	}
	return a.Username != "" && owner != "" && a.Username == owner
}
