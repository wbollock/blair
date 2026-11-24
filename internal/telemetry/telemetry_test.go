package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupWithoutExporters(t *testing.T) {
	var buf bytes.Buffer
	tel, err := Setup(context.Background(), Options{Version: "test", LogOutput: &buf, Prometheus: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tel.Shutdown(context.Background())
	if tel.ExportersActive {
		t.Fatal("exporters active without OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if tel.MetricsHandler == nil {
		t.Fatal("prometheus handler missing")
	}
	slog.Info("hello", "k", "v")
	if !strings.Contains(buf.String(), `"msg":"hello"`) {
		t.Fatalf("json log missing: %s", buf.String())
	}
}

func TestSetupWithOTLPEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	tel, err := Setup(context.Background(), Options{Version: "test", LogOutput: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if !tel.ExportersActive {
		t.Fatal("exporters should be active")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_ = tel.Shutdown(ctx)
}
