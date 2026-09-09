# Implementation Plan: Account Configuration

**Branch**: `001-account-configuration` | **Date**: 2026-09-09 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-account-configuration/spec.md`

## Summary

Establish the base of eetmeter-sync: hold one or more Mijn Eetmeter accounts for a
single operator, hold each account's secret only as in-memory ciphertext under an
out-of-band key (never persisted — configuration is re-read from the start-up
source every boot), and verify each account against the Eetmeter private JSON API
in two steps (authenticate, then one lightweight authenticated read-back). Configuration arrives either
declaratively at start-up (config file + environment) or through the HTTP/JSON
API at runtime; start-up configuration is the durable source of truth and runtime
changes are session-only. Verification is event-driven only — on configure, on
secret change, on operator-triggered re-check, and when Eetmeter refuses a live
session — with no background scheduler and no automatic retry loop. This feature
also stands up the project skeleton the rest of the product builds on: the
single-purpose `internal/` packages, the HTTP interface, the Postgres-backed
state store with embedded SQL migrations, a `docker-compose.yml` that provides
Postgres for local development and tests, the Dockerfile, and CI.

**Eetmeter contract note (constitution → Reference Material):** the reference
client exposes no `account`/`profile` GET. The concrete read-back used for
verification is `GET userfavorite` — the call the reference app itself issues
immediately after login to confirm a session works. FR-007 now defers the exact
endpoint to the recorded fixtures, and those fixtures are authoritative; see
[research.md](./research.md) Decision 2.

## Technical Context

**Language/Version**: Go 1.26 (pinned in `go.mod`).

**Primary Dependencies**: Standard library for everything except Postgres access —
`net/http` (server and Eetmeter client), `encoding/json`, `log/slog`,
`crypto/aes` + `crypto/cipher` (AES-256-GCM), `crypto/rand`, `crypto/sha1` for a
~40-line internal UUID v4/v5 helper, `embed` for bundled SQL migrations. One
third-party module: **`github.com/jackc/pgx/v5`** (`pgxpool`) for Postgres —
pure Go, no CGo, the de-facto standard driver, already the choice in the sibling
`../eetmeter-share`. Migrations are applied by a ~50-line in-repo runner over
`embed.FS`, so no migration-tool dependency is added. Any further module must be
justified in its PR per the constitution.

**Storage**: PostgreSQL (single database) reached via `pgxpool`, configured by
`EETMETER_SYNC_DATABASE_URL`. A `docker-compose.yml` at the repo root provides a
Postgres 16 service for local development and integration tests; production points
the same URL at a managed instance. Schema is created and evolved by embedded
`db/migrations/NNNN_*.sql` files applied automatically at start-up under a
`pg_advisory_lock`. This feature's schema is small: `schema_migrations` plus
`account_status` (per operator + label: status, cause category, timestamps,
detail). Secrets are **not** stored — configuration is authoritative from the
start-up source on every boot (spec Q4), so the database holds only verification
status that must outlive a restart (FR-016). Full schema in
[data-model.md](./data-model.md).

**Testing**: Go `testing` + `net/http/httptest`. Eetmeter interactions tested
against scrubbed recorded JSON fixtures under `internal/eetmeter/testdata/`.
`internal/store` tests run against a real Postgres from `docker-compose.yml`,
gated by `EETMETER_SYNC_TEST_DATABASE_URL` (the test `t.Skip`s when it is unset so
`go test ./...` still passes without a database); CI sets it via a Postgres
service container. `go test ./... -race` in CI.

**Target Platform**: Linux container (distroless, non-root) for delivery; builds
and runs on macOS/Linux for development.

**Project Type**: Single Go web service. One binary (`cmd/eetmeter-sync`),
importable core packages under `internal/`, HTTP/JSON as the only external
interface.

**Performance Goals**: Not throughput-bound. Each configured account reaches a
definitive status within 30 s of configuration or start-up (SC-002); a single
verification completes in a few seconds; a fresh deployment reaches "all accounts
connected" within 5 minutes (SC-001).

**Constraints**: No account secret in any response, log, error, metric, or
fixture (SC-003, FR-006). TLS required for every non-loopback bind; plain HTTP
only for a loopback dev bind. Every endpoint except the health check requires
authentication and auth attempts are rate-limited. Stateless process; all state
in Postgres; configuration from the environment; graceful `SIGTERM` (drain HTTP,
close the pool).
Outbound Eetmeter traffic: per-account serialized with a configurable minimum
interval, a per-account cap on verification triggers per rolling window (default
6 / 10m, SC-007), bounded in-attempt exponential backoff + jitter with a capped
attempt count, a global outbound concurrency cap, only `version` / `platform`
headers plus an identifying `User-Agent`, and token discard + re-auth on 401/403.

**Scale/Scope**: One operator, two Eetmeter accounts initially; data model and
store keys are operator-scoped so additional operators are a configuration change,
not a rewrite. Realistic ceiling in the tens of accounts.

## Constitution Check

*GATE: evaluated against `.specify/memory/constitution.md` v1.0.0 before Phase 0
and re-checked after Phase 1. No violations — Complexity Tracking is empty.*

| Principle / Rule | Gate | How this plan satisfies it |
|---|---|---|
| I. Library-First Core, API-Only Interface | PASS | All logic in single-purpose `internal/` packages runnable without a server; `internal/httpapi` is the only external interface; `cmd/eetmeter-sync` only wires. No scheduler/UI/CLI in this feature, so no bypass path exists to introduce. Every package has a one-purpose doc comment; no grouping-only packages. |
| II. Test-First for Core Logic and External Contracts (NON-NEGOTIABLE) | PASS (by plan) | `/speckit-tasks` will emit a failing-test task before implementation for every `internal/` package and for each Eetmeter contract (`POST account/credentials` success + 401; `GET userfavorite` success + 401 + 5xx/network). Eetmeter decoding tolerates unknown fields and fails loudly on a missing/type-mismatched depended-on field. |
| III. Non-Destructive, Idempotent Sync | N/A | No sync in this feature. Account removal is an explicit, operator-initiated action, never an automatic propagation. Re-verifying an unchanged account performs reads only and writes status idempotently. |
| IV. Deterministic Recipe Identity | N/A | No recipes in this feature. (Device identifier is derived deterministically as UUID v5 over operator + login, so it is stable without persistence — consistent with the constitution's determinism preference.) |
| V. Credential and Secret Protection | PASS | Secrets are never persisted — configuration is re-read from the authoritative start-up source every boot (spec Q4), so no at-rest secret copy exists in Postgres to protect. In memory the secret is held as AES-256-GCM ciphertext (`internal/secret`; key only from `EETMETER_SYNC_CRED_KEY` env / future KMS, never written to the database or repo) and decrypted only at the moment of an Eetmeter auth call. Logging passes through a redaction step; DTOs never carry the secret; fixtures are scrubbed. Every `account_status` row is keyed by `operator_id`. Missing/invalid key → refuse to start, no destructive write. |
| VI. Good Citizen to the Eetmeter API | PASS | `internal/eetmeter` serializes requests per account, enforces a configurable minimum interval, caps verification triggers per account to 6 per rolling 10 minutes (SC-007), retries only with bounded exponential backoff + jitter and a capped attempt count *within a single verification* (there is no scheduled re-verification), bounds global outbound concurrency, sends only `version` / `platform` + an identifying `User-Agent`, and discards the token + re-authenticates on 401/403. |
| Security & Deployment Constraints | PASS | TLS for non-loopback; all endpoints except `/healthz` require a bearer token mapped to an operator, with auth-attempt rate limiting; no default/hard-coded credentials (empty token config → refuse to start unless an explicit loopback-dev bind); secrets only from env; `.gitignore` excludes the config file, `.env`, and local compose overrides; multi-stage distroless non-root `Dockerfile`, image build in CI; stateless process (all state in Postgres), env config, graceful `SIGTERM`; structured `log/slog` at warning default through a redaction step; the state store is Postgres with schema defined by in-repo `db/migrations/*.sql` and documented in data-model.md; `gofmt`/`go vet` clean; CI runs `go test ./... -race` (with a Postgres service container) and a secret scanner. |
| Configuration precedence (explicit > env > file > default) | PASS | Runtime API change wins within a session; at boot the order is environment > config file > built-in default; the effective source is reportable per account (FR-004). |
| Dependencies (stdlib default, justify each) | PASS (1 module) | `github.com/jackc/pgx/v5` is the only third-party module: Postgres is the mandated external state store, pgx is pure-Go (no CGo), the de-facto standard driver, and already the sibling project's choice; the lighter `database/sql` + `lib/pq` was rejected because pgx's native interface and `pgxpool` are better fits and `lib/pq` is in maintenance mode. Migrations use an in-repo `embed.FS` runner rather than adding `golang-migrate`. Any further module is justified in its PR. |
| Commits & PRs (Conventional Commits, trailers) | PASS | Enforced at commit time; each commit builds and passes tests on its own; `main` stays green. |

## Project Structure

### Documentation (this feature)

```text
specs/001-account-configuration/
├── plan.md              # This file
├── research.md          # Phase 0 output — decisions and rationale
├── data-model.md        # Phase 1 output — entities, state machine, store & config schemas
├── quickstart.md        # Phase 1 output — run & validation guide
├── contracts/
│   └── openapi.yaml     # Phase 1 output — HTTP/JSON interface contract
└── tasks.md             # /speckit-tasks output (NOT created here)
```

### Source Code (repository root)

```text
cmd/
└── eetmeter-sync/
    └── main.go                  # wiring only: load config, build store/secret/eetmeter/service, start HTTP server, handle SIGTERM

internal/
├── account/                     # Account entity + value objects (Label, Login, DeviceID, Status, FailureCategory); operator-scoped set; label/login uniqueness and field validation — pure, no I/O
│   ├── account.go
│   ├── rules.go
│   └── account_test.go
├── verification/                # the verify use case: 2-step (authenticate → read-back), outcome classification (connected | credentials_rejected | temporarily_unavailable); depends on an EetmeterPort interface it declares — pure given the port
│   ├── verify.go
│   └── verify_test.go
├── eetmeter/                    # Eetmeter private-JSON-API client: auth + userfavorite read-back; typed request/response contracts; per-account serialization, min-interval, bounded backoff+jitter, global concurrency cap, version/platform headers + User-Agent, token discard + re-auth on 401/403
│   ├── client.go
│   ├── contracts.go
│   ├── throttle.go
│   ├── client_test.go
│   └── testdata/               # scrubbed recorded JSON fixtures + golden HTTP exchanges
├── config/                      # start-up configuration: parse config file + environment, apply precedence, validate, produce the initial account set
│   ├── config.go
│   └── config_test.go
├── secret/                      # AES-256-GCM encrypt/decrypt with the out-of-band key; slog redaction helper
│   ├── secret.go
│   └── secret_test.go
├── store/                       # persistence port + Postgres (pgxpool) implementation for verification status/timestamps, operator-scoped; embedded-SQL migration runner (pg_advisory_lock)
│   ├── store.go                 # the Store interface + domain types
│   ├── postgres.go              # pgxpool implementation
│   ├── migrate.go               # embed.FS migration runner
│   └── postgres_test.go         # runs against docker-compose Postgres; skips without EETMETER_SYNC_TEST_DATABASE_URL
├── accountservice/              # application service — the single audited entry point the HTTP layer calls: LoadFromConfig, Configure, Update, Remove, List, ReverifyOne, ReverifyAll; ties config + store + secret + verification + account set together
│   ├── service.go
│   └── service_test.go
├── httpapi/                     # HTTP/JSON transport: router, bearer-auth + auth-rate-limit middleware, handlers, request/response DTOs (never carry a secret), problem responses
│   ├── router.go
│   ├── handlers.go
│   ├── auth.go
│   ├── dto.go
│   └── handlers_test.go
└── uuid/                        # ~40-line UUID v4 (random) and v5 (namespaced) helper, to avoid a third-party dependency
    ├── uuid.go
    └── uuid_test.go

db/
└── migrations/
    └── 0001_account_status.sql  # schema_migrations + account_status; embedded via //go:embed in internal/store

docker-compose.yml              # Postgres 16 for local dev + integration tests (named volume, healthcheck)
Dockerfile                      # multi-stage, distroless, non-root
.dockerignore
.github/workflows/ci.yml        # gofmt + go vet + go test ./... -race (Postgres service container) + secret scan + docker build
.gitignore                      # add: config file, .env, docker-compose.override.yml
.env.example                    # documents every EETMETER_SYNC_* variable; real .env is git-ignored
```

**Structure Decision**: Single Go web service. The core is a set of
single-purpose packages under `internal/`; `internal/accountservice` is the one
place operations are composed and is the only thing `internal/httpapi` calls;
`cmd/eetmeter-sync/main.go` is wiring only. This keeps the daily-sync scheduler
and any future UI/CLI (later features) as clients of the same service surface with
no privileged path, per Principle I.

## Complexity Tracking

> No Constitution Check violations. Table intentionally empty.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |
