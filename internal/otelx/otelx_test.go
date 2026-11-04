package otelx

import (
	"testing"
)

func TestEnabledDefaultOn(t *testing.T) {
	t.Setenv("ROUNDPEN_OTEL_ENABLED", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	if !Enabled() {
		t.Fatal("expected enabled by default")
	}
	if got := otlpEndpoint(); got != DefaultOTLPEndpoint {
		t.Fatalf("default endpoint=%q want %q", got, DefaultOTLPEndpoint)
	}
}

func TestEnabledExplicitOff(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("ROUNDPEN_OTEL_ENABLED", "false")
	if Enabled() {
		t.Fatal("explicit false should win")
	}
}

func TestEndpointFromEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otlp.ttz.ac.cn/")
	if got := otlpEndpoint(); got != "https://otlp.ttz.ac.cn" {
		t.Fatalf("got %q", got)
	}
}

func TestNoisePaths(t *testing.T) {
	if !isNoisePath("/health") || !isNoisePath("/assets/app.js") {
		t.Fatal("expected noise paths filtered")
	}
	if isNoisePath("/v1/assistants") {
		t.Fatal("API path should not be noise")
	}
}

func TestOTLPProtocol(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "")
	if otlpProtocol() != "grpc" {
		t.Fatalf("default want grpc, got %q", otlpProtocol())
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otlp-grpc.ttz.ac.cn")
	if otlpProtocol() != "grpc" {
		t.Fatalf("otlp-grpc host want grpc, got %q", otlpProtocol())
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otlp.ttz.ac.cn")
	if otlpProtocol() != "http" {
		t.Fatalf("otlp.ttz host want http, got %q", otlpProtocol())
	}

	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	if otlpProtocol() != "http" {
		t.Fatalf("explicit http got %q", otlpProtocol())
	}
}
