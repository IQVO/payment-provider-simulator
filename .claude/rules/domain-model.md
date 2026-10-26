# Domain model: ubiquitous language, aggregates, events, use cases

## Ubiquitous Language (use these exact names)

- **ProviderAuthorization** (the "hold") — one hold the simulated provider placed on a payment method, with the
  captures and refunds taken against it. Protects: captures never exceed the remaining hold, refunds never exceed the
  captured total, no capture after expiry.
- **Token** — a PSP payment-method token `tok_<scenario>[_<suffix>]`. It names its Scenario; only tokens travel, never
  a card number.
- **Scenario** — the deterministic behaviour a token selects: `approve`, `decline`, `timeout`, `expire-early`.
- **Capture / Refund (Movement)** — money taken from a hold / returned to the payer; stored on the hold.
- **Reduce** — partial release: lowers the REMAINING hold, never raises it.
- **Reauthorize** — replaces a hold by a new one (new id, new expiry); the old hold is SUPERSEDED.
- **Idempotency-Key** — header of every POST; the same key (per operation) returns the first answer.

## Aggregates

- **ProviderAuthorization** (`internal/domain/providerauth`): states AUTHORIZED, CAPTURED, VOIDED, SUPERSEDED; only
  AUTHORIZED accepts hold changes. The hold is the consistency boundary: capture and refund records are part of it.
- Value vocabulary `internal/domain/providertoken` (Scenario, token parsing, hold validity) and error sentinels
  `internal/domain/shared`.

## Domain events

None. The simulator publishes and consumes no events (ADR 0002 A16).

## Key use cases (`internal/application/usecases`)

- `IssueToken` — mints a scenario token (`ports.IdempotencyStore`).
- `Authorize` — scenario decides: hold, decline answer, or stalled timeout (`AuthorizationRepository`, `UnitOfWork`).
- `Void`, `Reduce`, `Reauthorize` — hold changes under the hold's row lock.
- `Capture`, `Refund` — movements against a hold.
- `GetAuthorization` — read model of a hold with its movements.
