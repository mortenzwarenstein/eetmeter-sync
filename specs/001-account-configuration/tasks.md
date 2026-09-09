---
description: "Task list for feature 001 — Account Configuration"
---

# Tasks: Account Configuration

**Input**: Design documents in `/specs/001-account-configuration/`
**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/openapi.yaml](./contracts/openapi.yaml)

**Tests**: **REQUIRED — not optional.** Constitution Principle II (Test-First, NON-NEGOTIABLE)
and `CLAUDE.md` require a failing test before implementation for every package under
`internal/` and for every Eetmeter API contract. Eetmeter contracts are tested against
scrubbed recorded JSON fixtures.

**Organization**: Phase 1 Setup → Phase 2 Foundational (blocks all stories) → one phase
per user story in priority order → Polish.

## Format: `[ID] [P?] [Story?] Description with file path`

- **[P]**: may run in parallel — different files, no dependency on an unfinished task
- **[Story]**: `[US1]`–`[US4]`, on user-story tasks only

## Path conventions

Single Go service (see plan.md § Project Structure): binary in `cmd/eetmeter-sync/`,
importable packages in `internal/<pkg>/`, SQL in `db/migrations/`, ops files at repo root.

---

## Phase 1: Setup (shared infrastructure)

**Purpose**: repository skeleton, toolchain, local Postgres, CI.

- [ ] T001 Create the source tree — `cmd/eetmeter-sync/`, `internal/{uuid,secret,account,eetmeter,verification,config,store,accountservice,httpapi}/`, `internal/eetmeter/testdata/`, `db/migrations/` — and put a `doc.go` in each `internal/` package with a one-sentence single-purpose package comment per Constitution I
- [ ] T002 Add `github.com/jackc/pgx/v5` to `go.mod`, run `go mod tidy`, and confirm `go build ./...` succeeds on the empty skeleton
- [ ] T003 [P] Add `.gitignore` entries (`.env`, `*.accounts.json`, `docker-compose.override.yml`, `/eetmeter-sync`, coverage output) and create `.env.example` documenting every `EETMETER_SYNC_*` variable from data-model.md §4
- [ ] T004 [P] Add `docker-compose.yml` at repo root: a Postgres 16 service (db `eetmeter`, user `eetmeter`, password `eetmeter`, port 5432), a named volume, and a `pg_isready` healthcheck
- [ ] T005 [P] Add `.github/workflows/ci.yml`: `gofmt -l` gate, `go vet ./...`, `go test ./... -race` with a Postgres service container exporting `EETMETER_SYNC_TEST_DATABASE_URL`, a secret-scanner step (gitleaks), and a `docker build` step
- [ ] T006 [P] Add a multi-stage `Dockerfile` (build on `golang:1.26`, run on a distroless static non-root base, `USER nonroot`, entrypoint = the binary) and `.dockerignore`

---

## Phase 2: Foundational (blocking prerequisites)

**Purpose**: the packages and the running, authenticated, DB-backed service skeleton that
every user story builds on. **No user-story phase may start until this phase is done.**

