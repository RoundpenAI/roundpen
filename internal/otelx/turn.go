package otelx

import "context"

type turnTriggerKey struct{}

// WithTurnTrigger tags ctx so agent.turn spans can set turn.trigger.
func WithTurnTrigger(ctx context.Context, trigger string) context.Context {
	if trigger == "" {
		return ctx
	}
	return context.WithValue(ctx, turnTriggerKey{}, trigger)
}

// TurnTrigger returns the trigger from ctx, or "chat".
func TurnTrigger(ctx context.Context) string {
	if v, ok := ctx.Value(turnTriggerKey{}).(string); ok && v != "" {
		return v
	}
	return "chat"
}
