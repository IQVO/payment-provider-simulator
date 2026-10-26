# payment-provider-simulator

Storefront (e-commerce) domain of the retail-network organisation, bounded context **Payment Provider Simulator** (Generic, storefront tier).

Go stand-in for the external payment provider (own Postgres): tokenize (hosted-fields stand-in), authorize, capture, void, refund, re-authorize and reduce (partial release) with deterministic outcomes per test token. The HTTP contract is the one customer-payments documents in its ADR 0002 assumption A12; swapping the simulator for a real PSP changes nothing in the storefront (only `PAYMENT_PROVIDER_URL`).

The design lives in [warehouse-docs](https://iqvo.github.io/warehouse-docs/storefront/) `storefront/model/model.yaml`; the assumptions made while building are in `docs/adr/0002-payment-provider-simulator-assumptions.md`; the contract is `apis/openapi.yaml`.

Runtime: Go, hexagonal (domain `providerauth` / `providertoken`, application ports and use cases, chi HTTP, Postgres adapter with embedded migrations), real PostgreSQL only (`DATABASE_URL` is mandatory). The simulator publishes and consumes **no** events (ADR 0002 A16), so there is no Kafka dependency.

## Contract in one screen

Every POST carries an `Idempotency-Key`; the same key returns the same answer. Bodies are JSON, amounts are integer minor units, only PSP tokens travel, never a card number.

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

## Scenario vocabulary

A token names its scenario, so a test always gets the same outcome. `POST /tokens {"scenario":"decline"}` returns `tok_decline_<suffix>`; the bare literals `tok_approve`, `tok_decline`, `tok_timeout` and `tok_expire-early` work too.

| Scenario | Behaviour |
| --- | --- |
| `approve` | authorizes; the hold lasts 7 days |
| `decline` | every authorization answers `402 payment-declined`, `code: CARD_DECLINED` |
| `timeout` | the authorization answers `504 provider-timeout` after a short delay (`PROVIDER_TIMEOUT_DELAY`, default 1 s, at most 2 s); no hold is created |
| `expire-early` | authorizes; the hold lasts 5 minutes, so re-authorization before expiry is testable (a re-authorized hold keeps the scenario) |

A string that is not a token is `422`; a 13 to 19 digit string is refused as a card number.

## Running

```
DATABASE_URL=postgres://... go run ./cmd/api
```

| Variable | Meaning |
| --- | --- |
| `DATABASE_URL` | Postgres DSN (required) |
| `HTTP_ADDR` | listen address (default `:8080`) |
| `MIGRATIONS_DATABASE_URL` | direct DSN for the boot migrations (default `DATABASE_URL`) |
| `PROVIDER_TIMEOUT_DELAY` | stall of the timeout scenario (default `1s`, max `2s`) |
| `SHUTDOWN_DRAIN_DELAY` | wait after `/readyz` flips to 503 on shutdown (default `5s`) |

`make check` is the fast gate, `make check-all` the full one, `make integration` runs the Postgres tests on testcontainers. See `HARNESS.md` for every gate.

## Disclaimer

This project is a domain-driven design study. It is not associated with, endorsed by or affiliated with any company, and it uses no company's code, data, logos or trademarks. All names, figures and examples are illustrative or synthetic.

See `docs/adr/` for decisions and `docs/before-first-release.md` for the packaging checklist.