- [ ] T007 [P] Write failing unit tests in `internal/uuid/uuid_test.go`: v4 sets version/variant bits and varies; v5 is deterministic for the same namespace+name and differs for a different name; `String()` is RFC-4122 formatted; `Parse` round-trips
- [ ] T008 [P] Implement `internal/uuid/uuid.go` — `NewV4()`, `NewV5(ns UUID, name []byte)`, `Parse`, `String()` using `crypto/rand` and `crypto/sha1`; T007 passes
- [ ] T009 [P] Write failing unit tests in `internal/secret/secret_test.go`: encrypt→decrypt round-trips; a wrong key fails the GCM auth; stored form is `keyVersion(1) || nonce(12) || ciphertext+tag`, base64; the redaction `slog.LogValuer` never emits plaintext; a non-32-byte key is rejected
- [ ] T010 [P] Implement `internal/secret/secret.go` — `Cipher` built from the base64 `EETMETER_SYNC_CRED_KEY`, `Encrypt`/`Decrypt` (AES-256-GCM, random 12-byte nonce, 1-byte key version), and a redaction helper implementing `slog.LogValuer`; T009 passes
- [ ] T011 [P] Write failing unit tests in `internal/account/account_test.go`: label charset/length rules; login trim + lowercase normalization; deviceId derivation is `UUIDv5(projectNS, operatorID + "\x00" + login)` and stable; `Set.Add` rejects a duplicate label and a duplicate login; `failureCategory` is nil exactly when status is `connected` or `never_verified`
- [ ] T012 [P] Implement `internal/account/{account.go,rules.go}` — `Account`, value types (`Label`, `Login`, `DeviceID`, `Status`, `FailureCategory`), an operator-scoped `Set` with `Add`/`Update`/`Remove`/`Rename` enforcing label+login uniqueness, `Validate`, and deviceId derivation via `internal/uuid`; T011 passes (depends on T008)
- [ ] T013 [P] Add scrubbed Eetmeter fixtures in `internal/eetmeter/testdata/`: `auth_success.json`, `auth_invalid_credentials.json` (401), `userfavorite_success.json`, `userfavorite_unauthorized.json` (401), `userfavorite_server_error.json` (503) — no real tokens, e-mail, or personal data
- [ ] T014 [P] Write failing contract tests in `internal/eetmeter/client_test.go` (httptest + the T013 fixtures): `POST account/credentials` success → token parsed and the presented credential is `token:deviceId`; only `version`, `platform`, `User-Agent` (and `Authorization` when held) are sent; 401 → typed auth-rejected error; `GET userfavorite` 200 → ok; `GET userfavorite` 401 → exactly one re-auth then a typed error; 503 → typed transient error after bounded retries; decoding tolerates unknown fields but fails loudly when `token` is absent
- [ ] T015 Implement `internal/eetmeter/{contracts.go,client.go,throttle.go}` — typed request/response contracts, `Authenticate` and `GetUserFavorites`, per-account serialization + configurable min-interval, a global outbound concurrency semaphore, bounded exponential backoff + full jitter with a capped attempt count for transient failures only, token discard + single re-auth on 401/403, and the minimal header set; T014 passes (depends on T013)
- [ ] T015a [P] Write failing unit tests in `internal/eetmeter/throttle_test.go`: the per-account verification-trigger limiter admits `VERIFY_BURST` (6) triggers then rejects the 7th within `VERIFY_WINDOW` with a typed `ErrVerifyBudgetExhausted`; the budget refills as the window slides; separate accounts have independent budgets; the min-interval gate and the burst limiter are enforced independently
- [ ] T015b Implement the per-account verification-trigger limiter in `internal/eetmeter/throttle.go` — rolling-window counter keyed by `(operatorId, label)`, `VERIFY_BURST` / `VERIFY_WINDOW` from config (defaults 6 / 10m), exposed as `TryReserveVerify(operatorID, label) error` returning `ErrVerifyBudgetExhausted`; T015a passes (depends on T015)
- [ ] T016 [P] Write failing unit tests in `internal/verification/verify_test.go` with a fake `EetmeterPort`: both steps ok → `connected` + `authenticatedAt`; auth 401 → `credentials_rejected`; read-back 401 → `credentials_rejected`; timeout / 5xx / 429 / other 4xx → `temporarily_unavailable` + `service_unavailable`; `checkedAt` always set; `detail` carries no secret/token/personal data
- [ ] T017 [P] Implement `internal/verification/verify.go` — declares the `EetmeterPort` interface and `Verify(ctx, cred) VerificationResult` doing the two-step check and the classification from research D3; T016 passes
- [ ] T018 [P] Write failing tests in `internal/store/postgres_test.go`, gated by `EETMETER_SYNC_TEST_DATABASE_URL` (`t.Skip` when unset): the migration runner creates `schema_migrations` + `account_status` and is idempotent on a second call; concurrent `Migrate` calls serialize via `pg_advisory_lock` with no duplicate-version error; `Upsert` then `ListByOperator` round-trips status, category, both timestamps and `detail`; `Delete` removes only the target row; `status` / `failure_category` CHECK violations are rejected
- [ ] T019 [P] Add `db/migrations/0001_account_status.sql` per data-model.md §3 — `schema_migrations`, and `account_status` with PK `(operator_id, label)` and the `status` / `failure_category` CHECK constraints
- [ ] T020 Implement `internal/store/{store.go,postgres.go,migrate.go}` — the `Store` interface (`ListByOperator`, `Upsert`, `Delete`), a `pgxpool` implementation with parameterized upsert/select/delete, and an `embed.FS` migration runner using `pg_advisory_lock`, one transaction per file, and refuse-to-start when `schema_migrations` is ahead of the embedded set; T018 passes (depends on T002, T019)
- [ ] T021 [P] Write failing tests in `internal/httpapi/router_test.go`: `GET /healthz` → 200 with no auth; any `/v1/...` with a missing or bad bearer token → 401 problem+JSON with the presented token never echoed; repeated bad auth from one IP → 429; a valid `operatorId:token` pair resolves the operator into the request context
- [ ] T022 Implement the `internal/httpapi` skeleton — `router.go` (`net/http` mux, `/healthz`), `auth.go` (bearer middleware parsing `EETMETER_SYNC_API_TOKENS` → operator, per-IP auth-failure token-bucket limiter), `problem.go` (RFC-9457 writer), `dto.go` (the `Account` response shape with `secretSet`/`deviceIdSet`, never `secret`); T021 passes
- [ ] T023a [P] Write failing tests in `internal/accountservice/service_test.go`: the constructor wires the injected `account.Set`, `store.Store`, `secret.Cipher`, `verification` runner and `eetmeter.Client` and rejects a nil dependency; the account→DTO projection helper maps `status`, `failureCategory`, `secretUpdatedAt`, `lastAuthenticatedAt`, `lastCheckedAt`, `configSource`, sets `secretSet`/`deviceIdSet`, and its output carries **no** `secret` or token field for every account state (`never_verified`, `connected`, `credentials_rejected`, `temporarily_unavailable`)
- [ ] T023 [P] Implement the `internal/accountservice/service.go` skeleton — a `Service` holding an in-memory `account.Set`, `store.Store`, `secret.Cipher`, the `verification` runner and `eetmeter.Client`; a constructor; and an unexported helper that projects an account + its persisted status to the `httpapi` `Account` DTO; T023a passes
- [ ] T024 Implement `cmd/eetmeter-sync/main.go` wiring — read env (cred key, API tokens, `EETMETER_SYNC_DATABASE_URL`, listen addr, Eetmeter tuning), build `secret.Cipher` (refuse to start on a bad key), open `pgxpool` and run `store.Migrate` (refuse to start on failure), build `eetmeter.Client`, the `verification` runner, the `accountservice.Service` and the `httpapi` router; serve HTTP; on `SIGTERM` drain the server and close the pool. Boots with `/healthz` green and zero accounts (depends on T010, T015, T017, T020, T022, T023)

