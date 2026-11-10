---
id: overview
title: API overview
sidebar_label: Overview
slug: /api-reference
sidebar_position: 1
---

# API overview

:::info Implemented by cmd/api
`cmd/api` serves the REST routes below (`internal/adapters/inbound/http`) on
`HTTP_ADDR` (default `:8080`). The contract is `apis/openapi.yaml`; it is the
provider contract customer-payments documents in its ADR 0002, assumption A12,
and a change to it breaks customer-payments' Anti-Corruption Layer: change both
sides together, contract first. See the [Quickstart](/docs/overview/quickstart)
for real responses.
:::

| Interface | Source of truth | Notes |
| --- | --- | --- |
| REST | `apis/openapi.yaml` (OpenAPI 3.0.3, version 1.0.0) | The pages under *REST* are generated from the spec with `npm run gen-api-docs`; do not edit them by hand (`docs-api-drift` fails on a diff). Server named in the spec: `http://localhost:8080`; through Kong in the fleet cluster the base path is `/api/payment-provider-simulator` (port 8000). |
| Events | `apis/asyncapi.yaml` | Deliberately empty: `channels: {}`, `operations: {}`. The simulator publishes and consumes no events ([ADR 0002](/docs/adr/0002-payment-provider-simulator-assumptions) A16). |
| MCP, A2A | none | This repository has neither. |

## Endpoints

| Tag | Endpoint | Success | Notable errors |
| --- | --- | --- | --- |
| Tokens | `POST /tokens` | `201 {token, scenario}`; body optional, absent means `approve` | `422` unknown scenario |
| Authorizations | `POST /authorizations` | `201 {id, expiresAt, amountMinor, currency}` | `402 payment-declined` (`code: CARD_DECLINED`), `504 provider-timeout`, `422 validation-failed` (not a token, a card number, a bad currency) |
| Authorizations | `GET /authorizations/{id}` | `200`, the hold with `heldMinor`, `capturedMinor`, `refundedMinor`, `remainingMinor` and its `movements` (a test aid, ADR 0002 A14) | `404 not-found` |
| Authorizations | `POST /authorizations/{id}/voids` | `200`, releases the whole remaining hold | `409 illegal-transition` when not `AUTHORIZED` |
| Authorizations | `POST /authorizations/{id}/reauthorizations` | `201`, the NEW hold (new id and expiry); the old one becomes `SUPERSEDED` | `409 illegal-transition`, `422` |
| Authorizations | `POST /authorizations/{id}/reductions` | `200`; `amountMinor` is the new REMAINING hold; reducing to zero closes the hold | `422 not-a-reduction`, `409 illegal-transition` |
| Authorizations | `POST /authorizations/{id}/captures` | `201 {id, amountMinor, currency}`; partial captures accumulate | `409 over-capture`, `409 hold-expired`, `409 illegal-transition` |
| Authorizations | `POST /authorizations/{id}/refunds` | `201 {id, amountMinor, currency}`; accepted in any hold state | `409 over-refund` |
| Platform | `GET /healthz` | `200 {"status":"ok"}` | |
| Platform | `GET /readyz` | `200 {"status":"ready"}` | `503 dependency-unavailable` while Postgres does not answer, `503 {"status":"not_ready"}` while draining |

## Conventions in the contract

- **Idempotency-Key.** Required on every `POST`; 1 to 128 printable ASCII
  characters ([ADR 0004](/docs/adr/0004-adopt-the-fleet-rest-api-guideline)).
  The key is global. A replay of the same request returns the stored answer
  with `Idempotency-Replayed: true` (the first execution never carries it); the
  same key with another method, path or body is `409 idempotency-key-reused`; a
  missing key is `400 idempotency-key-required`, a malformed one
  `400 idempotency-key-invalid`. Declines (`402`) are stored and replayed; a
  failed command (5xx, or a business 4xx such as `over-capture`) rolls its claim
  back.
- **Money** is integer minor units (`amountMinor`) with a three-letter
  upper-case `currency`. Only PSP tokens travel, never a card number.
- **Errors** are RFC 9457 `application/problem+json` with
  `type: /problems/<slug>`. A 4xx decline is final; 5xx and 504 are transient.
- **No collections.** There is no list route, so nothing is paginated.
- **No authentication**, as everywhere in the fleet.
