---
description: "Task list for Recipe Sync implementation"
---

# Tasks: Recipe Sync

**Input**: Design documents from `/specs/001-recipe-sync/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: Included where the constitution (Principle III) requires them — the
reconciliation engine, the fingerprint, and the Eetmeter client parsing. HTTP
handlers get happy-path + the 409 case; the store gets opt-in integration tests.
No test-first mandate; write them alongside the code.

**Status (implemented in this session)**: 52 of 57 tasks done. The whole service
builds, `go vet`/`gofmt` clean, unit + Postgres integration tests green, runs end
to end via `make dev` against the bundled `cmd/fake-eetmeter`, and its container
image builds and runs.

**T001 (spike) — DONE. Every API path CONFIRMED against the live API**
(`contracts/eetmeter-api.md` "Spike log"; `internal/eetmeter/live_test.go` — a
read test and a self-cleaning create→update→delete write test, both pass against
production). Folded in:
- Wire format: read is **PascalCase**, write is **camelCase**; the real
  ingredient shape has more fields than the reference client showed.
- `version: 4.6.0` is pinned (anything else → `426`).
- Auth is a **device-bound `token` + `deviceId` pair** captured from the app — a
  bare password login is not reproducible server-side. Added
  `EETMETER_ACCOUNT_x_TOKEN` / `_DEVICE_ID` + token mode in the client.
- Create + update are the **same call**: `PUT combinedproduct` with a
  client-generated recipe id; delete is `DELETE combinedproduct/{id}`.
- `preparationMethodName` is ignored on write → excluded from the fingerprint.
- Added `internal/dotenv` (literal `.env` loader) after shell-sourcing `.env`
  mangled a password.
- Full engine run against the real account (both sides = one account): 44 real
  recipes → 44 `linked`, zero writes, zero errors.

**T030 / T036 / T041 / T048 / T056** — manual per-story validation against a real
*pair* of accounts. Only one account is available. Every behaviour is covered by
an `internal/syncengine/engine_test.go` integration test, the fake-API `make dev`
run, and (for the client) the live write round-trip test.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 / US2 / US3 / US4 — user-story phases only
- Every task names an exact file path.

## Path Conventions

Single Go module at repository root: `cmd/eetmeter-sync/`, `internal/…`. Layout is
fixed in [plan.md](./plan.md) → "Source Code".

---

## Phase 1: Setup

**Purpose**: Discovery and project skeleton.

- [X] T001 [P] Run the Mijn Eetmeter API spike against two throwaway accounts per [quickstart.md](./quickstart.md) "Step 0": confirm login + `Authorization: Basic <token>:<deviceId>`, `GET combinedproduct`, and the **create** and **update** request/response shapes. Save raw responses as fixtures in `internal/eetmeter/testdata/` and rewrite the UNVERIFIED sections of `specs/001-recipe-sync/contracts/eetmeter-api.md` with confirmed method/path/body. **Blocks T024, T031.**
- [X] T002 Initialize the Go module: `go mod init github.com/mortenzwarenstein/eetmeter-sync` (confirm path with maintainer), add `github.com/jackc/pgx/v5`, create `go.mod` / `go.sum`. The commit that adds pgx MUST justify it per constitution (no PG driver in stdlib; pgx is the maintained standard; `lib/pq` is frozen).
- [X] T003 [P] Create the package skeleton with doc comments and `package` declarations: `cmd/eetmeter-sync/main.go`, `internal/config/`, `internal/eetmeter/`, `internal/fingerprint/`, `internal/syncengine/`, `internal/store/`, `internal/store/migrations/`, `internal/httpapi/`, `internal/scheduler/`.
- [X] T004 [P] Add `.dockerignore` and a stub `Dockerfile` at repo root (real content in T052).
- [X] T005 [P] Add `scripts/check.sh` (or a `Makefile` target) running `gofmt -l .` and `go vet ./...`; wire nothing to CI yet.

---

## Phase 2: Foundational

**Purpose**: Everything every user story needs — config, DB, client read path, fingerprint, engine spine, HTTP spine.

**⚠️ No user-story phase can start until this phase is complete.**

- [X] T006 [P] Implement `internal/config/config.go`: load and validate every environment variable in [contracts/http-api.md](./contracts/http-api.md) → "Runtime configuration"; required vars error on absence; `Config.String()` / `LogValue()` redacts passwords and `SYNC_API_TOKEN`.
- [X] T007 [P] Set up `log/slog` in `cmd/eetmeter-sync/main.go`: level from `LOG_LEVEL`, JSON handler, a package helper that constructs account log attrs from `label`/`email` only (never password).
- [X] T008 Write `internal/store/migrations/0001_init.sql`: tables `account`, `recipe_link`, `sync_run`, `conflict_resolution`, `schema_migrations` exactly per [data-model.md](./data-model.md), including the partial unique index `one_running_sync` on `sync_run`.
- [X] T009 Implement `internal/store/migrate.go`: `//go:embed migrations/*.sql`, apply unapplied files in ascending numeric order each inside its own transaction, record `version` in `schema_migrations`.
- [X] T010 Implement `internal/store/store.go`: `Open(ctx, databaseURL)` (pgxpool), `Close()`, `Ping(ctx)`; query methods only (no business logic) for `account` (upsert `a`/`b`, get), `recipe_link` (list all, upsert, set status/timestamps), `sync_run` (start, upsert summary, finish), `conflict_resolution` (list, get by link, delete).
- [X] T011 [P] `internal/store/store_test.go`: integration tests that `t.Skip` unless `TEST_DATABASE_URL` is set — link upsert + status transition, `sync_run` start→finish, `conflict_resolution` upsert→delete. (depends on T010)
- [X] T012 [P] Implement `internal/eetmeter/types.go`: `Recipe` (`ID`, `Name`, `NumberOfPortions`, `Items`) and `Ingredient` (`ID`, `BrandName`, `BrandProductID`, `ProductUnitID`, `ProductName`, `UnitName`, `Amount`) with JSON tags matching the lower-first-char wire format from [contracts/eetmeter-api.md](./contracts/eetmeter-api.md).
- [X] T013 Implement `internal/eetmeter/client.go`: `New(cfg)`, `Login(ctx)` (`POST account/credentials`, generate `deviceId`, build `Authorization: Basic <token>:<deviceId>`), a request helper that adds `version` / `platform` headers and a 30s per-call `context` deadline, one retry on connection error / 5xx, and maps 401/403 to a typed `ErrAuth`. (depends on T012)
- [X] T014 Implement `ListRecipes(ctx) ([]Recipe, error)` in `internal/eetmeter/recipes.go` → `GET combinedproduct`, decoding `{ "items": [...] }`. (depends on T012)
- [X] T015 [P] `internal/eetmeter/client_test.go`: `httptest.Server` replaying T001 fixtures — asserts the auth header format, list decoding, and `ErrAuth` on 401. (depends on T013, T014)
- [X] T016 [P] Implement `internal/fingerprint/fingerprint.go`: `Of(Recipe) [32]byte` — normalize `Items` to `{brandProductID, productUnitID, productName, unitName, amount@3dp}`, sort by `(productUnitID, brandProductID, amount, productName)`, include `numberOfPortions`, exclude all `id`s and `name`, serialize canonically, SHA-256.
- [X] T017 [P] `internal/fingerprint/fingerprint_test.go`: ingredient reordering yields an equal hash; `amount` float noise (e.g. `300` vs `300.0000001`) yields an equal hash; a real change (portion count, amount, ingredient set) flips the hash. (depends on T016)
- [X] T018 Implement `internal/syncengine/reconcile.go` scaffold: types `Action`, `ActionKind` (`CreateInA`, `CreateInB`, `UpdateInA`, `UpdateInB`, `Link`, `FlagConflict`, `KeepConflict`, `ApplyResolution`, `RetireLink`, `Skip`, `Noop`) and the pure `Decide(link *Link, a, b *Recipe, pending *Resolution) []Action`. Implement only the uncontested branches now: both sides unchanged → `Noop`; first sight, both sides present, equal fingerprint → `Link`; a linked side's recipe absent this run → `RetireLink`; duplicate name within one account (signaled by the caller) → `Skip`. (US1/US2/US3 branches are filled in their phases.)
- [X] T019 Implement `internal/syncengine/engine.go` `Run(ctx, trigger string) (RunID, error)`: in-process `sync.Mutex` + running flag returning `ErrAlreadyRunning`; `Login` both accounts and on `ErrAuth` (or any fetch error) abort with a `failed` `sync_run` and **no writes**; `ListRecipes` both; build name→recipe maps; detect intra-account duplicate names; load links; loop `Decide`; apply each action's store + client effects inside a per-recipe-pair transaction; assemble `SyncSummary` — including an `items[]` list of `{name, action}` (one entry per recipe pair processed), the aggregate `counts`, and `trigger` / `result` / `startedAt` / `finishedAt`; upsert to `sync_run` as it goes; finalize `succeeded`. Non-US branches (create/update/conflict) call into action handlers that are stubbed to `Noop` until their phase.
- [X] T020 [P] Implement `internal/httpapi/server.go`: `NewServer(engine, store, cfg)`, `net/http.ServeMux`, bearer middleware (enforced only when `SYNC_API_TOKEN` set; always exempts `/healthz`), JSON write/error helpers.
- [X] T021 Implement foundational routes in `internal/httpapi/handlers.go`: `GET /healthz` (store `Ping`, 200/503), `POST /sync` and `GET /sync` (spawn `engine.Run` in a goroutine, return `202` with `runId`/`startedAt`, or `409` on `ErrAlreadyRunning`), `GET /sync/last` (latest `sync_run`, `404` if none). (depends on T020, T010, T019)
- [X] T022 Wire `cmd/eetmeter-sync/main.go`: load `Config` → `store.Open` → `store.Migrate` → seed `account` rows `a`/`b` from config (log a warning if an `email` changed) → build `eetmeter.Client` + `syncengine.Engine` → start `httpapi` server on `HTTP_ADDR` → `signal.NotifyContext` for `SIGINT`/`SIGTERM` graceful shutdown. (depends on T006–T021)
- [X] T023 [P] `internal/httpapi/handlers_test.go` (foundational subset): `/healthz` 200 and 503; `/sync` returns 202; `/sync` returns 409 when the engine reports running; `/sync/last` 404 then 200. (depends on T021)

