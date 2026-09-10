# Implementation Plan: Recipe Sync

**Branch**: `001-recipe-sync` | **Date**: 2026-09-10 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-recipe-sync/spec.md`

## Summary

Keep the recipe collections of two Mijn Eetmeter accounts in agreement, both
directions, without manual re-entry. A single Go service runs continuously: it
serves a small HTTP API and, on an in-process daily schedule, reconciles the two
accounts' recipes ("combined products" in the API). Reconciliation matches
recipes by name, detects per-side changes with a content fingerprint against a
stored baseline, propagates one-sided changes, flags two-sided changes for manual
resolution via HTTP, never deletes, and is crash-safe by being idempotent between
runs. State lives in PostgreSQL. Packaging is a Dockerfile; Kubernetes manifests
are out of scope (separate repo).

The one material unknown — how the API *creates and updates* a recipe — is
resolved by a real-account spike as the first task (see
[research.md](./research.md) §1, [contracts/eetmeter-api.md](./contracts/eetmeter-api.md)).

## Technical Context

**Language/Version**: Go 1.25+ (build with the 1.26.x toolchain)

**Primary Dependencies**: `github.com/jackc/pgx/v5` (PostgreSQL driver + pool) —
the **only** runtime dependency. Everything else is standard library: `net/http`
(incl. `ServeMux` method/wildcard routing, Go 1.22+), `crypto/sha256`, `time`,
`log/slog`, `embed`, `context`, `os`.

**Storage**: PostgreSQL. Hand-written SQL, no ORM. Schema via a tiny embedded
migration runner (numbered `.sql` files + `schema_migrations` table).

**Testing**: stdlib `testing` + `net/http/httptest`. Table-driven unit tests for
the reconciliation engine and the fingerprint; client tests replay spike
fixtures; store integration tests run only when `TEST_DATABASE_URL` is set.

**Target Platform**: Linux container in the maintainer's k3s cluster (Hetzner).
Distroless static image.

**Project Type**: Single Go module — a long-lived web service with an in-process
scheduler.

**Performance Goals**: Not performance-sensitive. A run handles ≤200 recipes with
sequential API calls, well under the 5-minute target (SC-001) and the 5-second
manual-trigger acknowledgement (SC-004, satisfied by returning `202` before the
run completes).

**Constraints**:
- One sync at a time (FR-014) — in-process mutex.
- No partial/corrupt state after a crash or abort (FR-018) — per-recipe-pair
  transactions, idempotent reconciliation, no run-spanning transaction.
- No password in any log line, HTTP response, or DB row (FR-020, SC-007).
- Never issue a delete that propagates a user's deletion (FR-010).

**Scale/Scope**: 2 accounts, low hundreds of recipes, 1 operator. ~1–2k LOC
expected.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

Constitution v1.0.0.

### I. Simplicity & YAGNI (KISS)

| Check | Status |
|-------|--------|
| Only current, stated requirements are built | PASS — scope is exactly the spec's four stories + their edges; explicit non-goals recorded (no >2 accounts, no real-time, no rename detection, no k8s manifests, no multi-user). |
| Fewest moving parts among options that satisfy the requirement | PASS — one binary, one dependency, in-process timer (vs. CronJob), stdlib router (vs. chi), raw SQL (vs. ORM), hand-rolled tz timer (vs. cron lib). Each alternative is named and rejected in `research.md`. |
| stdlib is the default; every third-party dep justified in its commit | PASS (planned) — `pgx/v5` is the sole dep; the commit that adds it will state: no PostgreSQL driver in stdlib, `pgx` is the maintained standard, `lib/pq` is frozen. |
| No abstraction/interface/knob without a present need | PASS — no repository interface, no plugin points. Config knobs are all backed by a concrete need (credentials, schedule time, tz, optional token, API base URL override for the spike). The one interface (Eetmeter client) exists because it is the seam tests replay fixtures through — a present need, not speculative. |
| Dead code deleted, not parked | PASS (discipline item for implementation). |

### II. Don't Assume

| Check | Status |
|-------|--------|
| Unclear requirements confirmed before building | PASS — spec-phase Q&A resolved sync direction, matching, conflict handling, deletion policy, account count; plan-phase Q&A resolved storage, scheduling, deploy scope, conflict UX. |
| External API behavior inspected against real data before parsing code | PASS (structurally) — READ shapes taken from a working client (Vreetmeter), not docs; WRITE shapes are explicitly marked UNVERIFIED and the **first task is a real-account spike** that produces fixtures. Client write code is not trusted until the spike lands. |
| Assumptions recorded where they live | PASS — spec has an Assumptions section; `contracts/eetmeter-api.md` marks each unverified endpoint; the client will carry `// UNVERIFIED until spike` comments at the exact call sites. |

### III. Pragmatic Testing

