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
- `POST /authorizations/{id}/voids`            -> `Void`
- `POST /authorizations/{id}/reauthorizations`     -> `Reauthorize`
- `POST /authorizations/{id}/reductions`          -> `Reduce`
- `POST /authorizations/{id}/captures`        -> `Capture`
- `POST /authorizations/{id}/refunds`         -> `Refund`
- `GET /healthz`, `GET /readyz`               -> liveness / readiness (Postgres ping + drain gate)

## Conventions

- Error shape: RFC 9457 `application/problem+json`, `type` = `/problems/<slug>`: `malformed-request` (400),
  `idempotency-key-required` (400), `not-found` (404), `payment-declined` (402, extension member `code`),
  `over-capture` / `over-refund` / `hold-expired` / `illegal-transition` (409), `validation-failed` /
  `not-a-reduction` / `idempotency-key-reused` (409), `provider-timeout` (504), `dependency-unavailable` (503).
- 4xx decline is final; 5xx / 504 are transient (retry with the same key).
- Replay classes (ADR 0005): every problem type above declares FINAL, OUTCOME or RETRYABLE on its
  catalogue entry. FINAL (`validation-failed`, `not-found`, `payment-declined`, `over-capture`,
  `not-a-reduction`, `hold-expired`, `illegal-transition`, the pre-store 400/404/409s) is stored
  and replayed; RETRYABLE (`over-refund`, `provider-timeout`, `internal-error`,
  `dependency-unavailable`) is not stored — the key is released and the retry executes again.
  No entry is OUTCOME: no keyed command commits state before refusing.
- Retention (ADR 0005, addendum 2026-11-22): a stored answer lives for `idempotency.RetentionTTL`
  (24 h). Inside the window the replay contract is unchanged; past it the key is free — the claim
  refreshes the expired row and the request re-executes (another request under the key is a new
  request, not `idempotency-key-reused`). A background purge (`cmd/api/retention.go`) deletes the
  expired rows hourly in bounded batches.
- `rejectNUL` runs first: a NUL in path, query or body is a client error, never a Postgres 22021.
- Auth: none. The fleet-wide REST+MCP auth was deliberately reverted 2026-09-11 and stays off;
  `internal/architecture/fitness_test.go`'s TestNoAuthMiddlewareReintroduced guards it.
- Scenario vocabulary (documented in README and OpenAPI): approve, decline, timeout, expire-early.