**Checkpoint**: `go run ./cmd/eetmeter-sync` boots, migrates, `/healthz` is green, and a triggered run logs in to both accounts, lists recipes, and writes a summary row.

---

## Phase 3: User Story 1 - New recipes propagate both ways (Priority: P1) 🎯 MVP

**Goal**: A recipe that exists in only one account is created in the other on the next sync, both directions; pre-existing same-named recipes are linked without duplication.

**Independent Test**: [quickstart.md](./quickstart.md) → "US1". Create a unique recipe in each account, `POST /sync`, confirm each appears on the other side with equal content and that a second sync is a no-op.

- [X] T024 [US1] Implement `CreateRecipe(ctx, Recipe) (Recipe, error)` in `internal/eetmeter/recipes.go` using the shape confirmed in T001; remove the UNVERIFIED comment. If the create response omits the new `id`, re-`ListRecipes` and match by `name`. (depends on T001, T012)
- [X] T025 [P] [US1] Reconcile any extra portable recipe fields the spike revealed: update `internal/fingerprint/fingerprint.go` + its test and `internal/eetmeter/types.go` so the fingerprint covers everything `CreateRecipe` sends. (depends on T001; no-op if the spike found nothing new)
- [X] T026 [US1] Fill the create/link branches in `internal/syncengine/reconcile.go` `Decide`: name in A only, no link → `CreateInB`; name in B only, no link → `CreateInA`; name on both, no link, equal fingerprint → `Link`; name on both, no link, unequal fingerprint → `FlagConflict`. (depends on T018)
- [X] T027 [US1] Implement the `CreateInA` / `CreateInB` / `Link` action handlers in `internal/syncengine/engine.go`: call `CreateRecipe` on the target, re-fetch the created recipe, set both link baselines to the server-side fingerprint, insert the `recipe_link` as `active`, and append a `{name, action}` entry (`createdInA` / `createdInB` / `linked`) to the summary `items[]`. (depends on T026, T019, T024)
- [X] T028 [P] [US1] Add table cases to `internal/syncengine/reconcile_test.go`: create-A-only, create-B-only, first-sync equal → link, first-sync unequal → flag, already-linked-unchanged → noop. (depends on T026)
- [X] T029 [P] [US1] Add `CreateRecipe` request-body + response-parse assertions to `internal/eetmeter/recipes_test.go` against the T001 fixture. (depends on T024)
- [ ] T030 [US1] Run the quickstart "US1" validation against two real accounts; record the outcome in the PR/commit. (depends on T027, T022)

