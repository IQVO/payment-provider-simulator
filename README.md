# payment-provider-simulator

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Payment Provider Simulator** (Generic, storefront tier).

Go stand-in for the external payment provider (own Postgres): tokenize (hosted-fields stand-in), authorize, capture, void, refund, re-authorize and reduce (partial release) with deterministic outcomes per test token. The HTTP contract is the one customer-payments documents in its ADR 0002 assumption A12; swapping the simulator for a real PSP changes nothing in the storefront (only `PAYMENT_PROVIDER_URL`).

It owns the provider-side `ProviderToken` and `ProviderAuthorization` (hold, captures, refunds) test world. It does **not** own storefront money movement — customer-payments owns that and calls this service over HTTP — and it speaks to no other context: it publishes and consumes **no** events ([ADR 0002](docs/adr/0002-payment-provider-simulator-assumptions.md) A16), so there is no Kafka dependency at runtime.

The design lives in [warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront) `storefront/model/model.yaml`; the assumptions made while building are in `docs/adr/0002-payment-provider-simulator-assumptions.md`; the contract is `apis/openapi.yaml`.

Runtime: Go, hexagonal (domain `providerauth` / `providertoken`, application ports and use cases, chi HTTP, Postgres adapter with embedded migrations), real PostgreSQL only (`DATABASE_URL` is mandatory).

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

## Documentation

