package otelx

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RecordLLMRelay records relay duration and errors.
func RecordLLMRelay(ctx context.Context, provider string, statusCode int, d time.Duration) {
	if relayDuration == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("llm.provider", provider),
		attribute.String("http.status_class", statusClass(statusCode)),
	)
	relayDuration.Record(ctx, d.Seconds(), attrs)
	if statusCode >= 400 || statusCode == 0 {
		if relayErrors != nil {
			relayErrors.Add(ctx, 1, attrs)
		}
	}
}

// RecordAgentTurn records turn duration and optional TTFT.
func RecordAgentTurn(ctx context.Context, trigger string, d, ttft time.Duration) {
	if turnDuration == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("turn.trigger", triggerOrChat(trigger)))
	turnDuration.Record(ctx, d.Seconds(), attrs)
	if ttft > 0 && turnTTFT != nil {
		turnTTFT.Record(ctx, ttft.Seconds(), attrs)
	}
}

// PromptsInFlightDelta adjusts the ACP concurrency gauge.
func PromptsInFlightDelta(ctx context.Context, delta int64) {
	if promptsInFlight == nil {
		return
	}
	promptsInFlight.Add(ctx, delta)
}

// RecordSandboxCreate records sandbox create duration.
func RecordSandboxCreate(ctx context.Context, d time.Duration, ok bool) {
	if sandboxCreate == nil {
		return
	}
	sandboxCreate.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.Bool("ok", ok),
	))
}

// RecordCDPReady records browser CDP ready duration.
func RecordCDPReady(ctx context.Context, d time.Duration, ok bool) {
	if cdpReady == nil {
		return
	}
	cdpReady.Record(ctx, d.Seconds(), metric.WithAttributes(
		attribute.Bool("ok", ok),
	))
}

func triggerOrChat(trigger string) string {
	if trigger == "" {
		return "chat"
	}
	return trigger
}