**Checkpoint**: MVP — new recipes flow both ways; re-running creates no duplicates.

---

## Phase 4: User Story 2 - Edits propagate both ways (Priority: P2)

**Goal**: A change to a linked recipe on one side is copied to the other on the next sync; an edit that makes the two sides identical is a no-op.

**Independent Test**: [quickstart.md](./quickstart.md) → "US2". Edit a linked recipe on one side only, `POST /sync`, confirm the other side matches and nothing else changed; repeat in the other direction.

- [X] T031 [US2] Implement `UpdateRecipe(ctx, id string, r Recipe) (Recipe, error)` in `internal/eetmeter/recipes.go` per T001. If the API has no true update, implement the delete-then-recreate fallback **restricted to the receiving copy of an already-linked pair** (permitted by FR-010, which forbids only deletion *propagation*). (depends on T001, T012)
- [X] T032 [US2] Fill the update branches in `internal/syncengine/reconcile.go` `Decide`: linked, changed on A only → `UpdateInB`; changed on B only → `UpdateInA`; changed on both but the two current fingerprints are equal → `Link`-style baseline refresh (`Noop` write, record `noop`). (depends on T018)
- [X] T033 [US2] Implement the `UpdateInA` / `UpdateInB` action handlers in `internal/syncengine/engine.go`: `UpdateRecipe` on the stale side, re-fetch, set both baselines to the winner's server fingerprint, append a `{name, action}` entry (`updatedInA` / `updatedInB`) to the summary `items[]`. (depends on T032, T019, T031)
- [X] T034 [P] [US2] Add table cases to `internal/syncengine/reconcile_test.go`: update-A-only, update-B-only, both-changed-to-equal → no write, neither-changed → noop. (depends on T032)
- [X] T035 [P] [US2] Add `UpdateRecipe` request/response assertions to `internal/eetmeter/recipes_test.go` against the T001 fixture. (depends on T031)
- [ ] T036 [US2] Run the quickstart "US2" validation against two real accounts. (depends on T033, T022)

