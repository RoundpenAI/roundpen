// Package otelx boots OpenTelemetry for roundpend (OTLP → SigNoz) and exposes
// helpers used by HTTP, LLM gateway, agent turns, and cold-path spans.
//
// By default export goes to DefaultOTLPEndpoint (OTLP/gRPC). Set
// ROUNDPEN_OTEL_ENABLED=false to disable; override the collector with
// OTEL_EXPORTER_OTLP_ENDPOINT / PROTOCOL.
package otelx

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName   = "roundpend"
	tracerName    = "github.com/RoundpenAI/roundpen"
	meterName     = "github.com/RoundpenAI/roundpen"
	attrInstallID = attribute.Key("roundpen.install_id")
	attrChannel   = attribute.Key("roundpen.channel")

	// DefaultOTLPEndpoint is the Roundpen SigNoz collector (OTLP/gRPC over TLS).
	DefaultOTLPEndpoint = "https://otlp-grpc.ttz.ac.cn"
)

// Options configures Resource identity for multi-NAS SigNoz filtering.
type Options struct {
	InstallID   string
	Version     string
	Environment string // deployment.environment; empty reads ROUNDPEN_OTEL_ENV
	Channel     string // roundpen.channel: fnos|compose|dev
	Logger      *slog.Logger
}

var (
	mu       sync.Mutex
	tp       *sdktrace.TracerProvider
	mp       *sdkmetric.MeterProvider
	lp       *sdklog.LoggerProvider
	enabled  bool
	initOnce sync.Once

	relayDuration   metric.Float64Histogram
	relayErrors     metric.Int64Counter
	turnDuration    metric.Float64Histogram
	turnTTFT        metric.Float64Histogram
	promptsInFlight metric.Int64UpDownCounter
	sandboxCreate   metric.Float64Histogram
	cdpReady        metric.Float64Histogram
)

// Enabled reports whether OTLP export should run. Default is on (built-in
// collector); set ROUNDPEN_OTEL_ENABLED=false to opt out.
func Enabled() bool {
	if v := strings.TrimSpace(os.Getenv("ROUNDPEN_OTEL_ENABLED")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return true
}

func normalizeEndpoint(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func otlpEndpointFromEnv() string {
	if v := normalizeEndpoint(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")); v != "" {
		return v
	}
	if v := normalizeEndpoint(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")); v != "" {
		return v
	}
	return normalizeEndpoint(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
}

func otlpEndpoint() string {
	if ep := otlpEndpointFromEnv(); ep != "" {
		return ep
	}
	return DefaultOTLPEndpoint
}

func otlpProtocol() string {
	p := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")))
	if p == "" {
		p = strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")))
	}
	switch p {
	case "grpc", "grpc/protobuf":
		return "grpc"
	case "http/protobuf", "http/json", "http":
		return "http"
	}
	ep := strings.ToLower(otlpEndpoint())
	// Dedicated HTTP collector host (legacy / alternate path).
	if strings.Contains(ep, "otlp.ttz.ac.cn") && !strings.Contains(ep, "otlp-grpc") {
		return "http"
	}
	// Default collector and host:port forms use gRPC.
	return "grpc"
}

// Init installs global TracerProvider / MeterProvider. Safe to call when
// disabled (no-op). Must run after install_id is known.
func Init(ctx context.Context, opts Options) error {
	var initErr error
	initOnce.Do(func() {
		if !Enabled() {
			return
		}
		log := opts.Logger
		if log == nil {
			log = slog.Default()
		}
		res, err := newResource(opts)
		if err != nil {
			initErr = err
			return
		}
		proto := otlpProtocol()
		tracerProvider, err := newTraceProvider(ctx, res, proto)
		if err != nil {
			initErr = fmt.Errorf("otel traces: %w", err)
			return
		}
		meterProvider, err := newMeterProvider(ctx, res, proto)
		if err != nil {
			_ = tracerProvider.Shutdown(context.Background())
			initErr = fmt.Errorf("otel metrics: %w", err)
			return
		}
		logProvider, err := newLogProvider(ctx, res, proto)
		if err != nil {
			_ = tracerProvider.Shutdown(context.Background())
			_ = meterProvider.Shutdown(context.Background())
			initErr = fmt.Errorf("otel logs: %w", err)
			return
		}
		if err := initInstruments(meterProvider.Meter(meterName)); err != nil {
			_ = tracerProvider.Shutdown(context.Background())
			_ = meterProvider.Shutdown(context.Background())
			_ = logProvider.Shutdown(context.Background())
			initErr = err
			return
		}
		otel.SetTracerProvider(tracerProvider)
		otel.SetMeterProvider(meterProvider)
		otel.SetLoggerProvider(logProvider)
		mu.Lock()
		tp = tracerProvider
		mp = meterProvider
		lp = logProvider
		enabled = true
		mu.Unlock()
		log.Info("otel enabled",
			slog.String("protocol", proto),
			slog.String("endpoint", otlpEndpoint()),
			slog.String("install_id", opts.InstallID),
			slog.String("service.version", opts.Version),
		)
	})
	return initErr
}

func newResource(opts Options) (*resource.Resource, error) {
	env := strings.TrimSpace(opts.Environment)
	if env == "" {
		env = strings.TrimSpace(os.Getenv("ROUNDPEN_OTEL_ENV"))
	}
	attrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(strings.TrimSpace(opts.Version)),
		semconv.ServiceInstanceID(strings.TrimSpace(opts.InstallID)),
		attrInstallID.String(strings.TrimSpace(opts.InstallID)),
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		attrs = append(attrs, semconv.HostName(host))
	}
	if env != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentName(env))
	}
	if ch := strings.TrimSpace(opts.Channel); ch != "" {
		attrs = append(attrs, attrChannel.String(ch))
	}
	// Merge env OTEL_RESOURCE_ATTRIBUTES first, then overwrite identity keys so
	// operators can add site labels without clobbering install_id.
	return resource.New(context.Background(),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
	)
}

