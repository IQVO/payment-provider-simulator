# CLAUDE.md — payment-provider-simulator

`payment-provider-simulator` is the **Payment Provider Simulator** bounded
context (Generic, `storefront` tier) of the retail-network organisation's
storefront domain (GitHub org `IQVO`, Go module
`github.com/IQVO/payment-provider-simulator`). It is the Go stand-in for the
external payment provider, on its own Postgres: tokenize (hosted-fields
stand-in), authorize, capture, void, refund, re-authorize and reduce, with a
deterministic outcome per test token. Its HTTP contract is the one
customer-payments' anti-corruption layer calls at `PAYMENT_PROVIDER_URL`
(customer-payments ADR 0002, assumption A12); swapping it for a real PSP must
change nothing in the storefront. It owns the provider-side
`ProviderAuthorization` (the hold, with its captures and refunds) and the
token/scenario vocabulary. It ships ONE binary, `cmd/api` (REST `:8080`). No
MCP, no A2A, no `web/`; its Docusaurus docs site lives in `docs/`.

## Hard rules (each one cost an incident or is fitness-tested)

1. **Hexagonal dependency rule.** `internal/domain` depends on nothing internal
   but domain; `internal/application` only on domain and application; adapters
   never depend on each other; only `cmd/` wires layers. `make arch-test`
   (`TestHexagonalArchitecture`) enforces it.
2. **No events, no Kafka at runtime** (ADR 0002 A16). The simulator publishes
   and consumes nothing; `apis/asyncapi.yaml` declares no channel and
   `KAFKA_BROKERS` is not read. The harness's
   `internal/adapters/kafka/cloudevents` and `internal/adapters/outbound/kafka`
   packages stay in the tree unused because the fitness tests expect them.
   Adding an event needs an ADR first, and then CloudEvents 1.0 applies in
   full (`.claude/rules/fleet/cloudevents.md`; namespace `retailnetwork`).
3. **Outcomes are deterministic per token.** A token `tok_<scenario>[_<suffix>]`
   names its Scenario: `approve` (7-day hold), `decline` (402
   `payment-declined`, `code: CARD_DECLINED`), `timeout` (504 after
   `PROVIDER_TIMEOUT_DELAY`, at most 2 s, no hold), `expire-early` (5-minute
   hold). Never add randomness; tests in other repos depend on it.
4. **Only tokens travel, never a card number.** A 13-19 digit string is
   refused (`internal/domain/providertoken`). Do not add a field that could
   carry PAN data.
5. **The hold is the consistency boundary** (`internal/domain/providerauth`):
   captures never exceed the remaining hold, refunds never exceed the captured
   total, no capture after expiry; only AUTHORIZED accepts hold changes;
   Reduce only lowers the remaining hold; Reauthorize supersedes the old hold
   with a new id and expiry. Hold changes run under the hold's row lock.
6. **Every POST is behind the `Idempotency-Key` middleware** (ADR 0004): the
   key is global, a replay returns the stored answer with
   `Idempotency-Replayed: true`, the same key with another method, path or
   body is 409 `idempotency-key-reused`. Errors are RFC 9457 problems
   `/problems/<slug>`; a 4xx decline is final, 5xx/504 are transient.
7. **Real Postgres only.** `cmd/api` refuses to boot without `DATABASE_URL`;
   the memory adapters are test doubles
   (`TestRuntimeBinaryHasNoInMemoryAdapters`); Postgres integration tests use
   testcontainers only (`TestPostgresIntegrationTestsUseTestcontainers`).
8. **No auth layer** (fleet-wide revert 2026-09-11):
   `TestNoAuthMiddlewareReintroduced`.
9. **The contract is customer-payments'.** A change to `apis/openapi.yaml`
   (routes, status codes, problem slugs) breaks its ACL adapter: change both
   sides together, contract first.

## Commands

```bash
make check-fast   # fmt-check + vet + arch-test + tests of changed packages: run before saying "done"
make check        # fmt-check vet build lint test
make check-all    # check + coverage (90% gate on domain+application) + arch-test + bdd
make integration  # real Postgres via testcontainers (needs Docker)
make mutation     # gremlins on internal/domain/providerauth (CI job mutation-fast, blocking)
make vuln         # govulncheck ./...
make guide-lint   # these guides: skills load, references resolve, context budget
```

Run locally: `DATABASE_URL=... go run ./cmd/api` (REST `:8080`; migrations run
at boot; `/readyz` gates on a Postgres ping and the drain gate).

## Where things are written down (read before editing the matching area)

- `.claude/rules/domain-model.md`: ubiquitous language (hold, token,
  scenario, movement, reduce, reauthorize), the aggregate, use cases (always
  loaded). `.claude/rules/rest-api.md`: routes and error slugs.
  `.claude/rules/integration-events.md`: why nothing is published.
- `.claude/rules/fleet/*.md`: fleet-wide rules copied from the harness
  template; never edit them here.
- `docs/adr/`: decisions (0001 bounded context, 0002 assumptions incl. the
  contract and the no-events decision, 0003 observability, 0004 REST
  guideline). `apis/openapi.yaml`: the contract (`api-lint` runs the Spectral
  `rest-guideline` ruleset from `.spectral.yaml`).
- `docs/`: the Docusaurus site (published to GitHub Pages from `main`). Pages
  in `docs/docs/` (runbook `docs/docs/operations/runbook.md`, packaging
  checklist `docs/docs/operations/before-first-release.md`); the REST reference
  `docs/docs/api-reference/rest/` is GENERATED from `apis/openapi.yaml`: after a
  spec change run `npm run clean-api-docs && npm run gen-api-docs` in `docs/`
  and commit it, or the `docs-api-drift` job fails. `docs/operations/runbook.md`
  is only a pointer kept for the fleet alert links.
- `HARNESS.md`: what each sensor runs.
- `charts/payment-provider-simulator/`: the Helm chart.

harness-template: v4
(hooks, guide-lint).

<!-- harness:scoped-rules:start (generated by tools/migrate_v3.py in warehouse-harness-template; do not hand-edit) -->
## Scoped rules and harness

Claude Code loads each rule below automatically when you touch the matching paths. OpenCode and Codex do NOT: read the rule BEFORE editing matching files.

| When touching | Read |
|---|---|
| `internal/adapters/**/kafka/**`, `internal/adapters/outbound/events/**`, `apis/asyncapi*` | `.claude/rules/integration-events.md` |
| `internal/adapters/inbound/http/**`, `apis/openapi*.yaml`, `apis/openapi/**` | `.claude/rules/rest-api.md` |

Hooks (`scripts/harness/hook.py`, wired for Claude Code, Codex and OpenCode) block pushes to develop/main, `--no-verify`, bare `rm -rf`, and edits to generated files, and feed gofmt/vet findings back after each edit. Before saying "done" run `make check-fast`; the full gate is `make check-all`. `HARNESS_OFF=1` disables the hooks when debugging the harness itself.
<!-- harness:scoped-rules:end -->
