---
paths:
  - "internal/adapters/inbound/http/**"
  - "apis/openapi*.yaml"
  - "apis/openapi/**"
---

# REST API (inbound adapter)

Route -> use case mapping, kept in sync with `apis/openapi.yaml` by hand. Every POST needs an `Idempotency-Key`.

- `POST /tokens`                              -> `IssueToken`
- `POST /authorizations`                      -> `Authorize`
- `GET /authorizations/{id}`                  -> `GetAuthorization`
- `POST /authorizations/{id}/void`            -> `Void`
- `POST /authorizations/{id}/reauthorize`     -> `Reauthorize`
- `POST /authorizations/{id}/reduce`          -> `Reduce`
- `POST /authorizations/{id}/captures`        -> `Capture`
- `POST /authorizations/{id}/refunds`         -> `Refund`
- `GET /healthz`, `GET /readyz`               -> liveness / readiness (Postgres ping + drain gate)

## Conventions

- Error shape: RFC 9457 `application/problem+json`, `type` = `/problems/<slug>`: `malformed-request` (400),
  `idempotency-key-required` (400), `not-found` (404), `payment-declined` (402, extension member `code`),
  `over-capture` / `over-refund` / `hold-expired` / `illegal-transition` (409), `validation-failed` /
  `not-a-reduction` / `idempotency-key-reused` (422), `provider-timeout` (504), `dependency-unavailable` (503).
- 4xx decline is final; 5xx / 504 are transient (retry with the same key).
- `rejectNUL` runs first: a NUL in path, query or body is a client error, never a Postgres 22021.
- Auth: none. The fleet-wide REST+MCP auth was deliberately reverted 2026-09-11 and stays off;
  `internal/architecture/fitness_test.go`'s TestNoAuthMiddlewareReintroduced guards it.
- Scenario vocabulary (documented in README and OpenAPI): approve, decline, timeout, expire-early.