**Checkpoint**: an authenticated, migrated, DB-connected service runs; no account operations yet.

---

## Phase 3: User Story 1 — Configure and verify the first account (Priority: P1) 🎯 MVP

**Goal**: an operator supplies one account's credentials over the HTTP API; the system
stores the secret safely, verifies it against Eetmeter, and reports `connected` or
`credentials rejected` — with no secret in any response or log.

**Independent test**: `POST /v1/accounts` with valid credentials → the account shows
`connected` with a last-authenticated time; `POST` with a wrong secret → `credentials_rejected`
and no credential material in any response, log, or error (spec US1).

- [ ] T025 [P] [US1] Write failing contract + service tests in `internal/httpapi/accounts_create_test.go` (httptest, fake Eetmeter): valid `POST /v1/accounts` → 201, body `status:"connected"`, `secretSet:true`, no `secret`/token anywhere; wrong secret → 201 `status:"credentials_rejected"`, `failureCategory:"credentials_rejected"`; duplicate label → 409; duplicate login → 409; blank secret / bad label → 422; `GET /v1/accounts/{label}` returns the account; unknown label → 404
- [ ] T026 [US1] Implement `accountservice.Configure(ctx, operatorID, in)` in `internal/accountservice/service.go` — validate via `internal/account`, enforce label + login uniqueness (409), accept or derive the deviceId, encrypt the secret in memory via `secret.Cipher`, reserve via `eetmeter.TryReserveVerify` before calling `verification.Verify` and return `ErrVerifyBudgetExhausted` to the caller unchanged, run `verification.Verify`, `store.Upsert` the result, set `configSource="runtime"` and `secretUpdatedAt=now`; return the projected DTO (depends on T024, T015b)
- [ ] T027 [US1] Implement the `POST /v1/accounts` and `GET /v1/accounts/{label}` handlers and routes in `internal/httpapi/{handlers.go,router.go}` — decode `AccountCreate`, map service errors to 201 / 409 / 422 / 404 problem responses, serialize the `Account` DTO with no secret; T025 passes (depends on T026, T022)
- [ ] T028 [US1] Add `slog` info/warn events for "account configured" and "verification outcome" in the service and handlers, routed through the `secret` redaction helper, and assert in a test that no secret or token string is present in captured log output (depends on T027)