func newTraceProvider(ctx context.Context, res *resource.Resource, proto string) (*sdktrace.TracerProvider, error) {
	ep := otlpEndpoint()
	var (
		exp sdktrace.SpanExporter
		err error
	)
	if proto == "http" {
		exp, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(ep))
	} else {
		exp, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(ep))
	}
	if err != nil {
		return nil, err
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0))),
	), nil
}

func newMeterProvider(ctx context.Context, res *resource.Resource, proto string) (*sdkmetric.MeterProvider, error) {
	ep := otlpEndpoint()
	var (
		exp sdkmetric.Exporter
		err error
	)
	if proto == "http" {
		exp, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(ep))
	} else {
		exp, err = otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(ep))
	}
	if err != nil {
		return nil, err
	}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithResource(res),
	), nil
}

func initInstruments(m metric.Meter) error {
	var err error
	relayDuration, err = m.Float64Histogram("llmgw.relay.duration",
		metric.WithDescription("LLM gateway relay duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}
	relayErrors, err = m.Int64Counter("llmgw.relay.errors",
		metric.WithDescription("LLM gateway relay errors"),
	)
	if err != nil {
		return err
	}
	turnDuration, err = m.Float64Histogram("agent.turn.duration",
		metric.WithDescription("Agent turn wall time"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}
	turnTTFT, err = m.Float64Histogram("agent.turn.ttft",
		metric.WithDescription("Time to first assistant/thought token"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}
	promptsInFlight, err = m.Int64UpDownCounter("agent.prompts.in_flight",
		metric.WithDescription("ACP prompts currently holding the concurrency gate"),
	)
	if err != nil {
		return err
	}
	sandboxCreate, err = m.Float64Histogram("sandbox.create.duration",
		metric.WithDescription("Sandbox create duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}
	cdpReady, err = m.Float64Histogram("browser.cdp.ready.duration",
		metric.WithDescription("Browser CDP attach/ready duration"),
		metric.WithUnit("s"),
	)
	return err
}

// Shutdown flushes exporters. Safe when Init was a no-op.
func Shutdown(ctx context.Context) error {
	mu.Lock()
	curTP, curMP, curLP := tp, mp, lp
	tp, mp, lp = nil, nil, nil
	enabled = false
	mu.Unlock()
	var first error
	if curTP != nil {
		if err := curTP.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	if curMP != nil {
		if err := curMP.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	if curLP != nil {
		if err := curLP.Shutdown(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Tracer returns the package tracer (no-op provider when disabled).
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// Start is a shorthand for Tracer().Start.
func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, opts...)
}

// EndSpan sets error status when err != nil, ships the error to OTEL Logs, and
// ends the span. ctx correlates the log with the active trace when possible.
func EndSpan(ctx context.Context, span trace.Span, err error) {
	if span == nil {
		return
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		emitErrorLog(ctx, span, err)
	}
	span.End()
}

// SetUserID attaches user.id to the current span when non-empty.
func SetUserID(ctx context.Context, userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(semconv.UserID(userID))
}

// DetectChannel guesses the install channel from env when opts.Channel is empty.
func DetectChannel() string {
	if v := strings.TrimSpace(os.Getenv("ROUNDPEN_OTEL_CHANNEL")); v != "" {
		return v
	}
	if strings.TrimSpace(os.Getenv("ROUNDPEN_FNOS_GATEWAY_SOCKET")) != "" {
		return "fnos"
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "compose"
	}
	return "dev"
}

func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	default:
		return "other"
	}
}
