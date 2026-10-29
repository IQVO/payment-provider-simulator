# Payment Provider Simulator — Operations Runbook

Payment Provider Simulator is a Go stand-in for the external payment provider:
tokenize, authorize, capture, void, refund, re-authorize and reduce, with
deterministic outcomes driven by scenario tokens. One deployable: `cmd/api`
(REST only). Runtime: real Postgres only — there is no in-memory mode, so a
pod that cannot reach Postgres refuses to boot and never pretends to be
healthy. The simulator publishes and consumes no events (ADR 0002 A16), so
there is no Kafka, no outbox relay and no consumer.

## Deploy

The chart is `charts/payment-provider-simulator` (packaged by the `release`
job as `oci://ghcr.io/iqvo/payment-provider-simulator:<version>`; the image is
`ghcr.io/iqvo/payment-provider-simulator:v<version>`). The kind cluster
deploys it through the warehouse-infra ArgoCD app with the local image
`warehouse/payment-provider-simulator`.

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
kubectl -n payment-provider-simulator port-forward svc/payment-provider-simulator 8080:80
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

The shutdown is sequential (cmd/api/main.go): on SIGTERM /readyz flips to 503,
`SHUTDOWN_DRAIN_DELAY` passes so the load balancer deregisters the pod, then
the HTTP listener drains bounded at 10s. A pod that exits non-zero on its own
failed the boot contract (missing `DATABASE_URL`, unreachable Postgres after
the bounded retry); check
`kubectl -n payment-provider-simulator logs deploy/payment-provider-simulator --previous`.

## Postgres

The service connects with least privilege (its own schema only —
authorizations, tokens, idempotency). Boot migrations run against
`MIGRATIONS_DATABASE_URL` (direct, session-mode connection); when the pooled
DSN goes through a transaction-mode pooler, keep the direct key populated or
migrations hang. The migrations/first ping retry with bounded backoff
(`internal/bootretry`), so the startup probe allows ~60s.

## Dashboards and telemetry

Logs are stdout JSON (level from `LOG_LEVEL`) collected by the cluster log
pipeline. Dashboards and alerts for the fleet live in the warehouse-infra
repository (Grafana dashboards under its observability stack, backed by
Prometheus); look for the payment-provider-simulator service panels there.
