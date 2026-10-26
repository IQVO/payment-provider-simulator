# 2. Payment Provider Simulator assumptions

Status: Accepted. Contract source: customer-payments `docs/adr/0002-customer-payments-assumptions.md`, assumption A12.

## Context

The simulator is the stand-in for the external payment provider. customer-payments documents the HTTP contract it
speaks (A12) and ships a reference client and a local fake. Where A12 or the model is silent, the least-surprising
reading is recorded here as an assumption.

## Assumptions

- **A1** The provider contract is A12 verbatim: every POST carries an `Idempotency-Key`, the same key returns the
  same answer, bodies are JSON, amounts are integer minor units, only PSP tokens travel. A missing key is 400
  `idempotency-key-required`.
- **A2** Idempotency is scoped by (operation, key). The first request fingerprint is stored with the answer; the same
  key with another request is 422 `idempotency-key-reused`. The claim, the effect and the stored answer commit in ONE
  Postgres transaction, and a concurrent duplicate waits on the unique index, so an effect never happens twice. A
  failed command rolls its claim back, so its key stays usable; successes and declines are replayed byte for byte.
- **A3** Tokens are self-describing: `tok_<scenario>[_<suffix>]` with scenario one of `approve`, `decline`,
  `timeout`, `expire-early`. `POST /tokens` mints one (empty scenario = approve); the bare literals (`tok_approve`,
  `tok_decline`, ...) are accepted too, so consumer tests need no round trip. Authorizing does not look the token up:
  nothing about a token is stored besides the idempotent answer of `POST /tokens`. A string that is not a token is 422;
  a 13 to 19 digit string is refused as a card number.
- **A4** `decline` answers `402` problem+json `payment-declined` with the extension member `code`
  (`CARD_DECLINED`), because the reference client reads 402 as a final decline and the `code` member from the body.
  The decline is stored under the key like any answer. Decline applies to authorizations only.
- **A5** `timeout` answers `504` problem `provider-timeout` after `PROVIDER_TIMEOUT_DELAY` (default 1 s, capped at
  2 s, `0` = none) and creates no hold, so a retry is always safe. 504 is not stored; the token times out every time.
- **A6** `expire-early` holds last 5 minutes (default holds 7 days). A re-authorized hold keeps its scenario.
- **A7** `reduce {amountMinor}` sets the new REMAINING hold (the reference port: "lowers the remaining hold to
  amountMinor"); it never raises it (422 `not-a-reduction`); equal is a no-op; zero closes the hold.
- **A8** `reauthorize {amountMinor}` replaces the hold by a NEW hold of that amount (new id, new expiry) and marks the
  old one SUPERSEDED, whatever the old hold's expiry (a saga may recover after expiry). Captures already taken stay
  with the old hold; refunds against them remain possible on the old id.
- **A9** `void` releases the remaining hold, also after a partial capture. A voided, fully captured or superseded hold
  answers 409 `illegal-transition` to void, reduce, reauthorize and capture.
- **A10** `capture` needs the hold unexpired (409 `hold-expired`, at or after `expiresAt`) and within the remaining
  hold (409 `over-capture`). `refund` works in any state and never exceeds the captured total (409 `over-refund`).
- **A11** Status codes: authorize, reauthorize, captures, refunds and tokens answer 201; void and reduce 200. The
  reference client accepts any 2xx.
- **A12** Capture and refund records are stored as a JSON list on the hold row (`movements`), not as their own tables:
  they are never queried on their own and the hold is the consistency boundary.
- **A13** Commands on one hold serialize on `SELECT .. FOR UPDATE`; there is no version column.
- **A14** `GET /authorizations/{id}` is an addition for tests (the provider dashboard); it is not part of A12.
- **A15** The system clock reads UTC truncated to the microsecond (Postgres precision), as in the fleet.
- **A16** Kafka: the simulator publishes and consumes no business events. The harness's `cloudevents` and `kafka`
  adapter packages stay in the tree (the fitness tests expect them) but no producer or consumer is wired, no
  `KAFKA_BROKERS` is required and `/readyz` checks Postgres and the drain gate only. `apis/asyncapi.yaml` declares no
  channel so the shared `api-lint` job keeps one entry per service. No arch or lint gate had to be weakened.
- **A17** The hold currency is a three-letter upper-case code; `reference` is required (the checkout session id).

## Consequences

Swapping the simulator for a real provider changes only `PAYMENT_PROVIDER_URL` in customer-payments. Any divergence a
consumer finds becomes a new assumption here.
