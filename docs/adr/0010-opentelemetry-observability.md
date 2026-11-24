# ADR-0010: OpenTelemetry for traces, metrics, logs and profiles

Status: Accepted · Date: 2026-11-24

## Context
The owner wants real observability from day one (profiles, traces, logs and
metrics), without being locked into one vendor. Honeycomb is already in use,
and Prometheus-only metrics would cover just one signal.

## Decision
- Instrument with the OpenTelemetry Go SDK and export over OTLP, configured
  only through the standard `OTEL_*` env vars. No endpoint means exporters off.
- Traces: `otelhttp` + service/DB spans; W3C propagation from the browser.
- Metrics: OTel metrics (RED per route, DB, scheduler, SSE, runtime). A
  Prometheus exporter is optional.
- Logs: `slog` through the `otelslog` bridge, so logs carry trace context.
- Profiles: OTel profiles signal where supported. Until it is stable, use a
  Pyroscope-compatible push with span-profile linking. `pprof` runs on a
  localhost-only debug listener.
- No content or PII in telemetry, enforced by a test.

## Consequences
- Adds SDK dependencies and a little startup code in `internal/telemetry`.
- Span, metric and attribute names are a documented contract
  (`docs/OBSERVABILITY.md`) so dashboards don't break silently.
- The profiles signal is newer than the others. The fallback path is isolated
  so it can be swapped out later.