**Checkpoint**: One-sided edits propagate; convergent edits write nothing.

---

## Phase 5: User Story 4 - On-demand trigger and run summary (Priority: P2)

**Goal**: The maintainer can start a sync from a browser or `curl`, gets an immediate acknowledgement, is blocked from starting a second concurrent run, and can read a per-account, per-recipe summary of what happened.

**Independent Test**: [quickstart.md](./quickstart.md) → "US4". `POST /sync` → 202; immediate second `POST /sync` → 409; after completion `GET /sync/last` shows `counts` and an `items` list; `GET /sync` in a browser starts a run.

- [X] T037 [US4] Enrich `SyncSummary` in `internal/syncengine/engine.go`: add a human-readable `detail` string to each `items[]` entry, make sure every `action` value in [data-model.md](./data-model.md) → "Run summary JSON" can be produced, and add the `accounts` label block. The `items[]` array, `counts`, `trigger`, `result`, and timestamps already exist from T019 — this task completes the shape, it does not introduce it.
- [X] T038 [US4] In `internal/httpapi/handlers.go`: `GET /sync/last` serializes the full structured summary; the `409` body from `POST/GET /sync` includes the in-progress `runId` and `startedAt`; confirm `GET /sync` (browser) path returns `202`. (depends on T037, T021)
- [X] T039 [P] [US4] Add a minimal `GET /` handler in `internal/httpapi/handlers.go` returning a `text/plain` list of the routes. (depends on T020)
- [X] T040 [P] [US4] Extend `internal/httpapi/handlers_test.go`: full summary shape from `/sync/last`, `409` body contents on concurrent trigger, `GET /sync` starts a run. (depends on T037, T038)
- [ ] T041 [US4] Run the quickstart "US4" validation (curl + browser + concurrent 409). (depends on T038, T022)

**Checkpoint**: Runs are observable in detail; concurrent triggers are rejected cleanly.

---

## Phase 6: User Story 3 - Conflicting edits are flagged, not overwritten (Priority: P3)