**Checkpoint**: User Story 1 is fully functional and independently testable over HTTP.

---

## Phase 4: User Story 2 — Deploy with accounts already supplied (Priority: P1)

**Goal**: accounts provided in start-up configuration (config file and/or the single-account
env var) are loaded and verified automatically at boot, with no manual step; an invalid
entry is reported clearly without a secret and without blocking the valid ones.

**Independent test**: start the service with two accounts defined purely in configuration →
both appear with a definitive status within ~30 s and no interactive call was made (spec US2).

- [ ] T029 [P] [US2] Write failing tests in `internal/config/config_test.go`: parse the JSON file shape from data-model.md §4; `EETMETER_SYNC_ACCOUNT` (single JSON object) overrides a same-label file entry (env > file); a blank/invalid account is rejected with the operator and label named and **no secret in the message**, while the other accounts still load; `configSource` is set to `file` or `env` accordingly
- [ ] T030 [P] [US2] Implement `internal/config/config.go` — `Load(environ, fsys) (specs []AccountSpec, errs []LoadError)`: read `EETMETER_SYNC_CONFIG_FILE`, merge `EETMETER_SYNC_ACCOUNT`, apply precedence, validate each account with the `internal/account` rules, and collect per-account errors instead of aborting; T029 passes (depends on T012)
- [ ] T031 [P] [US2] Write failing integration test in `internal/accountservice/loadfromconfig_test.go` (fake Eetmeter): two valid specs → both added, verified, `configSource:"file"`; one spec missing a field → returned as a load error, not added, others unaffected; a later `Configure` for a file-sourced label → `configSource:"runtime"` wins and is reportable
- [ ] T032 [US2] Implement `accountservice.LoadFromConfig(ctx, specs)` in `internal/accountservice/service.go` — add each valid spec to the operator set, encrypt its secret, verify each (respecting the Eetmeter concurrency cap), upsert status, tag `configSource`; return the aggregated load errors; T031 passes (depends on T026)
- [ ] T033 [US2] Wire config loading into `cmd/eetmeter-sync/main.go` — call `config.Load` then `service.LoadFromConfig` during boot, log each `LoadError` (operator + label, no secret) and keep serving; configured accounts reach a definitive status within ~30 s of boot (SC-002) (depends on T030, T032, T024)

**Checkpoint**: User Stories 1 and 2 both work independently.

---

## Phase 5: User Story 3 — Fix an account after its password changes (Priority: P2)

**Goal**: updating an account's stored secret triggers automatic re-verification and clears
a failure without a restart; a live session refused by Eetmeter is re-authenticated once
from stored credentials before any failure is reported.

**Independent test**: put an account into `credentials_rejected`, `PUT` the correct secret →
it returns to `connected` on the automatic re-verification, no restart (spec US3).

- [ ] T034 [P] [US3] Write failing tests in `internal/httpapi/accounts_update_test.go`: `PUT /v1/accounts/{label}` with a new `secret` on a `credentials_rejected` account → 200 `status:"connected"`, fresh `lastAuthenticatedAt`, advanced `secretUpdatedAt`; `PUT` with a colliding `newLabel` → 409; unknown label → 404; empty body → 422; the secret never appears in the response or logs
- [ ] T035 [P] [US3] Write a failing test in `internal/eetmeter/client_test.go` (extend): a `200`-then-`401` sequence on the read-back triggers exactly one re-authentication from stored credentials before a failure is surfaced, and never loops (FR-013 / spec US3 scenario 2)
- [ ] T036 [US3] Implement `accountservice.Update(ctx, operatorID, label, in)` in `internal/accountservice/service.go` — apply `login` / `secret` / `deviceId` / `newLabel` changes via `internal/account` (uniqueness → 409); on a secret change re-encrypt, run `verification.Verify`, upsert status and bump `secretUpdatedAt`; set `configSource="runtime"` (depends on T026)
- [ ] T037 [US3] Implement the `PUT /v1/accounts/{label}` handler and route in `internal/httpapi/handlers.go` — decode `AccountUpdate`, require at least one field (422), map errors, serialize the DTO; T034 passes (depends on T036)
- [ ] T038 [US3] Harden the single-re-auth path in `internal/eetmeter/client.go` so T035 passes — one re-auth per request at most, no retry storm (depends on T015)

