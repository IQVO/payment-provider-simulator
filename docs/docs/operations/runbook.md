---
id: runbook
title: Runbook
sidebar_label: Runbook
sidebar_position: 2
---

# Payment Provider Simulator runbook

Payment Provider Simulator is a Go stand-in for the external payment provider:
tokenize, authorize, capture, void, refund, re-authorize and reduce, with
deterministic outcomes driven by scenario tokens. One deployable: `cmd/api`
(REST only). Runtime: real Postgres only — there is no in-memory mode, so a
pod that cannot reach Postgres refuses to boot and never pretends to be
healthy. The simulator publishes and consumes no events ([ADR 0002](/docs/adr/0002-payment-provider-simulator-assumptions) A16), so
there is no Kafka, no outbox relay and no consumer.

## Deploy

The chart is `charts/payment-provider-simulator` (packaged by the `release`
job as `oci://ghcr.io/iqvo/payment-provider-simulator:<version>`; the image is
`ghcr.io/iqvo/payment-provider-simulator:v<version>`).

In the fleet's k3s cluster the release is managed by `warehouse-infra`
(`terraform/apps.yaml` entry `payment-provider-simulator`, values in
`helm-values/payment-provider-simulator.yaml`, image tag pinned in
`terraform/targets/k3s.images.yaml`): namespace `warehouse-systems`, Secret
`payment-provider-simulator-db` (keys `DATABASE_URL`, `MIGRATIONS_DATABASE_URL`;
`DATABASE_URL` goes through PgBouncer), and the gateway route
`/api/payment-provider-simulator` (Kong, `stripPath`). customer-payments reaches it
in-cluster at `http://payment-provider-simulator.warehouse-systems.svc.cluster.local:80`.
Change the release there, not with a hand-run `helm upgrade`; the commands below
are for a standalone install.

Mandatory values — the chart refuses to render without them:

| Value | Env var | Why |
| --- | --- | --- |
| `database.url` or `database.existingSecret` | `DATABASE_URL` | Postgres DSN (from a Secret, never the ConfigMap) |
| `database.migrationsExistingSecretKey` / `database.migrationsUrl` | `MIGRATIONS_DATABASE_URL` | Direct (non-pooled) DSN for boot migrations; falls back to `DATABASE_URL` |

Useful optionals: `config.providerTimeoutDelay` (`PROVIDER_TIMEOUT_DELAY`, the
stall of a timeout-scenario authorization before its 504; default 1s, capped at
2s), `config.shutdownDrainDelay` (`SHUTDOWN_DRAIN_DELAY`, default 5s, whole
seconds or minutes). `terminationGracePeriodSeconds` must exceed drain + 20s
(HTTP drain 10s + 10s buffer); the chart refuses anything shorter.

Deploy / upgrade:

```sh
helm upgrade --install payment-provider-simulator charts/payment-provider-simulator \
  --namespace payment-provider-simulator --create-namespace \
  --set database.existingSecret=pps-db
kubectl -n payment-provider-simulator rollout status deployment/payment-provider-simulator
```

## Rollback

```sh
helm history payment-provider-simulator -n payment-provider-simulator
helm rollback payment-provider-simulator <revision> -n payment-provider-simulator
kubectl -n payment-provider-simulator rollout status deployment/payment-provider-simulator
```

The boot migrations are forward-only; before rolling back across a release
that added a migration, confirm the older binary tolerates the newer schema
(columns are additive fleet-wide).

## Health

- `GET /healthz` — liveness only (startup + liveness probes).
- `GET /readyz` — 200 only while Postgres answers a ping (verdict cached 3s,
  bounded 2s) and the process is not draining. Flips to 503 first during
  shutdown, before the listener drains.

```sh
# In the fleet cluster, through Kong:
curl -i http://localhost:8000/api/payment-provider-simulator/healthz
curl -i http://localhost:8000/api/payment-provider-simulator/readyz
# Or straight to the Service:
kubectl -n warehouse-systems port-forward svc/payment-provider-simulator 8080:80
curl -i http://localhost:8080/readyz
```

The shutdown is sequential (cmd/api/main.go): on SIGTERM /readyz flips to 503,
`SHUTDOWN_DRAIN_DELAY` passes so the load balancer deregisters the pod, then
the HTTP listener drains bounded at 10s. A pod that exits non-zero on its own
failed the boot contract (missing `DATABASE_URL`, unreachable Postgres after
the bounded retry); check
`kubectl -n warehouse-systems logs deploy/payment-provider-simulator --previous`.

## Postgres

The service connects with least privilege (its own schema only —
authorizations, tokens, idempotency). Boot migrations run against
`MIGRATIONS_DATABASE_URL` (direct, session-mode connection); when the pooled
DSN goes through a transaction-mode pooler, keep the direct key populated or
migrations hang. The migrations/first ping retry with bounded backoff
(`internal/bootretry`), so the startup probe allows ~60s.

## Dashboards, alerts and telemetry

Logs are stdout JSON (level from `LOG_LEVEL`, `trace_id`/`span_id` stamped by the
slog bridge, [ADR 0003](/docs/adr/0003-fleet-observability-standard)); traces and
the `http.server.request.duration` histogram go to the OTLP collector named by
`OTEL_EXPORTER_OTLP_ENDPOINT`. Export never blocks: without a collector the
service still serves.

`warehouse-infra` owns the dashboard and the alerts (Grafana, backed by
Prometheus and Loki):

| What | Where |
| --- | --- |
| Dashboard `Warehouse — Payment Provider Simulator` | `http://localhost:3000/d/warehouse-payment-provider-simulator` (`terraform/dashboards/contexts/payment-provider-simulator.json`): Kong request rate, 5xx and p95 latency, service HTTP RED, Go runtime, logs |
| Rule group `warehouse-payment-provider-simulator` | `WarehouseKong5xxRatioHigh` (more than 5 % of requests on the Kong route answer 5xx for 5 m), `WarehousePodRestarting` (2 or more restarts in 15 m) |
| Rule group `warehouse-payment-provider-simulator-slo` | `WarehouseApiAvailabilityBurnFast` / `...BurnSlow` and `WarehouseApiLatencyBurnFast` / `...BurnSlow` (multi-window burn rates on the Kong route) |

First moves on an alert: check `/readyz` (a `503` naming Postgres means the
database is unreachable), then the pod logs above. A `504 provider-timeout` is
the `timeout` scenario working as designed, not an outage; only a caller using
`tok_timeout` tokens sees it.
