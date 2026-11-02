# ADR 0004: Adopt the fleet REST API guideline

- Status: Accepted
- Date: 2026-11-01

## Context

The 2026-10-30 application portfolio assessment found divergent REST conventions across the warehouse fleet: verb
URLs, key reuse answered 422 in most services, two spellings of the replay header and unpaginated lists. The fleet
REST API guideline (warehouse-docs) removes them as one hard cutover, enforced by the `rest-guideline` Spectral rule
(R1-R8) in the `api-lint` job.

## Decision

The simulator adopts the guideline in full:

- URLs name things: a lifecycle transition is a POST creating a transition resource (plural noun).
- Every POST requires `Idempotency-Key` (1-128 printable ASCII; missing is 400 `idempotency-key-required`, malformed
  400 `idempotency-key-invalid`). The check runs as route-scoped middleware before the body is read; the use case
  then claims the key in the SAME unit of work as the aggregate change, so the effect happens once.
- The key is GLOBAL: every key is claimed in one scope and the stored fingerprint includes the operation and its
  inputs (hold id, amount, token, ...). The same key with another request, on any route, is 409
  `idempotency-key-reused` (was 422). A replay returns the stored answer with `Idempotency-Replayed: true`; the first
  execution never carries the header. Declines (402) are stored and replayed; a failed command (5xx, business 4xx
  such as `over-capture`) rolls its claim back.
- Collections: the simulator has no collection GET (`GET /authorizations/{id}` is an item), so nothing is paginated
  and nothing is exempt.

### Renamed routes (no aliases; the old routes answer 404)

| Old | New |
| --- | --- |
| `POST /authorizations/{id}/void` | `POST /authorizations/{id}/voids` |
| `POST /authorizations/{id}/reauthorize` | `POST /authorizations/{id}/reauthorizations` |
| `POST /authorizations/{id}/reduce` | `POST /authorizations/{id}/reductions` |

Status codes and bodies are unchanged; `operationId`s are unchanged.

### Exemptions

None.

### Consumers changed

This repo has no outbound HTTP client and no web remote; the BDD step definitions
(`features/steps_holds_test.go`) and the HTTP adapter tests moved to the new routes. No other repo's code was found
calling the renamed routes.

## Consequences

- Breaking: must deploy together with the REST cutover set.
- The fingerprint is the operation plus the decoded inputs, not a hash of the raw body bytes: two bodies that decode
  to the same inputs (whitespace, key order) are the same request and replay.