**Checkpoint**: User Stories 1–3 work independently.

---

## Phase 6: User Story 4 — Inspect and manage the configured accounts (Priority: P2)

**Goal**: list every configured account with its health and timestamps (no secret), remove
an account (deleting its stored secret and status), and trigger an on-demand re-check of one
account or all of them.

**Independent test**: with several accounts configured, `GET /v1/accounts` shows status +
last-auth + last-check for each and no secret; `DELETE` one → it disappears and its secret is
unrecoverable; `POST …/verify` updates status and timestamps (spec US4).

- [ ] T039 [P] [US4] Write failing tests in `internal/httpapi/accounts_list_delete_test.go`: `GET /v1/accounts` lists every account with `label`, `status`, `lastAuthenticatedAt`, `lastCheckedAt`, `failureCategory` and no secret; `DELETE /v1/accounts/{label}` → 204, the `account_status` row is gone, the account is absent from the list, and a repeat `DELETE` is still 204; another operator's accounts are untouched
- [ ] T040 [P] [US4] Write failing tests in `internal/httpapi/accounts_verify_test.go` (httptest, fake Eetmeter with a per-account request counter): `POST /v1/accounts/{label}/verify` re-runs verification and advances `lastCheckedAt`; `POST /v1/accounts/verify` re-verifies every account; within one `VERIFY_WINDOW` the 7th rapid single-account re-check returns `429`, the fake Eetmeter received `POST account/credentials` for that account at most `VERIFY_BURST` (6) times (one per admitted verification, in-attempt retries aside), and `lastCheckedAt` stops advancing once the budget is spent (SC-007); a re-check of a different account in the same window still runs
- [ ] T041 [US4] Implement `List`, `Remove`, `ReverifyOne`, `ReverifyAll` in `internal/accountservice/service.go` — `List` joins the in-memory set with persisted status; `Remove` drops the in-memory account, calls `store.Delete`, and discards any in-flight verification result for it (spec edge case); `Reverify*` call `eetmeter.TryReserveVerify` per account and propagate `ErrVerifyBudgetExhausted` for the transport to map to 429, then call `verification.Verify` and upsert (depends on T026)
- [ ] T042 [US4] Implement the `GET /v1/accounts`, `DELETE /v1/accounts/{label}`, `POST /v1/accounts/{label}/verify` and `POST /v1/accounts/verify` handlers and routes in `internal/httpapi/{handlers.go,router.go}`, mapping `ErrVerifyBudgetExhausted` to a 429 problem response; T039 and T040 pass (depends on T041, T022)

**Checkpoint**: all four user stories work independently.

---

## Phase 7: Polish & cross-cutting concerns

- [ ] T043 [P] Add a restart test in `internal/accountservice/restart_test.go`: persisted status is reloaded onto a fresh `Service` (not reset to `never_verified`), and a runtime-only account is absent after the set is rebuilt from configuration alone (FR-016 / spec Q4 / quickstart V7)
- [ ] T044 [P] Add an encryption-key edge-case test covering `cmd/eetmeter-sync`: an absent or malformed `EETMETER_SYNC_CRED_KEY` fails the boot fast and writes nothing to Postgres (no `account_status` change, `schema_migrations` untouched) (quickstart V8)
- [ ] T045 [P] Finalize `.github/workflows/ci.yml` — confirm `gofmt`, `go vet`, `go test ./... -race` (with the Postgres service container), the secret scanner and `docker build` all gate the build, and add a `go test ./...` run *without* `EETMETER_SYNC_TEST_DATABASE_URL` to prove the store tests skip cleanly
- [ ] T046 [P] Finalize the `Dockerfile` and `.dockerignore` — distroless non-root, small final image, `SIGTERM` handling verified against the running container
- [ ] T047 [P] Run `gofmt -l .` and `go vet ./...`, fix findings, and confirm every `internal/` package has a single-purpose `doc.go` comment (Constitution I)
- [ ] T048 Run the full `quickstart.md` validation (V1–V9) against the compose Postgres and a fake or live Eetmeter; capture the results
- [ ] T049 [P] Update `README.md` with a short "run locally" pointer to `quickstart.md`, the `EETMETER_SYNC_*` variable list, and a note about the Eetmeter `userfavorite` read-back discrepancy recorded in research.md Decision 2

