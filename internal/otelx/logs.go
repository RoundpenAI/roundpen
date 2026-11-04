package otelx

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

func newLogProvider(ctx context.Context, res *resource.Resource, proto string) (*sdklog.LoggerProvider, error) {
	ep := otlpEndpoint()
	var (
		exp sdklog.Exporter
		err error
	)
	if proto == "http" {
		exp, err = otlploghttp.New(ctx, otlploghttp.WithEndpointURL(ep))
	} else {
		exp, err = otlploggrpc.New(ctx, otlploggrpc.WithEndpointURL(ep))
	}
	if err != nil {
		return nil, err
	}
	return sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
	), nil
}

// TeeLogger keeps the existing stdout handler and, when OTEL logs are on,
// also ships Warn+ records to SigNoz Logs (correlated with the active span).
func TeeLogger(base *slog.Logger) *slog.Logger {
	mu.Lock()
	provider := lp
	mu.Unlock()
	if base == nil || provider == nil {
		return base
	}
	otelHandler := otelslog.NewHandler(serviceName,
		otelslog.WithLoggerProvider(provider),
	)
	return slog.New(&fanoutHandler{
		primary: base.Handler(),
		otel:    &minLevelHandler{min: slog.LevelWarn, next: otelHandler},
	})
}

// LogError ships err to SigNoz Logs (no-op when OTEL is off). Prefer this for
// failure paths that do not already go through slog.Warn/Error.
func LogError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	emitErrorLog(ctx, trace.SpanFromContext(ctx), err)
}

// emitErrorLog sends an error to the OTEL Logs pipeline (SigNoz Logs tab).
func emitErrorLog(ctx context.Context, span trace.Span, err error) {
	if err == nil {
		return
	}
	mu.Lock()
	provider := lp
	mu.Unlock()
	if provider == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger := provider.Logger(serviceName)
	var rec otellog.Record
	rec.SetTimestamp(time.Now())
	rec.SetBody(attribute.StringValue(err.Error()))
	rec.SetSeverity(otellog.SeverityError)
	rec.SetSeverityText("ERROR")
	if span != nil && span.SpanContext().IsValid() {
		// Bridge picks correlation from ctx when present; also stash message attrs.
		ctx = trace.ContextWithSpan(ctx, span)
	}
	logger.Emit(ctx, rec)
}

// fanoutHandler writes every record to primary (stdout) and optionally to otel.
type fanoutHandler struct {
	primary slog.Handler
	otel    slog.Handler
}

func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.primary.Enabled(ctx, level) || (h.otel != nil && h.otel.Enabled(ctx, level))
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.primary.Handle(ctx, r.Clone())
	if h.otel != nil && h.otel.Enabled(ctx, r.Level) {
		if oerr := h.otel.Handle(ctx, r); err == nil {
			err = oerr
		}
	}
	return err
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := &fanoutHandler{primary: h.primary.WithAttrs(attrs)}
	if h.otel != nil {
		out.otel = h.otel.WithAttrs(attrs)
	}
	return out
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	out := &fanoutHandler{primary: h.primary.WithGroup(name)}
	if h.otel != nil {
		out.otel = h.otel.WithGroup(name)
	}
	return out
}

// minLevelHandler drops records below min.
type minLevelHandler struct {
	min  slog.Level
	next slog.Handler
}

func (h *minLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min && h.next.Enabled(ctx, level)
}

func (h *minLevelHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.min {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &minLevelHandler{min: h.min, next: h.next.WithAttrs(attrs)}
}

func (h *minLevelHandler) WithGroup(name string) slog.Handler {
	return &minLevelHandler{min: h.min, next: h.next.WithGroup(name)}
}