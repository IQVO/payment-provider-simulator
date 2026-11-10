---
id: testing
title: Testing
sidebar_label: Testing
sidebar_position: 1
---

# Testing

The test pyramid as it is on this branch, the `make` targets that run each
layer, and the CI jobs that enforce them. Counts were taken from the source
tree (`func Test...` per file, `TestMain` excluded; `Scenario:` per feature
file).

## The pyramid

| Layer | What it covers | Where | Count | Run with |
| --- | --- | --- | --- | --- |
| Unit: domain | the `ProviderAuthorization` hold (capture, refund, void, reduce, reauthorize, expiry), tokens and scenarios, shared rules | `internal/domain/*/` | 22 tests (15 `providerauth`, 6 `providertoken`, 1 `shared`) | `make test` |
| Unit: use cases | every command and query over in-memory doubles | `internal/application/usecases/` | 12 tests | `make test` |
| Unit: adapters and boot | HTTP handlers and routes (httptest over the real chi router), the Idempotency-Key middleware, readiness, the request log, telemetry, the harness CloudEvents and Kafka packages, clock and ids, the bounded boot retry, `cmd/api` configuration | `internal/adapters/**`, `internal/bootretry`, `cmd/api/*_test.go` | 53 tests | `make test` |
| Behaviour (godog / Gherkin) | acceptance scenarios through the real router and use cases, in-memory doubles | `features/*.feature`, runner `features/features_test.go` (`TestFeatures`) | 3 feature files, 15 scenarios (`tokens_and_authorizations` 7, `holds` 6, `expiry` 2) | `make bdd` |
| Contract: repositories | one suite every repository implementation must pass, run against the memory doubles and against real Postgres | `internal/adapters/outbound/repocontract/` | shared suite | `make test`, `make integration` |
| Integration | real Postgres started by testcontainers: the repository contract, migrations up/down/up, idempotency over an aborted transaction and a stored decline, the real `cmd/api` binary serving the provider contract | files tagged `//go:build integration` (4 files) | 5 tests | `make integration` |
| Mutation | gremlins over `internal/domain` | `.gremlins.yaml` | thresholds: efficacy 95, mutant coverage 80 (measured: 52 killed, 0 lived, 9 not covered; efficacy 100 %, mutator coverage 85.25 %) | `make mutation` (`providerauth`, blocking in CI), `make mutation-full` |
| Architecture fitness | structure and policy as executable tests | `internal/architecture/` | 28 tests | `make arch-test` |

Totals: 116 unit-tagged and 5 integration-tagged test functions plus the 15
godog scenarios. Integration tests never read `DATABASE_URL` and never `t.Skip`
on it: each boots its own container
(`TestPostgresIntegrationTestsUseTestcontainers`), so they need only Docker.

## Fitness tests (`internal/architecture/`)

| Test | Enforces |
| --- | --- |
| `TestHexagonalArchitecture` | the domain imports no outer layer; adapters do not reach sideways into siblings |
| `TestRuntimeBinaryHasNoInMemoryAdapters` | no `cmd/**` imports `internal/adapters/outbound/memory` |
| `TestNoGoFilesAtRepoRoot` | Go files live under `cmd/`, `internal/`, `features/` |
| `TestNoAuthMiddlewareReintroduced` | no bearer / JWT check under `internal/adapters/inbound` (auth was removed fleet-wide) |
| `TestPostgresIntegrationTestsUseTestcontainers`, `TestKafkaIntegrationTestsUseTestcontainers` | integration tests start their own containers |
| `TestCloudEventsOnly`, `TestNoEventEnvelopeToggleOrFlatEnvelope`, `TestEventCatalogueMatchesContract` | the fleet event rules; they hold trivially while nothing is published |
| `TestKafkaWritersSetRequiredAcks`, `TestKafkaWriterTopicNotSetTwice`, `TestKafkaConsumerGroupNeverHardcodedInline`, `TestReplayConsumersSetCommitInterval`, `TestDeadLetterCallsGuardCancellation`, `TestOutboxRelaysPassAttemptCap` | fleet Kafka and outbox rules, kept so an event added later starts compliant |
| `TestMCPAdapterDependencyRule`, `TestMCPOptionalToolInputsCarryOmitempty` | the MCP rules; they skip while there is no MCP adapter |