---

## Dependencies & execution order

### Phase dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: depends on Setup. **Blocks every user story.**
- **User stories (Phases 3–6)**: each depends only on Foundational. US1 → US2 → US3 → US4
  is the priority order; with staff they can overlap, but they share
  `internal/accountservice/service.go` and `internal/httpapi/handlers.go`, so the
  implementation tasks that touch those files serialize.
- **Polish (Phase 7)**: depends on the user stories it exercises (T043 needs US1–US2,
  T048 needs US1–US4).

### Key task dependencies

- T008 → T012 (deviceId derivation); T012 → T030, T026
- T013 → T014 → T015 → T017, T038
- T015 → T015a → T015b; T015b → T026, T041
- T019 → T020; T002 → T020
- T023a → T023
- T010, T015, T015b, T017, T020, T022, T023 → T024 (main wiring)
- T024 → T026 → T027 → T028
- T026 → T032 → T033; T030 → T033
- T026 → T036 → T037
- T026 → T041 → T042

### Parallel opportunities

- Setup: T003, T004, T005, T006 in parallel after T001/T002.
- Foundational: the test+impl pairs for `uuid`, `secret`, `account`, `verification` are
  mutually independent — T007–T012 and T016–T017 can proceed in parallel; T013–T015b
  (`eetmeter`) and T018–T020 (`store`) in parallel with them; T021 and the T023a→T023
  pair in parallel. Only T024 (wiring) waits for all.
- Within a story: the `[P]` test tasks can be written together before the implementation
  tasks in that phase.

---

## Parallel example: Foundational package tests

```bash
# Write these failing tests together (different files, no shared deps):
Task: "internal/uuid/uuid_test.go — v4/v5 behavior"          # T007
Task: "internal/secret/secret_test.go — AES-GCM + redaction" # T009
Task: "internal/account/account_test.go — rules + deviceId"  # T011
Task: "internal/verification/verify_test.go — classification"# T016
Task: "internal/store/postgres_test.go — migrate + CRUD"     # T018
Task: "internal/httpapi/router_test.go — healthz + auth"     # T021
```

---

## Implementation strategy

### MVP (User Story 1 only)

1. Phase 1 Setup.
2. Phase 2 Foundational — the whole skeleton must stand up (this is the bulk of feature 001).
3. Phase 3 User Story 1.
4. **STOP and validate**: run quickstart V1 (and V9 for auth). An operator can add an
   account over HTTP and see it verified, with no secret leaking.

### Incremental delivery

1. Setup + Foundational → service boots, migrates, authenticates.
2. + US1 → runtime `POST` configure/verify (MVP, quickstart V1).
3. + US2 → declarative start-up configuration (quickstart V2, V3).
4. + US3 → secret update re-verifies; session re-auth (quickstart V4).
5. + US4 → list / delete / on-demand re-verify (quickstart V5, V6).
6. Polish → restart + key-edge tests, CI/Docker finalization, full quickstart run.

---

## Notes

- Every `internal/` package and every Eetmeter contract has its failing test task before
  the implementation task — this is mandatory (Constitution II), not optional.
- `[P]` = different files, no dependency on an unfinished task.
- Secrets, tokens and personal data must never reach a log, response, metric, error, or
  fixture — assert this in tests (T028, T034, T039) and scrub fixtures (T013).
- Commit after each task or logical group; keep `main` building and `go test ./... -race`
  green; use Conventional Commits with the trailers from `CLAUDE.md`.
- Stop at any checkpoint to validate a story against `quickstart.md` before continuing.