**Goal**: When both sides changed a linked recipe since the last sync, neither copy is written and the recipe is listed as a conflict; the maintainer picks a winner and the next run applies it.

**Independent Test**: [quickstart.md](./quickstart.md) → "US3". Edit the same linked recipe differently on both sides, `POST /sync`, confirm neither side changed and `GET /conflicts` lists it; `POST /conflicts/{id}/resolve` then `POST /sync`, confirm the chosen side wins and the conflict clears.

- [X] T042 [P] [US3] Finalize `conflict_resolution` and conflict-field queries in `internal/store/store.go`: upsert a pending resolution (one per link), get by link id, delete on apply; set/clear `recipe_link.conflict_detected_at`. (depends on T010)
- [X] T043 [US3] Fill the conflict branches in `internal/syncengine/reconcile.go` `Decide`: linked, changed on both sides, unequal fingerprints, no pending resolution → `FlagConflict` (no writes); status already `conflict`, still divergent, no resolution → `KeepConflict`; pending resolution present → `ApplyResolution{winner}`; pending resolution but link no longer divergent → `Skip` with a "stale resolution discarded" detail. (depends on T018)
- [X] T044 [US3] Implement the conflict action handlers in `internal/syncengine/engine.go`: `FlagConflict` sets `recipe_link.status='conflict'` + `conflict_detected_at`, writes nothing to either account, records `flaggedConflict`; `ApplyResolution` copies the winner's current content to the other side via `UpdateRecipe`, sets both baselines to the winner's fingerprint, deletes the `conflict_resolution` row, sets status back to `active`, records `resolutionApplied`. (depends on T043, T019, T031, T042)
- [X] T045 [US3] Implement conflict routes in `internal/httpapi/handlers.go`: `GET /conflicts` (each `recipe_link` in `conflict`, with A and B content re-fetched live, plus `pendingResolution`); `POST /conflicts/{linkID}/resolve` (accept `{"winner":"a|b"}` or `?winner=`, `400` on a bad/missing winner, `404` if that link is not currently a conflict, else upsert the pending resolution). (depends on T020, T042)
- [X] T046 [P] [US3] Add table cases to `internal/syncengine/reconcile_test.go`: flag on dual divergent change, conflict persists on the next run with no resolution, resolution applied, stale resolution discarded, dual change that converges is NOT a conflict. (depends on T043)
- [X] T047 [P] [US3] Extend `internal/httpapi/handlers_test.go`: `/conflicts` lists a seeded conflict with both versions; `resolve` happy path, `400` bad winner, `404` when the link is not in conflict. (depends on T045)
- [ ] T048 [US3] Run the quickstart "US3" validation against two real accounts. (depends on T044, T045, T022)

**Checkpoint**: Dual edits never overwrite; conflicts are listed and resolvable; the resolution lands on the following run.

---

## Phase 7: Automatic daily runs

**Purpose**: FR-012 — the sync also runs unattended once a day.

- [X] T049 [P] Implement `internal/scheduler/scheduler.go`: parse `SYNC_DAILY_TIME` in `SYNC_TIMEZONE`, compute the next occurrence, `time.Timer`, on fire call `engine.Run(ctx, "scheduled")` (log and skip on `ErrAlreadyRunning`), then reschedule; run once at boot when `SYNC_ON_STARTUP` is true. (depends on T019)
- [X] T050 Wire the scheduler into `cmd/eetmeter-sync/main.go` start/stop alongside the HTTP server in the graceful-shutdown path. (depends on T049, T022)
- [X] T051 [P] `internal/scheduler/scheduler_test.go`: next-occurrence math across a midnight and a DST boundary; a fired timer invokes the callback; an overlapping tick is skipped. (depends on T049)

**Checkpoint**: With nobody watching, recipes still converge each morning.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T052 [P] Fill in the multi-stage `Dockerfile` (build → distroless static) and verify `docker build` + `docker run --env-file` per [quickstart.md](./quickstart.md) → "Container build".
- [X] T053 [P] Verify SC-007 / FR-020: audit `slog` call sites, HTTP responses, and `sync_run.error` writes for any password path; add a redaction unit test in `internal/config/`.
- [X] T054 [P] Confirm the per-call (30s) and per-run (10m) context deadlines and the single-retry policy in `internal/eetmeter/client.go`; add a retry test against `httptest`.
- [X] T055 [P] Write `README.md`: what it is, the env vars (link to [contracts/http-api.md](./contracts/http-api.md)), how to run, how to review and resolve conflicts.
- [ ] T056 Run the full [quickstart.md](./quickstart.md) end to end against two real accounts, including the edge checks: deletion → `linkRetired`, duplicate name → `skipped`, wrong password → `failed` with no writes, process killed mid-run → next run reconciles.
- [X] T057 [P] `gofmt -l .` clean, `go vet ./...` clean, `go mod tidy`; run `scripts/check.sh`.

