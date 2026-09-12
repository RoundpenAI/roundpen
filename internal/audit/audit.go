// Package audit records control-plane actions (who / action / resource).
// This is a slog-backed trail, not a policy fence.
package audit

import (
	"context"
	"log/slog"

	"github.com/RoundpenAI/roundpen/internal/authz"
)

// Record writes an audit line. Extra attrs should be non-secret (ids, names, codes).
func Record(ctx context.Context, action string, attrs ...any) {
	actor, ok := authz.From(ctx)
	fields := []any{"action", action}
	if ok {
		fields = append(fields, "actor", actor.Username, "admin", actor.Admin)
	}
	fields = append(fields, attrs...)
	slog.Info("audit", fields...)
}
