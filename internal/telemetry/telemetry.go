// Package telemetry configures OpenTelemetry traces, metrics and logs (SPEC §14.2, ADR-0010).
//
// Exporters are enabled only when an OTLP endpoint is configured through the
// standard OTEL_* environment variables; otherwise the global providers stay
// no-ops and only stdout JSON logging is active.
package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ServiceName is the OTel service.name for every signal.
const ServiceName = "blair"

// Options controls telemetry setup.
type Options struct {
	Version    string
	LogLevel   string
	LogOutput  io.Writer // defaults to os.Stdout
	Prometheus bool
}

// Telemetry holds the shutdown hook and an optional Prometheus handler.
type Telemetry struct {
	Shutdown        func(context.Context) error
	MetricsHandler  http.Handler // non-nil when Prometheus is enabled
	ExportersActive bool
}

// otlpConfigured reports whether any OTLP endpoint env var is set.
func otlpConfigured() bool {
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Setup installs global providers and the default slog logger.
func Setup(ctx context.Context, opts Options) (*Telemetry, error) {
	out := opts.LogOutput
	if out == nil {
		out = os.Stdout
	}
	level := parseLevel(opts.LogLevel)
	jsonHandler := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: level})

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(ServiceName),
		semconv.ServiceVersion(opts.Version),
	))
	if err != nil {
		return nil, err
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	var shutdowns []func(context.Context) error
	t := &Telemetry{}
	var readers []sdkmetric.Option

	if opts.Prometheus {
		reg := prometheus.NewRegistry()
		exp, err := promexporter.New(promexporter.WithRegisterer(reg))
		if err != nil {
			return nil, err
		}
		readers = append(readers, sdkmetric.WithReader(exp))
		t.MetricsHandler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	}

	handler := slog.Handler(jsonHandler)
	if otlpConfigured() {
		t.ExportersActive = true

		te, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(te), sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		shutdowns = append(shutdowns, tp.Shutdown)

		me, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, err
		}
		readers = append(readers, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me, sdkmetric.WithInterval(30*time.Second))))

		le, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, err
		}
		lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(le)), sdklog.WithResource(res))
		global.SetLoggerProvider(lp)
		shutdowns = append(shutdowns, lp.Shutdown)

		handler = slog.NewMultiHandler(jsonHandler, otelslog.NewHandler(ServiceName, otelslog.WithLoggerProvider(lp)))
	}

	if len(readers) > 0 {
		mp := sdkmetric.NewMeterProvider(append(readers, sdkmetric.WithResource(res))...)
		otel.SetMeterProvider(mp)
		shutdowns = append(shutdowns, mp.Shutdown)
		if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
			return nil, err
		}
	}

	slog.SetDefault(slog.New(handler))

	t.Shutdown = func(ctx context.Context) error {
		var errs []error
		for i := len(shutdowns) - 1; i >= 0; i-- {
			errs = append(errs, shutdowns[i](ctx))
		}
		return errors.Join(errs...)
	}
	return t, nil
}
