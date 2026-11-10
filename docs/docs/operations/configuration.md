---
id: configuration
title: Configuration
sidebar_label: Configuration
sidebar_position: 1
---

# Configuration

`payment-provider-simulator` ships **one binary**, `cmd/api`, configured only
through environment variables (`cmd/api/main.go`, `cmd/api/config.go`,
`cmd/api/telemetry.go`). There is no config file, no flag and no secret-manager
integration. REST is unauthenticated fleet-wide, so there is no token or key to
configure.

The service runs on real Postgres only: there is no in-memory mode
(`TestRuntimeBinaryHasNoInMemoryAdapters`), and a missing `DATABASE_URL` is a
boot error raised before anything is opened or bound. It reads no Kafka
variable: it publishes and consumes no events (ADR 0002 A16).

## `cmd/api`

| Variable | Default | Required? | Meaning | Chart value |
| --- | --- | --- | --- | --- |
| `DATABASE_URL` | none | **yes** | Postgres DSN of the service's own database | `database.url` or `database.existingSecret` + `database.existingSecretKey` (always from a Secret) |
| `MIGRATIONS_DATABASE_URL` | `DATABASE_URL` | no | direct (session-mode) DSN for the boot migrations; set it when `DATABASE_URL` goes through a transaction-mode pooler | `database.migrationsUrl` or `database.migrationsExistingSecretKey` |
| `HTTP_ADDR` | `:8080` | no | listen address; its port must equal the chart's `service.targetPort` | `config.httpAddr` |
| `PROVIDER_TIMEOUT_DELAY` | `1s` | no | Go duration a `timeout`-scenario authorization stalls before its `504`; capped at `2s`, `0` = no stall, unset / malformed / negative = the default | `config.providerTimeoutDelay` |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | no | wait after `/readyz` flips to `503` on SIGTERM, before the HTTP listener drains; `0` disables it, an invalid value falls back to the default | `config.shutdownDrainDelay` (whole seconds or minutes) |
| `LOG_LEVEL` | `info` | no | `debug`, `info`, `warn` (or `warning`), `error`; anything else is `info` | `config.logLevel` |
| `OTEL_SERVICE_NAME` | `payment-provider-simulator` | no | `service.name` of spans and metrics, and the request-duration middleware's service name | `otel.serviceName` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | no | OTLP/gRPC collector, `host:port` or a URL; export never blocks, so without a collector telemetry is dropped and the service still serves | `otel.endpoint` (rendered when `otel.enabled`) |
| `SERVICE_VERSION` | `dev` | no | `service.version` | the image tag (or the chart's `appVersion`) |
| `ENVIRONMENT` | `local` | no | `deployment.environment.name` | `environment` |

Fixed in code, not configurable: the HTTP drain bound (10 s), the hold validity
(7 days; 5 minutes for `expire-early`) and the 2 s cap of the timeout stall.

## In the Helm chart

`charts/payment-provider-simulator` renders `HTTP_ADDR`, `LOG_LEVEL` and the
two optional delays both as explicit `env` entries and in a ConfigMap mounted
with `envFrom`; both DSNs come only from a Secret (`templates/secret.yaml` or
`database.existingSecret`). The chart refuses to render when:

- neither `database.url` nor `database.existingSecret` is set;
- `extraEnv` sets a `*_MODE` variable, a `*_CONSUMER_GROUP` (there is no
  consumer) or a variable the chart renders itself;
- `config.shutdownDrainDelay` is not whole seconds, whole minutes or `0`;
- `terminationGracePeriodSeconds` (default 30) does not exceed
  `SHUTDOWN_DRAIN_DELAY` + 10 s HTTP drain + 10 s buffer;
- `config.httpAddr`'s port differs from `service.targetPort`.

`charts/payment-provider-simulator/tests/render_test.sh` checks every variable
`cmd/api` reads is rendered (CI job `chart-tests`). Deployment steps are in the
[Runbook](/docs/operations/runbook).
