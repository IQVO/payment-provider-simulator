---
id: quickstart
title: Quickstart
sidebar_label: Quickstart
sidebar_position: 2
---

# Quickstart

From a clean checkout to a running `payment-provider-simulator` on real
Postgres, and the first calls against it. The outputs below were captured from
a real run of `cmd/api` built from this branch against Postgres 16 in Docker
(ids and timestamps differ on every run).

## Prerequisites

| Need | Why |
| --- | --- |
| Go `1.27.3` (`go.mod`) | build and test |
| Docker | a local Postgres for a manual run; the integration suite starts its own Postgres with testcontainers |
| `golangci-lint` v2.14.0, `gremlins` v0.6.0, `govulncheck` (optional) | `make lint`, `make mutation`, `make vuln` print the exact install command when the tool is missing |
| `lefthook install` (once per clone) | pre-commit runs `make fmt-check`, `make vet`, `make lint`; pre-push runs `make check` |

No Kafka broker is needed: the simulator publishes and consumes no events.

## 1. Postgres

```sh
docker run -d --name pps-pg -e POSTGRES_PASSWORD=pw -e POSTGRES_DB=payment_provider -p 55461:5432 postgres:16
```

## 2. Start `cmd/api`

`DATABASE_URL` is the only mandatory variable; the boot migrations run against
it (or against `MIGRATIONS_DATABASE_URL` when set). Keep the password out of the
URL and pass it with `PGPASSWORD`:

```sh
export PGPASSWORD=pw
export DATABASE_URL="postgres://postgres@localhost:55461/payment_provider?sslmode=disable"
go run ./cmd/api
```

```json
{"time":"2026-11-10T06:10:10.985951-03:00","level":"INFO","msg":"postgres adapters configured"}
{"time":"2026-11-10T06:10:10.986389-03:00","level":"INFO","msg":"http server listening","addr":":18091"}
```

(The capture set `HTTP_ADDR=:18091`; the default is `:8080`, used below.)

Without `DATABASE_URL` the binary exits with status 1 before opening anything:
`{"level":"ERROR","msg":"service exited with error","error":"DATABASE_URL is required: this service runs on real Postgres, there is no in-memory mode"}`.

```sh
curl -s http://localhost:8080/healthz   # {"status":"ok"}
curl -s http://localhost:8080/readyz    # {"status":"ready"}
```

## 3. First calls

Issue a token for a scenario (the body is optional; absent means `approve`):

```sh
curl -s -X POST http://localhost:8080/tokens -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: qs-token-2' -d '{"scenario":"decline"}'
```

```json
{"token":"tok_decline_fe2e990bb96a","scenario":"decline"}
```

Place a 100.00 EUR hold with the bare `tok_approve` literal:

```sh
curl -s -i -X POST http://localhost:8080/authorizations -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: qs-auth-1' \
  -d '{"amountMinor":10000,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"checkout-qs-1"}'
```

```text
HTTP/1.1 201 Created
Content-Type: application/json

{"id":"hold_c4f9babd0266","expiresAt":"2026-11-17T09:10:27.625676Z","amountMinor":10000,"currency":"EUR"}
```

The same request with the same key is a replay: the same body, plus
`Idempotency-Replayed: true`. The same key with another body is refused:

```text
HTTP/1.1 409 Conflict
Content-Type: application/problem+json

{"type":"/problems/idempotency-key-reused","title":"Idempotency-Key was already used with a different request","status":409,"detail":"Idempotency-Key was already used with a different request","instance":"/authorizations"}
```

A `tok_decline` authorization is a final decline:

```text
HTTP/1.1 402 Payment Required
Content-Type: application/problem+json

{"type":"/problems/payment-declined","title":"The payment provider declined the authorization","status":402,"detail":"the payment method was declined: CARD_DECLINED","instance":"/authorizations","code":"CARD_DECLINED"}
```

A `POST` without the header is `400 idempotency-key-required`; a card number in
`paymentMethodToken` is `422 validation-failed` ("a card number is never
accepted, only a payment-method token"); `tok_timeout` answered
`504 provider-timeout` after 1.04 s with the default `PROVIDER_TIMEOUT_DELAY`.

## 4. Move money against the hold

Each call carries its own `Idempotency-Key`:

| Call | Body | Answer |
| --- | --- | --- |
| `POST /authorizations/hold_c4f9babd0266/captures` | `{"amountMinor":4000}` | `201 {"id":"capture_15ae5076c4ed","amountMinor":4000,"currency":"EUR"}` |
| `POST /authorizations/hold_c4f9babd0266/reductions` | `{"amountMinor":5000}` (the new REMAINING hold: 6000 becomes 5000) | `200 {"id":"hold_c4f9babd0266"}` |
| `POST /authorizations/hold_c4f9babd0266/captures` | `{"amountMinor":2000}` | `201 {"id":"capture_5d2e16ee7598",...}` |
| `POST /authorizations/hold_c4f9babd0266/refunds` | `{"amountMinor":1500}` | `201 {"id":"refund_dca87bb30626",...}` |

Read the hold (a test aid; ADR 0002 A14):

```sh
curl -s http://localhost:8080/authorizations/hold_c4f9babd0266
```

```json
{"id":"hold_c4f9babd0266","status":"AUTHORIZED","scenario":"approve","reference":"checkout-qs-1","currency":"EUR","heldMinor":9000,"capturedMinor":6000,"refundedMinor":1500,"remainingMinor":3000,"expiresAt":"2026-11-17T09:10:27.625676Z","createdAt":"2026-11-10T09:10:27.625676Z","movements":[{"id":"capture_15ae5076c4ed","kind":"CAPTURE","amountMinor":4000,"at":"2026-11-10T09:10:38.358934Z"},{"id":"capture_5d2e16ee7598","kind":"CAPTURE","amountMinor":2000,"at":"2026-11-10T09:10:38.448177Z"},{"id":"refund_dca87bb30626","kind":"REFUND","amountMinor":1500,"at":"2026-11-10T09:10:38.478288Z"}]}
```

`POST /authorizations/hold_c4f9babd0266/voids` then releases the remaining 30.00:
the hold reads `"status":"VOIDED"`, `"heldMinor":6000`, `"remainingMinor":0`. An
unknown id is `404 not-found`.

## 5. Clean up

```sh
docker rm -f pps-pg
```

Next: [Configuration](/docs/operations/configuration) for every variable, and
[Testing](/docs/development/testing) for the gates to run before a push.
