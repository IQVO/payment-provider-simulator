# ADR 0005: Every problem type has a replay class

- Status: Accepted (2026-11-21); amended 2026-11-22 (a 24 h idempotency-key TTL and an hourly purge — the retention open point of judgement call 4, order-sourcing #84 / last-mile-delivery #86 / exception-management #76)

## Context

The fleet rule of 2026-11-18 (owner decision; `.claude/rules/fleet/idempotency-and-outbox.md`, standard in warehouse-harness-template `docs/idempotency-replay-classes.md`) supersedes one sentence of ADR 0004: "a failed command (5xx, business 4xx such as `over-capture`) rolls its claim back". Under the rule, a request retried under the same `Idempotency-Key` is answered from the store only when its first answer is final for that request, and every problem type a keyed route can answer declares exactly one class on its catalogue entry:

- **FINAL** — the answer is a function of the request alone (or of state that provably only moves one way): stored and replayed.
- **OUTCOME** — the command committed state, then refused: stored, and the writes commit with the stored answer.
- **RETRYABLE** — nothing committed and the answer depends on state that can change: NOT stored; the key is released and a retry executes again.

The simulator's store is not an HTTP middleware: every POST runs the keyed-run engine `usecases.Deps.Once` (`internal/application/usecases/deps.go`), which claims the key, runs the effect and stores the answer in ONE unit of work (`postgres/idempotency.go`: `INSERT .. ON CONFLICT (operation, key) DO NOTHING`, then `UPDATE` of the answer). Until this decision that meant no refusal was ever stored: any effect error rolled the claim back with everything else. The one exception was the 402 decline, which has always been a stored `views.Answer` (ADR 0004) — the behaviour customer-payments' ACL depends on ("the provider owns and replays its own decline", customer-payments ADR 0008).

## Decision

Every catalogue entry in `internal/adapters/inbound/http/errors.go` (plus the adapter's own problems) declares its class. `Once` asks the catalogue through a new `ports.Refusals` port: a FINAL or OUTCOME refusal is stored as `views.Answer{Kind: PROBLEM, Slug, Detail}` and the unit COMMITS (the first answer is byte-identical; the retry replays it with `Idempotency-Replayed: true`); RETRYABLE, unclassified and 5xx errors roll the whole unit back exactly as before — the key is released, the retry executes, a different body under the released key is a NEW request. A 2xx is stored as before; a 5xx and a panic are never stored.

| Problem type | Class | Status | Reason (code) |
| --- | --- | --- | --- |
| `malformed-request` | FINAL | 400 | the request alone (`decodeJSON`/`decodeOptional` in `response.go`/`handlers.go`, `rejectNUL` in `nul.go`); answered before the claim, so nothing is stored under a key for it |
| `idempotency-key-required` | FINAL | 400 | the header alone (`idempotency_key.go`); the middleware answers before the store |
| `idempotency-key-invalid` | FINAL | 400 | the header alone (`idempotency_key.go`); the middleware answers before the store |
| `idempotency-key-reused` | FINAL | 409 | the request vs the stored fingerprint (`deps.go` `Once`); the answer of a hit, never stored |
| `resource-not-found` | FINAL | 404 | a NUL path can never name a resource (`nul.go`); answered before the store |
| `not-found` | FINAL | 404 | the command's own target: hold ids are minted here (`internal/adapters/outbound/ids/ids.go`: `hold_` + 12 random hex, not caller-chosen), nothing is ever deleted (no DELETE in the postgres adapter) and no event can deliver the id later — an unknown id stays unknown forever |
| `validation-failed` | FINAL | 422 | every `shared.Validation` site checks the request's own fields only (`providertoken/token.go` scenario/card/token, `providerauth/authorization.go:83-92` id/reference/currency/amount, `providerauth/commands.go:26,40,67` amounts, `usecases/authorize.go` `validateAmount`) |
| `payment-declined` | FINAL | 402 | the scenario is fixed in the token; the decline is a stored Answer replayed under the key (ADR 0004) — unchanged, and the class now says so |
| `over-capture` | FINAL | 409 | one-way state: `RemainingMinor = held − captured`; captured only grows (`commands.go:31`), held only shrinks (`commands.go:55,72`), nothing raises either — an amount over the remaining hold can never pass later. Reclassify if a hold-raise/extension feature lands |
| `not-a-reduction` | FINAL | 409 | same one-way proof: `commands.go:69` compares against `RemainingMinor`, which only shrinks |
| `hold-expired` | FINAL | 409 | `expiresAt` is fixed at authorize (`authorization.go`); the clock only moves forward, so an identical later capture is still expired (`commands.go:22`). Reclassify if an expiry-extension feature lands |
| `illegal-transition` | FINAL | 409 | status leaves AUTHORIZED one-way (`settle` `commands.go:95-104`, `Void` :56, `Reauthorize` :88); no un-void, un-supersede or un-capture exists |
| `over-refund` | RETRYABLE | 409 | refundable = captured − refunded, and captured GROWS with any later capture (`commands.go:31,42`): the identical refund passes after one — proven over real Postgres |
| `provider-timeout` | RETRYABLE | 504 | a 5xx is never stored; answered before the claim (`usecases/authorize.go:32-37`), so the retry stalls again |
| `internal-error` | RETRYABLE | 500 | a 5xx is never stored (the fallback of `problemFor`) |
| `dependency-unavailable` | RETRYABLE | 503 | a 5xx; `/readyz` only (`readiness.go`), classified for completeness |

### There is no OUTCOME entry

Every refusal is raised before any `Save`: `usecases/holds.go` `mutate`/`movement` apply the domain change to the aggregate first and save only when it succeeds, and `providerauth/commands.go` refuses inside the aggregate. No keyed command commits state and then refuses, and the single transaction means a refusal discards any write that preceded it. OUTCOME therefore cannot exist in this store; the mechanism (`Once` commits a stored OUTCOME refusal with the unit) exists and is exercised by the reference test, but no entry uses it.

### Judgement calls

1. The hold-state 409s (`over-capture`, `not-a-reduction`, `hold-expired`, `illegal-transition`) read aggregate state but every input moves one way only (captured monotone up, held monotone down, expiry fixed, status irreversible), so the identical retry can never pass: FINAL per the fleet's one-way-state rule, with the reclassification trigger named per row. `over-refund` is the counter-proof that keeps RETRYABLE. Phase-0 guessed all five RETRYABLE; the code decides.
2. `validation-failed` and `not-found` refusals are now STORED and replayed (behaviour change vs ADR 0004): the retry of the same body replays the 422/404 with `Idempotency-Replayed: true`, and a different body under that key is 409 `idempotency-key-reused` instead of a new request. Descriptions-only contract change (oasdiff clean).
3. The pre-store FINAL entries (`malformed-request`, `idempotency-key-required`/`-invalid`/`-reused`, `resource-not-found`) are never stored in production — they are answered before or beside the claim; the class is declarative (checkout #65 J8, warehouse-ops-agent #230 precedent).
4. No TTL is enforced at all (`idempotency_records.created_at` exists; nothing purges, no age check on replay): an open point, to be fixed in its own PR (shape: order-sourcing #84 / last-mile-delivery #86).

### Consumers

customer-payments (ADR 0008) is the only caller: it sends deterministic keys `<op>:<authorizationID>:<groupNo>:<qualifier>` and retries internal attempts under them. After a RETRYABLE refusal (an `over-refund`) its retry re-executes the operation here — desired per the fleet rule; after a FINAL refusal it now replays the stored answer. No consumer-side retry behaviour is added. The simulator has no outbound HTTP client, so it calls nobody with a key of its own.

## Consequences

- A retry of a FINAL refusal answers from the store (byte-identical, `Idempotency-Replayed: true`); a retry of a RETRYABLE refusal executes again; a different body under a released key is a new request.
- A concurrent duplicate waits on the store's unique index until the first unit ends: a released (RETRYABLE) first request leaves the waiter a fresh claim, so it EXECUTES the request itself; a committed first answer (2xx or stored FINAL) is replayed to the waiter.
- `replay_class_catalogue_test.go` holds this table equal to the catalogue; the harness reference test (`idempotency_replay_classes_test.go`, verbatim) proves each class over the keyed-run engine.

## Addendum 2026-11-22: a 24 h TTL and an hourly purge

Judgement call 4 above left retention as this ADR's open point: "No TTL is
enforced at all (`idempotency_records.created_at` exists; nothing purges, no
age check on replay)". This PR resolves it the fleet way (the pattern is
order-sourcing #84's worker, last-mile-delivery #86's port shape and
exception-management #76's TTL introduction — this store had NO TTL, so a
stored answer replayed forever):

- **One constant**, `idempotency.RetentionTTL = 24 h` (the fleet default):
  the single age shared by the replay read's guard, the claim's refresh and
  the purge.
- **Age guard on the replay read**: `Claim`'s SELECT answers only a row
  with `created_at > now() - TTL`, so an expired row can never answer a
  replay between expiry and the sweep.
- **Refresh on the claim**: the claim INSERT's conflict arm became
  `DO UPDATE SET fingerprint, answer = NULL, created_at = now() WHERE
  created_at <= now() - TTL`. A LIVE row matches nothing (0 rows affected)
  and replays exactly as before; an EXPIRED row is refreshed in place and
  the request EXECUTES — past the window the key is free, and a different
  request under it is a NEW request instead of 409
  `idempotency-key-reused`. The `idempotency-key-reused` FINAL premise
  above held on "no TTL, no purge"; it now holds within the 24 h window
  only.
- **Purge**: `PurgeExpired(ctx, limit)` on `ports.IdempotencyStore`
  (Postgres and the memory double), run by `cmd/api`'s first background
  worker — one pass at start, then hourly, batches of 500 picked oldest
  first with `FOR UPDATE SKIP LOCKED`, the age re-checked on the DELETE, no
  advisory lock, never waiting on a row a request holds (migration 0002
  indexes `created_at`). Expiry is enforced at read; the purge only stops
  the table from growing. The shared contract suite
  `repocontract.RunIdempotencyRetention` pins both stores to the same
  semantics.

**What the TTL means for customer-payments (ADR 0008).** That dependency
retries internal attempts under deterministic keys `<op>:<authorizationID>:<groupNo>:<qualifier>`
and relies on this service replaying a stored provider answer under them.
Within the 24 h window nothing changes: the replay contract is byte for
byte what ADR 0005 decided (FINAL answers, successes and the stored decline
replay with `Idempotency-Replayed: true`). Past the window the key is free
and the same request re-executes — the fleet rule's desired behaviour: a
caller retrying a day later gets a fresh, correct answer (and for
authorize, a fresh hold) instead of a stale replay; a caller that must not
re-execute must keep its retries inside the window.

The pre-claim 400/404/409 answers (`malformed-request`,
`idempotency-key-required`/`-invalid`, `resource-not-found`) are untouched:
they are answered before the store and never had a row to expire. The
table above stands as the record of 2026-11-21; no Decision text is
rewritten.