This repository has no docs site of its own (it is one of the storefront's supporting repos, so it has no ddd-crew context pack). Its documentation lives on the warehouse-docs storefront pages:

| Area | Pages |
|---|---|
| Storefront domain | [Overview](https://iqvo.github.io/warehouse-docs/storefront), [Edge and supporting components](https://iqvo.github.io/warehouse-docs/storefront/components) (the simulator's page incl. the test-token table) |
| The caller | [customer-payments context](https://iqvo.github.io/warehouse-docs/storefront/contexts/customer-payments) (the ACL port this contract implements) |
| Decisions & contracts | [Storefront ADRs](https://iqvo.github.io/warehouse-docs/storefront/adr), [API reference](https://iqvo.github.io/warehouse-docs/storefront/api-reference) |

In-tree: [docs/adr/](docs/adr/) (3 decisions), `docs/before-first-release.md` (packaging checklist), `HARNESS.md` (every quality gate).

## Architecture

Hexagonal / ports & adapters. Dependencies point inward only:

```
cmd/api/                     composition root: the provider REST API on real Postgres
internal/
  domain/                    providerauth, providertoken, shared — pure business logic
  application/
    ports/                   OUT interfaces only (repos, Clock)
    usecases/                one struct per use case
    idempotency/             the Idempotency-Key machinery
    views/                   the read models
  adapters/
    inbound/http/            chi handlers, problem+json mapping, idempotency middleware,
                             the OTel request-duration middleware
    inbound/requestlog/      the request log line
    outbound/postgres/       pgxpool repos, embedded golang-migrate
    outbound/telemetry/      OTel wiring (traces, metrics, the slog bridge)
    outbound/clock/ ids/     wall clock, id source
    outbound/memory/         test doubles only, never linked into cmd/** (fitness-tested)
    outbound/kafka/          the fleet harness' outbox writer; nothing is published
    outbound/repocontract/   the repository contract tests
  architecture/              the fitness tests below
apis/                        openapi.yaml (REST) + asyncapi.yaml (deliberately empty)
docs/adr/                    3 ADRs
features/                    godog scenarios (tokens_and_authorizations, holds, expiry)
charts/payment-provider-simulator/   the Helm chart
```

The layering is an executable test, not a convention: `internal/architecture/architecture_test.go` (`TestHexagonalArchitecture`) fails the build if the domain ever imports an outer layer or an adapter reaches sideways into a sibling. The same package pins the event-catalogue and Kafka writer rules the fleet harness shares (`catalogue_fitness_test.go`, `events_fitness_test.go`, `kafka_*_fitness_test.go`) and that no in-memory adapter reaches a binary.

## Integration

The simulator is deliberately silent: `apis/asyncapi.yaml` declares `channels: {}` and `operations: {}` — it publishes no events and consumes none ([ADR 0002](docs/adr/0002-payment-provider-simulator-assumptions.md) A16). Its only integration surface is the HTTP contract below, called by customer-payments' Anti-Corruption Layer at `PAYMENT_PROVIDER_URL`.

## Running it

```sh
DATABASE_URL="postgres://user:pass@localhost:5432/payment_provider?sslmode=disable" go run ./cmd/api
# migrations run at boot; REST on :8080; /readyz gates on a Postgres ping + drain gate
```

### Configuration

`cmd/api` (the only binary):

| Env var | Default | Meaning |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Listen address |
| `DATABASE_URL` | **required** | Postgres DSN |
| `MIGRATIONS_DATABASE_URL` | `DATABASE_URL` | Direct DSN for the boot migrations |
| `PROVIDER_TIMEOUT_DELAY` | `1s` | Stall of a timeout-scenario authorization (max `2s`) |
| `SHUTDOWN_DRAIN_DELAY` | `5s` | Wait after `/readyz` flips to 503 on shutdown |
| `LOG_LEVEL` | `info` | Minimum log level |
| `OTEL_SERVICE_NAME` | `payment-provider-simulator` | `service.name` of spans and metrics |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | telemetry default | Collector OTLP/gRPC endpoint; export never blocks |
| `SERVICE_VERSION` | `dev` | `service.version` |
| `ENVIRONMENT` | `local` | `deployment.environment.name` |

## Observability

[ADR 0003](docs/adr/0003-fleet-observability-standard.md) adopted the fleet standard: OTLP traces and metrics (`OTEL_SERVICE_NAME` `payment-provider-simulator`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `SERVICE_VERSION`, `ENVIRONMENT`, `LOG_LEVEL`); the `http.server.request.duration` histogram comes from the chi middleware; JSON logs on stdout are bridged to the trace context (`trace_id`/`span_id`) by the `slogotel` bridge. Export never blocks: without a Collector telemetry is dropped and the service still starts, serves and shuts down normally. The Helm chart's `otel.*` values render the env.

## API

Contract: [`apis/openapi.yaml`](apis/openapi.yaml). Every POST carries an `Idempotency-Key`; the same key returns the same answer. Bodies are JSON, amounts are integer minor units, only PSP tokens travel, never a card number.

| Route | Purpose |
| --- | --- |
| `POST /tokens` `{scenario}` | issue a scenario token (`tok_<scenario>_<suffix>`) |
| `POST /authorizations` `{amountMinor, currency, paymentMethodToken, reference}` | place a hold: `201 {id, expiresAt}` or `402` decline |
| `POST /authorizations/{id}/void` | release the remaining hold |
| `POST /authorizations/{id}/reauthorize` `{amountMinor}` | replace by a new hold (`201`, new id and expiry) |
| `POST /authorizations/{id}/reduce` `{amountMinor}` | lower the REMAINING hold (partial release) |
| `POST /authorizations/{id}/captures` `{amountMinor}` | capture (`201 {id}`) |
| `POST /authorizations/{id}/refunds` `{amountMinor}` | refund captured money (`201 {id}`) |
| `GET /authorizations/{id}` | read a hold (test aid) |
| `GET /healthz`, `GET /readyz` | liveness, readiness (Postgres ping + drain gate) |

A 4xx decline is final (`402` problem+json with the extension member `code`); 5xx and 504 are transient.

### Scenario vocabulary

A token names its scenario, so a test always gets the same outcome. `POST /tokens {"scenario":"decline"}` returns `tok_decline_<suffix>`; the bare literals `tok_approve`, `tok_decline`, `tok_timeout` and `tok_expire-early` work too.

| Scenario | Behaviour |
| --- | --- |
| `approve` | authorizes; the hold lasts 7 days |
| `decline` | every authorization answers `402 payment-declined`, `code: CARD_DECLINED` |
| `timeout` | the authorization answers `504 provider-timeout` after a short delay (`PROVIDER_TIMEOUT_DELAY`, default 1 s, at most 2 s); no hold is created |
| `expire-early` | authorizes; the hold lasts 5 minutes, so re-authorization before expiry is testable (a re-authorized hold keeps the scenario) |

A string that is not a token is `422`; a 13 to 19 digit string is refused as a card number.

## Helm chart

`charts/payment-provider-simulator` deploys `cmd/api` (Deployment, Service, ConfigMap, optional Secret; Ingress/HPA off by default). Values that matter: `config.httpAddr`, `config.providerTimeoutDelay`, `database.url` (or `existingSecret`; `DATABASE_URL` always comes from a Secret), `otel.enabled` and `environment`. Linted and rendered in CI by the `helm-lint` (`ct lint`) and `chart-tests` jobs.

## Local development / quality gate

`make check` is the fast gate, `make check-all` the full one, `make integration` runs the Postgres tests on testcontainers (needs Docker). `make help` lists everything; `HARNESS.md` documents every gate. lefthook wires the same targets as hooks (`lefthook install` once per clone): pre-commit runs `make fmt-check`, `make vet`, `make lint`; pre-push runs `make check`.

## Tests

- **Unit**: `make test` — domain, use cases, handlers, adapters.
- **BDD**: `make bdd` — godog scenarios in `features/` (`tokens_and_authorizations.feature`, `holds.feature`, `expiry.feature`).
- **Integration**: `make integration` — testcontainers Postgres.
- **Mutation**: `make mutation` (blocking subset) / `make mutation-full` — gremlins, config in `.gremlins.yaml`.
- **Architecture fitness**: `make arch-test` — `internal/architecture/`.

CI (`.github/workflows/ci.yml`) runs `lint`, `guide-lint`, `complexity`, `test`, `bdd`, `integration`, `mutation-fast`, `api-lint`, `vuln`, `helm-lint`, `chart-tests` and `arch-test` on every push and PR; the exhaustive `mutation` job runs on schedule; `docker-publish` pushes the image and `release` tags and publishes the chart.

## Architecture Decision Records

[docs/adr/](docs/adr/) — 3 ADRs: 0001 bounded context, 0002 assumptions (incl. the contract and the no-events decision), 0003 fleet observability standard. `docs/before-first-release.md` is the packaging checklist.

## License

[MIT](LICENSE).
