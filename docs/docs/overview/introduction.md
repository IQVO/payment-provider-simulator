---
id: introduction
title: Introduction
sidebar_label: Introduction
sidebar_position: 1
---

# Payment Provider Simulator

**Payment Provider Simulator** (`payment-provider-simulator`) is the Go stand-in
for the external payment provider of the **Storefront** area of the
warehouse-systems fleet ([ADR 0001](/docs/adr/0001-payment-provider-simulator-bounded-context)).
`customer-payments` calls it through its Anti-Corruption Layer at
`PAYMENT_PROVIDER_URL`; the HTTP contract is the one customer-payments documents
in its ADR 0002, assumption A12, so replacing the simulator with a real payment
service provider (PSP) changes only that URL. It runs on its own Postgres,
ships one binary (`cmd/api`, REST on `:8080`) and has no MCP server, no A2A
agent and no `web/` front end.

Outcomes are **deterministic per test token**: a payment-method token names its
scenario, so a test in another repository always gets the same answer. It never
sees a card number, only the tokens it issued itself.

## What it owns

| Concept | Where | Notes |
| --- | --- | --- |
| `ProviderAuthorization` aggregate (the hold) | `internal/domain/providerauth` | States `AUTHORIZED`, `CAPTURED`, `VOIDED`, `SUPERSEDED`. Captures never exceed the remaining hold, refunds never exceed the captured total, no capture at or after `expiresAt`; only an `AUTHORIZED` hold accepts a capture, void, reduce or re-authorization. Captures and refunds are kept as a JSON list of `movements` on the hold row (ADR 0002 A12). |
| Tokens and the scenario vocabulary | `internal/domain/providertoken` | `tok_<scenario>[_<suffix>]` with scenario `approve`, `decline`, `timeout` or `expire-early`; a 13 to 19 digit string is refused as a card number. |
| The Idempotency-Key store | `internal/application/idempotency`, Postgres table | One global key scope ([ADR 0004](/docs/adr/0004-adopt-the-fleet-rest-api-guideline)); a replay returns the stored answer. |
| The provider contract | `apis/openapi.yaml` | Generated into the [REST reference](/docs/api-reference). |

## What it does NOT own

| Not owned | Owner |
| --- | --- |
| Storefront money movement: when to authorize, capture per shipment group, release or refund | `customer-payments`, which calls this service over HTTP |
| The checkout session and the purchase saga | `checkout` |
| Events of any kind | nobody here: the simulator publishes and consumes **no** events, so there is no Kafka at runtime ([ADR 0002](/docs/adr/0002-payment-provider-simulator-assumptions) A16; `apis/asyncapi.yaml` declares no channel and no operation) |
| Authentication | nobody: REST is unauthenticated, as everywhere in the fleet |

## Scenarios

| Scenario | Behaviour |
| --- | --- |
| `approve` | authorizes; the hold lasts 7 days |
| `decline` | every authorization answers `402 payment-declined` with `code: CARD_DECLINED` |
| `timeout` | the authorization answers `504 provider-timeout` after `PROVIDER_TIMEOUT_DELAY` (default 1 s, at most 2 s); no hold is created |
| `expire-early` | authorizes; the hold lasts 5 minutes, so re-authorization before expiry is testable (a re-authorized hold keeps the scenario) |

`POST /tokens {"scenario":"decline"}` returns `tok_decline_<suffix>`; the bare
literals `tok_approve`, `tok_decline`, `tok_timeout` and `tok_expire-early` work
too. A 4xx decline is final; 5xx and 504 are transient and a client retries
them with the same `Idempotency-Key`.

## Code layout

Hexagonal (ports and adapters); `TestHexagonalArchitecture` in
`internal/architecture/` fails the build when a dependency points outward.

| Path | Holds |
| --- | --- |
| `cmd/api/` | the composition root: configuration, telemetry, Postgres pool, boot migrations, HTTP server, sequential shutdown |
| `internal/domain/` | `providerauth`, `providertoken`, `shared`: pure business rules |
| `internal/application/` | `ports/` (outbound interfaces), `usecases/` (one struct per use case), `idempotency/`, `views/` |
| `internal/adapters/inbound/http/` | chi router, handlers, problem+json mapping, the Idempotency-Key middleware, the request-duration middleware |
| `internal/adapters/outbound/postgres/` | pgx repositories and the embedded migration `0001_provider_schema` |
| `internal/adapters/outbound/memory/` | test doubles only, never linked into `cmd/` (`TestRuntimeBinaryHasNoInMemoryAdapters`) |
| `internal/adapters/kafka/cloudevents/`, `internal/adapters/outbound/kafka/` | harness packages kept for the fleet fitness tests; nothing is published |
| `features/` | godog scenarios |
| `charts/payment-provider-simulator/` | the Helm chart |

## Where to go next

| You want to | Read |
| --- | --- |
| Run it locally and make the first calls | [Quickstart](/docs/overview/quickstart) |
| Configure, deploy or operate it | [Configuration](/docs/operations/configuration), [Runbook](/docs/operations/runbook) |
| Test or change it | [Testing](/docs/development/testing) |
| Call it | [API overview](/docs/api-reference) and the generated REST pages |
| Know why it is built this way | [Architecture decision records](/docs/adr) |