| Check | Status |
|-------|--------|
| Non-trivial logic has automated tests | PASS (planned) — reconciliation engine (all branches, table-driven) and fingerprint canonicalization are the core logic and are explicitly in scope for tests (`research.md` §12). |
| Every bug fix adds a failing-then-passing test | PASS (discipline item). |
| Trivial glue not over-tested | PASS — HTTP handlers get one happy-path + the 409 case; struct mapping untested; store gets a few integration tests, opt-in via env. |
| No test-first mandate, no coverage target imposed | PASS — none imposed. |

### IV. Idiomatic Go

| Check | Status |
|-------|--------|
| `gofmt` + `go vet` clean before commit | PASS (discipline item; add to CI later if wanted). |
| Standard project layout and naming | PASS — `cmd/eetmeter-sync/`, `internal/…`; no bespoke structure. |
| Clear over clever; exported identifiers documented | PASS (discipline item). |

**Gate result: PASS.** No violations; Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/001-recipe-sync/
├── plan.md              # this file
├── spec.md              # feature spec
├── research.md          # Phase 0 — decisions & the API spike definition
├── data-model.md        # Phase 1 — Postgres schema, run-summary JSON, state machine
├── quickstart.md        # Phase 1 — run + per-story validation guide
├── contracts/
│   ├── http-api.md      # this service's HTTP API + env config
│   └── eetmeter-api.md  # external Mijn Eetmeter API (confirmed vs. unverified)
└── checklists/
    └── requirements.md  # spec quality checklist (from /speckit-specify)
```

### Source Code (repository root)

```text
go.mod
go.sum
Dockerfile                          # multi-stage → distroless static image
.dockerignore

cmd/
└── eetmeter-sync/
    └── main.go                     # load config, open pool, run migrations,
                                    # build engine, start scheduler + HTTP server,
                                    # signal-based graceful shutdown

internal/
├── config/
│   └── config.go                   # env → Config struct; validation; no secrets in String()
├── eetmeter/                       # the external API client
│   ├── client.go                   # login, auth header, request plumbing, retries, timeouts
│   ├── recipes.go                  # List / Create / Update combined products
│   │                               #   (Create/Update carry `// UNVERIFIED until spike`)
│   ├── types.go                    # Recipe, Ingredient DTOs + wire (un)marshalling
│   └── testdata/                   # fixtures captured by the spike
├── fingerprint/
│   └── fingerprint.go              # canonical serialization + SHA-256 of portable content
├── syncengine/                     # the reconciliation logic (the heart)
│   ├── engine.go                   # Run(ctx, trigger): lock, auth, fetch, reconcile loop,
│   │                               # summary assembly, single-flight (409/skip)
│   ├── reconcile.go                # pure decision function: (link, aRecipe, bRecipe,
│   │                               # pendingResolution) -> action(s)
│   └── reconcile_test.go           # table-driven: every branch + first-sync + edges
├── store/
│   ├── store.go                    # pgxpool wrapper; queries for account, recipe_link,
│   │                               # sync_run, conflict_resolution
│   ├── migrate.go                  # embedded numbered .sql runner
│   ├── migrations/
│   │   └── 0001_init.sql
│   └── store_test.go               # opt-in via TEST_DATABASE_URL
├── httpapi/
│   ├── server.go                   # ServeMux routes, bearer middleware, JSON helpers
│   ├── handlers.go                 # /healthz /sync /sync/last /conflicts /conflicts/{id}/resolve
│   └── handlers_test.go            # happy path per route + 409 concurrent
└── scheduler/
    └── scheduler.go                # next-occurrence-of HH:MM in tz; timer; calls engine.Run
```

**Structure Decision**: Single Go module, standard `cmd/` + `internal/` layout.
Packages are split along the natural seams — external API client, pure
fingerprint, pure reconcile decision + engine orchestration, persistence, HTTP,
scheduling — each small and independently testable. This is the minimum division
that keeps the branchy reconcile logic isolated as a pure function with its own
exhaustive test table; collapsing further would entangle it with HTTP and DB I/O
and make that testing harder, which Principle III wants easy.

## Complexity Tracking

No constitution violations. Nothing to justify.

## Phase sequence for implementation (feeds `/speckit-tasks`)

1. **API spike** (`research.md` §1) — confirm create/update, capture fixtures,
   update `contracts/eetmeter-api.md`. Blocks trustworthy client write code.
2. **Module skeleton** — `go.mod`, config, `slog` setup, `main.go` wiring stubs.
3. **Store** — migrations + `store.go` + opt-in integration tests.
4. **Eetmeter client** — types, list (fixture-tested), login; then create/update
   per spike results.
5. **Fingerprint** — canonicalization + tests.
6. **Reconcile** — pure decision function + the full test table.
7. **Engine** — orchestration, single-flight, summary, read-back-after-write.
8. **HTTP API** — routes, bearer middleware, handlers + tests.
9. **Scheduler** — daily timer wired to the engine.
10. **Dockerfile** + `.dockerignore`; `quickstart.md` walkthrough end to end.
11. **Manual story validation** against two real accounts (`quickstart.md`).