Most carry a companion `...SensorBites` / `...FailsOnBadFixtures` test proving
the sensor fails on a bad fixture.

## `make` targets

| Target | Runs | In `check` / `check-all`? |
| --- | --- | --- |
| `make build` | `go build ./...` | check |
| `make vet` | `go vet ./...` | check |
| `make fmt` / `make fmt-check` | `gofmt -w .` / fail if `gofmt -l .` lists files | `fmt-check` in check |
| `make lint` | `golangci-lint run --allow-parallel-runners ./...` (v2.14.0; config `.golangci.yml`) | check |
| `make test` | `go test ./... -race` (unit + httptest + godog) | check |
| `make coverage` | `go test ./... -race -coverprofile=coverage.out -coverpkg=./internal/domain/...,./internal/application/...`, then fails below 90 % | check-all |
| `make arch-test` | `go test ./internal/architecture/... -v` | check-all |
| `make bdd` | `go test ./... -run TestFeatures -v` | check-all |
| `make integration` | `go test -tags=integration ./... -race -count=1` (Docker needed) | neither (slow) |
| `make mutation` / `make mutation-full` | gremlins on `./internal/domain/providerauth` / on `./internal/domain` | neither |
| `make vuln` | `govulncheck ./...` | neither |
| `make check-fast` | `fmt-check vet arch-test` plus the tests of changed packages (the agent Stop hook) | neither |
| `make guide-lint` | `python3 scripts/harness/guide_lint.py` | neither |
| `make check` | `fmt-check vet build lint test` | the pre-push gate (lefthook) |
| `make check-all` | `check coverage arch-test bdd` | run before pushing |

## CI (`.github/workflows/ci.yml`)

| Job | Runs | When |
| --- | --- | --- |
| `lint`, `complexity`, `file-length` | golangci-lint; the complexity linters (gocyclo 15, gocognit 20, cyclop 15, funlen 80, nestif); revive's 300-line file budget (blocking for touched files on PRs into develop) | push, PR |
| `guide-lint` | `scripts/harness/guide_lint.py`, `scripts/harness/repo_lint.py` and the harness self-tests; advisory (`continue-on-error`) | push, PR |
| `test` | `make coverage` (90 % gate) | push, PR |
| `bdd`, `arch-test`, `integration` | the godog suite, the fitness tests, the testcontainers suite | push, PR |
| `mutation-fast` / `mutation` | gremlins on `providerauth` (blocking) / on `internal/domain` (weekly schedule, opens a `harness:red` issue when red) | push, PR / schedule |
| `drift` | deadcode, `go mod tidy -diff`, coverage quality | weekly schedule |
| `api-lint` | Spectral on `apis/openapi.yaml` (`rest-guideline` ruleset) and `apis/asyncapi.yaml` | push, PR |
| `docs-api-drift` | regenerates `docs/docs/api-reference/rest` from `apis/openapi.yaml` and fails on any diff | push, PR |
| `vuln` | `govulncheck ./...` | push, PR |
| `helm-lint`, `chart-tests` | `ct lint` and the chart render tests | push, PR |
| `trivy-scan` | image build and Trivy scan | PR |
| `docker-publish`, `release` | image to GHCR, chart release | push to `main` |

`docs.yml` builds this site on every pull request touching `docs/`, `apis/` or
the workflow and deploys GitHub Pages only from `main`. CodeQL runs from
`codeql.yml`.

When `apis/openapi.yaml` changes, regenerate the REST reference in `docs/` and
commit it, or `docs-api-drift` fails:

```sh
cd docs
npm ci
npm run clean-api-docs && npm run gen-api-docs
npm run build
```
