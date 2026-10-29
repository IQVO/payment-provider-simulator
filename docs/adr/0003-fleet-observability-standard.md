# ADR 0003: Adopt the fleet observability standard (logs, metrics, traces)

## Status

Accepted (2026-10-29).

## Context

The simulator logged JSON with `log/slog` and nothing else: no trace or span id
on a log line, no request id, no request log of the fleet's shape, no metrics,
no traces. The storefront services and the WMS/WES fleet already share one
pipeline and one convention (inventory-storage ADR 0016, wes-work-planning
ADR 0013; checkout's ADR 0006 is the storefront instance of it).

## Decision

Adopt the standard, in the same shape as checkout's:

- `internal/adapters/outbound/telemetry` (Setup: OTLP traces + metrics over
  gRPC, W3C TraceContext + Baggage propagator, Go runtime metrics,
  non-blocking with no Collector), `WithTraceContext` on the JSON handler.
- The chi router runs the standard order: RequestID, otelchi (a server span
  named after the route pattern), the request-duration histogram,
  `requestlog.RequestLogger`, chi's `Recoverer`, then the existing `rejectNUL`.
- Chart: `otel.{enabled,endpoint,serviceName}` and `environment`, rendered as
  `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `SERVICE_VERSION` and
  `ENVIRONMENT` on the api Deployment; `extraEnv` may not set these names;
  the render tests assert each.

No Tier-2 business counter: the simulator is an external-system stub with no
domain outcomes. `normalizeEndpoint` builds the plaintext prefix through
`net/url` rather than an `http://` literal, keeping this copy free of URL
literals like the reference fleet copies.

## Consequences

- `/healthz` and `/readyz` are unchanged; the deterministic test tokens
  (approve, decline, timeout, expire-early) are unchanged.
- A Collector that is down degrades to dropped telemetry, never to a refused
  boot or a hung request.