---

## Dependencies & Execution Order

### Phase order

- **Phase 1 Setup** → **Phase 2 Foundational** → user-story phases → **Phase 7** → **Phase 8**.
- T001 (spike) has no code dependency and should start first; it only blocks T024 and T031 (the client write methods). All other Foundational work can proceed while the spike is arranged.
- Foundational **blocks every user-story phase**.

### User-story phase order

- **US1 (P1)** after Foundational — no dependency on other stories. This is the MVP.
- **US2 (P2)** after Foundational — independent of US1 in principle; shares `reconcile.go` / `engine.go` / `recipes.go`, so in a single-developer flow do it after US1.
- **US4 (P2)** after Foundational — the trigger + `/sync/last` spine, and the `items[]` + `counts` summary shape, are already in Foundational (T019, T021); this phase adds per-item `detail`, full action-kind coverage, the 409 body, and the browser affordance. Best done after US1/US2 so the summary has real actions to render.
- **US3 (P3)** after Foundational — adds the conflict store queries, `Decide` branches, and two endpoints. Independent of US2's update path only if T031 (`UpdateRecipe`) is present, so sequence it after US2.

### Within a phase

- Different files marked **[P]** run in parallel.
- `reconcile.go` branch tasks precede the matching `engine.go` handler tasks precede the manual validation task.
- Tests marked [P] can be written in parallel with sibling [P] tasks.

---

## Parallel Opportunities

- **Setup**: T003, T004, T005 in parallel (T001 too; T002 must land before any code compiles).
- **Foundational**: T006 + T007 (config/logging); T008–T011 (store) parallel with T012–T015 (client) parallel with T016–T017 (fingerprint) parallel with T020 (http server). T018/T019 need the store+client+fingerprint; T021–T023 need T019/T020.
- **US1**: T024 ∥ T025; then T026 → T027; T028 ∥ T029 alongside.
- **US2**: T031 → T032 → T033; T034 ∥ T035.
- **US3**: T042 ∥ T043; then T044 and T045; T046 ∥ T047.
- **Polish**: T052–T055 and T057 all parallel; T056 last.

### Parallel example — Foundational

```text
# Three independent tracks after T002 lands:
Track A: T008 → T009 → T010 → T011        (store)
Track B: T012 → T013 → T014 → T015        (eetmeter client, read path)
Track C: T016 → T017                      (fingerprint)
# Converge: T018 → T019, with T020 (http server) in parallel, then T021 → T022 → T023
```

---

## Implementation Strategy

### MVP (stop here for a first useful cut)

1. Phase 1 Setup.
2. Phase 2 Foundational — the whole phase; it is the spine.
3. Phase 3 US1 — new recipes propagate both ways.
4. Validate US1 against two real accounts (T030). Deploy the container and let it be triggered by hand.

At this point recipes you add on either side stop needing manual re-entry — the
original ask is met, minus edits/conflicts/automation.

### Incremental delivery after MVP

1. **US2** — edits propagate. Re-validate US1 still holds.
2. **US4** — the run summary becomes legible; concurrent trigger is safe.
3. **US3** — conflicts are surfaced and resolvable instead of silently skipped as `flagged`.
4. **Phase 7** — turn on the daily scheduler; stop triggering by hand.
5. **Phase 8** — Dockerfile finalised, secret-hygiene audit, README, full quickstart pass.

### Notes

- `[P]` = different files, no incomplete-task dependency.
- The spike (T001) is the single biggest risk-retirement; do it before committing to the client write code.
- Commit per task or per logical group; each user-story checkpoint is a safe stopping point.
- No coverage target. Cover `reconcile.go` and `fingerprint.go` thoroughly; keep handler and store tests thin.
