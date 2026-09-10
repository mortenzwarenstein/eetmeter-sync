---
description: "Task list for Sync & Conflict UI"
---

# Tasks: Sync & Conflict UI

**Input**: Design documents from `/specs/002-sync-conflict-ui/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/ui.md

**Tests**: Included for the non-trivial pieces only (conflict diff, session store,
auth middleware, handler behaviour) — required by Constitution principle III.
Templates and struct-to-view mapping are not separately tested.

**Organization**: Tasks are grouped by user story. US1 and US2 are both P1 and
together form the MVP; US3 and US4 are P2 increments.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1–US4 for story-phase tasks; none for Setup / Foundational / Polish

## Path Conventions

Single Go module. UI code is added to the existing `internal/httpapi` package;
templates under `internal/httpapi/templates/` behind `embed.FS`. No new package,
no new dependency, no DB migration.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Feature scaffolding that carries no logic.

- [X] T001 [P] Create `internal/httpapi/templates/layout.tmpl` — HTML shell with an inline `<style>` block (page layout + `.diff` / `.only-a` / `.only-b` marker classes + a `.dry-run` badge style), a `{{block "content" .}}` slot, a flash-banner region, and a `{{if .Running}}<meta http-equiv="refresh" content="3">{{end}}` in `<head>`.
- [X] T002 [P] Create `internal/httpapi/ui.go` with the `//go:embed templates/*.tmpl` FS, a parsed `*template.Template` package var, a `render(w http.ResponseWriter, status int, name string, data any)` helper, and a `flashMessage(code string) string` mapper for `started` / `already-running` / `resolved` / `stale` / `bad-credentials`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The engine seam and HTTP wiring every UI page builds on. JSON API
behaviour must stay identical (FR-020).

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T003 Make dry-run per-run in `internal/syncengine/engine.go`: change `Trigger(trigger string, dryRun bool)` and `RunSync(ctx context.Context, trigger string, dryRun bool)`, thread `dryRun` through `execute` and `apply` (replace every `e.dryRun` read), and remove the `dryRun` field plus the `dryRun` argument to `New`.
- [X] T004 Update all in-repo callers of the old signatures: `internal/syncengine/engine_test.go` and `internal/scheduler/scheduler_test.go` (grep `Trigger(` / `RunSync(` / `syncengine.New(`); pass an explicit `false` unless the case is about dry-run.
- [X] T005 Update `cmd/eetmeter-sync/main.go`: pass `cfg.DryRun` in the scheduler closure (`engine.Trigger(reason, cfg.DryRun)`) and the startup trigger; drop `cfg.DryRun` from the `syncengine.New(...)` call; pass `cfg.DryRun`, `cfg.AccountA.Label`, `cfg.AccountB.Label` into `httpapi.New(...)`.
- [X] T006 Change `httpapi.New` in `internal/httpapi/server.go` to `New(engine Engine, store Store, token string, defaultDryRun bool, labelA, labelB string, log *slog.Logger)`; add the fields to `Server`; update the `Engine` interface to `Trigger(trigger string, dryRun bool) (string, time.Time, error)`; have the JSON `handleSync` in `internal/httpapi/handlers.go` call `s.engine.Trigger("manual", s.defaultDryRun)` (behaviour unchanged from today).
- [X] T007 Update `internal/httpapi/handlers_test.go`: the fake `Engine` implements `Trigger(trigger string, dryRun bool)`; fix `httpapi.New(...)` call sites for the new signature.
- [X] T008 In `internal/httpapi/server.go` register the UI routes on the mux, each wrapped by a new `s.uiProtected(next)` middleware that is currently a pass-through: `GET /ui`, `POST /ui/sync`, `GET /ui/conflicts`, `POST /ui/conflicts/{linkID}/resolve`, `GET /ui/login`, `POST /ui/login`, `POST /ui/logout` (handlers are stubs returning 501 until their story). Add a `"GET  /ui"` line to the `routes` list in `handleIndex` (`internal/httpapi/handlers.go`).

**Checkpoint**: `make test` green; JSON API unchanged; `/ui/*` routes exist as stubs.

---

## Phase 3: User Story 1 - Trigger a sync and see what it did (Priority: P1) 🎯 MVP

**Goal**: From `/ui`, start a sync, get immediate confirmation, and watch the
latest run's per-account summary appear when it finishes.

**Independent Test**: Open `/ui` with no prior run → "no runs yet"; click Trigger
→ redirect with "started" banner within seconds; page self-refreshes while
running; on completion shows trigger/times/result/per-account counts; a second
immediate trigger shows "already running" and starts no second run.

### Implementation for User Story 1

- [X] T009 [US1] Add `dashboardView` assembly + `GET /ui` handler in `internal/httpapi/ui.go`: call `Store.LatestRunSummary`, decode the summary JSON, set `HasRun`, set `Running` from `result == "running"` **or** `Engine.Status()`, resolve `LabelA`/`LabelB` from the summary `accounts` map falling back to the configured labels, map `?msg=`.
- [X] T010 [P] [US1] Create `internal/httpapi/templates/dashboard.tmpl`: "no runs yet" empty state; otherwise run id, trigger, started/finished, `succeeded`/`failed`/`running` (+ error text), and a per-account table of created / updated / flagged / skipped / retired with skip reasons from `items`; a `POST /ui/sync` form with a `dryRun` checkbox and a submit button; a link to `/ui/conflicts`.
- [X] T011 [US1] Add the `POST /ui/sync` handler in `internal/httpapi/ui.go`: call `s.engine.Trigger("manual", s.defaultDryRun)`; `errors.Is(err, syncengine.ErrAlreadyRunning)` → `303 /ui?msg=already-running`; other error → `render` a 500 HTML page; success → `303 /ui?msg=started`.
- [X] T012 [US1] Wire the auto-refresh: pass `dashboardView.Running` into the layout so T001's `{{if .Running}}<meta refresh>{{end}}` fires only while a run is in progress and is absent once finished.
- [X] T013 [P] [US1] Tests in `internal/httpapi/ui_test.go` (httptest + fake Engine/Store): `GET /ui` no-run → 200 + empty-state text; `GET /ui` with a finished summary → counts rendered; meta-refresh present iff `result=="running"`; `POST /ui/sync` → 303 `Location: /ui?msg=started`; fake Engine returning `ErrAlreadyRunning` → 303 `?msg=already-running`.

**Checkpoint**: US1 fully usable in a browser; MVP candidate (open, no auth).

---

## Phase 4: User Story 2 - Review a conflict and choose which version wins (Priority: P1)

**Goal**: `/ui/conflicts` shows each unresolved conflict with both accounts'
current versions side by side and differences marked; the maintainer picks a
winner and optionally triggers a sync immediately.

**Independent Test**: With one conflict present, open `/ui/conflicts` → both
versions side by side, diffs marked; choose A + Save → "recorded", entry shows
"applies next run"; Save & sync now → run starts immediately; resolving the same
conflict in another tab then submitting → "no longer open" + refreshed list, no
error page; no conflicts → empty state.

### Implementation for User Story 2

- [X] T014 [P] [US2] Create `internal/httpapi/ui_diff.go`: pure `diffSides(a, b *ConflictSide) (portions diffPair, items []itemDiffRow)` per `data-model.md` — compare `NumberOfPortions`; index items by `ProductName` (fall back to `ProductUnitID` on collision); walk the sorted union marking `OnlyA` / `OnlyB` / `AmountDiffers` / `UnitDiffers`; deterministic (name-sorted) output.
- [X] T015 [P] [US2] Table-driven tests in `internal/httpapi/ui_diff_test.go`: identical sides (no marks), portion-only diff, amount diff, unit diff, item only in A, item only in B, duplicate product names on one side, and stable ordering across input permutations.
- [X] T016 [US2] Add `conflictsView` assembly + `GET /ui/conflicts` handler in `internal/httpapi/ui.go`: `Store.ListConflicts`, build one `conflictRow` per conflict (name, detected-at, `diffSides` output, `Pending` from `PendingResolution`), map `?msg=`, empty state when the slice is empty.
- [X] T017 [P] [US2] Create `internal/httpapi/templates/conflicts.tmpl`: per conflict, an A|B side-by-side block (portions + ingredient rows) using the `.diff` / `.only-a` / `.only-b` classes from T001; a `winner` radio (a/b) with two submit buttons `action=save` and `action=save_and_sync`; rows with a pending resolution render "resolved — applies on next run (X)" instead of the form; empty state message.
- [X] T018 [US2] Add the `POST /ui/conflicts/{linkID}/resolve` handler in `internal/httpapi/ui.go`: parse `linkID` (non-int → 400 HTML); require `winner` ∈ {`a`,`b`} (else 400 HTML); `Store.RecordResolution`; `found == false` → `303 /ui/conflicts?msg=stale`; if `action == "save_and_sync"` call `s.engine.Trigger("manual", s.defaultDryRun)` and on `ErrAlreadyRunning` → `303 ?msg=already-running`; otherwise `303 /ui/conflicts?msg=resolved`.
- [X] T019 [P] [US2] Tests in `internal/httpapi/ui_test.go`: conflicts list renders both sides and at least one diff marker; `save` → 303 `?msg=resolved` and `RecordResolution` called with the right link/winner; `save_and_sync` also calls `Trigger`; fake `RecordResolution` returning `found=false` → `?msg=stale`; missing/invalid `winner` → 400; no conflicts → empty-state text.

**Checkpoint**: US1 + US2 both work independently → MVP complete.

---

## Phase 5: User Story 3 - Start a dry run to preview changes safely (Priority: P2)

**Goal**: The trigger control's dry-run option produces a preview run that writes
nothing and is labelled unmistakably.

**Independent Test**: Tick Dry run + Trigger → result labelled "DRY RUN / preview";
`/conflicts` and `/sync/last` show no new writes; neither account changed.

**Depends on**: US1 (edits `POST /ui/sync` and `dashboard.tmpl`).

### Implementation for User Story 3

- [X] T020 [US3] Update the `POST /ui/sync` handler in `internal/httpapi/ui.go`: read the `dryRun` form field; effective `dryRun := formChecked || s.defaultDryRun`; pass it to `s.engine.Trigger("manual", dryRun)` (a globally dry deployment cannot be overridden to a real run).
- [X] T021 [P] [US3] Update `internal/httpapi/templates/dashboard.tmpl`: render a prominent `.dry-run` badge ("DRY RUN — preview, nothing was written") whenever `Summary.DryRun` is true; ensure the trigger form's `dryRun` checkbox has a clear label.
- [X] T022 [P] [US3] Tests in `internal/httpapi/ui_test.go`: `POST /ui/sync` with `dryRun=on` → fake Engine records `dryRun == true`; with `defaultDryRun == true` and the box unchecked → still `true`; `GET /ui` with a summary carrying `dryRun:true` → badge text present.

**Checkpoint**: Dry-run preview available from the UI.

---

## Phase 6: User Story 4 - Access is protected when a token is configured (Priority: P2)

**Goal**: When `SYNC_API_TOKEN` is set, `/ui/*` (except `/ui/login`) requires a
signed-in session cookie; a sign-in form takes the shared secret; logout works.
When the token is unset, the UI is open.

**Independent Test**: Token set + fresh browser → `/ui` redirects to `/ui/login`;
wrong secret → error banner, no cookie; correct secret → cookie + `/ui`; navigate
freely; logout → cookie cleared, back to login. Token unset → no sign-in step.

**Depends on**: Foundational (T008's `uiProtected` seam). Independent of US1–US3
except for shared files (`ui.go`, `server.go`, `ui_test.go`).

### Implementation for User Story 4

- [X] T023 [P] [US4] Create `internal/httpapi/ui_session.go`: `session{id string; createdAt, lastSeen time.Time}` and `sessionStore` (`sync.Mutex` + `map[string]*session`) with `create() string` (256-bit `crypto/rand` id, base64url), `valid(id string) bool` (present and within a 30-day sliding window; renew `lastSeen`; delete on expiry; opportunistic sweep), `revoke(id string)`; plus `setSessionCookie` / `clearSessionCookie` helpers (`HttpOnly`, `SameSite=Lax`, `Path=/ui`, `Max-Age` 30d, `Secure` when `r.TLS != nil` or `X-Forwarded-Proto == "https"`).
- [X] T024 [P] [US4] Table-driven tests in `internal/httpapi/ui_session_test.go`: `valid` true right after `create`; false past TTL (inject a clock or set `lastSeen` back); sliding renewal keeps it valid; `revoke` makes it invalid; expired entries are swept.
- [X] T025 [US4] Implement `uiProtected` in `internal/httpapi/server.go`: if `s.token == ""` pass through; else read the session cookie and require `s.sessions.valid(id)`, otherwise `303 /ui/login`. Construct the `sessionStore` in `httpapi.New` and store it on `Server`.
- [X] T026 [US4] Add the auth handlers in `internal/httpapi/ui.go`: `GET /ui/login` renders `login.tmpl` (or `303 /ui` when `s.token == ""`, mapping `?msg=bad-credentials`); `POST /ui/login` compares `secret` to `s.token` with `crypto/subtle.ConstantTimeCompare` → on match `create` + `setSessionCookie` + `303 /ui`, else `303 /ui/login?msg=bad-credentials`; `POST /ui/logout` → `revoke` + `clearSessionCookie` + `303 /ui/login`.
- [X] T027 [P] [US4] Create `internal/httpapi/templates/login.tmpl`: one `<input type="password" name="secret">`, a submit button, and a `bad-credentials` error banner; no value is ever echoed back.
- [X] T028 [P] [US4] Tests in `internal/httpapi/ui_test.go`: token unset → `GET /ui` 200 and `GET /ui/login` → 303 `/ui`; token set + no cookie → `GET /ui` 303 `/ui/login`; wrong secret → 303 `?msg=bad-credentials` and no `Set-Cookie`; right secret → `Set-Cookie` + 303 `/ui`; request with a valid cookie → `GET /ui` 200; `POST /ui/logout` → expired cookie + 303; assert no response body contains the token or a password.

**Checkpoint**: All four stories independently functional.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T029 [P] Run `gofmt -l .` and `go vet ./...`; fix any findings; delete leftover comments referring to the removed construction-time `dryRun`.
- [X] T030 [P] Add a short "Web UI" section to `README.md`: the `/ui` routes, sign-in behaviour when `SYNC_API_TOKEN` is set, and the dry-run toggle; note `SYNC_DRY_RUN` forces every UI run to a dry run.
- [X] T031 Run `make test` (full suite) and the JSON-API regression checks from `quickstart.md` ("Regression check — JSON API unchanged").
- [X] T032 Execute `specs/002-sync-conflict-ui/quickstart.md` Scenarios A–D end to end, once with `SYNC_API_TOKEN` unset and once set.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: after Setup. Blocks all user stories. Internal order: T003 → T004; T003 → T006 → T007; T005 after T003 + T006; T008 after T002 + T006.
- **US1 (Phase 3)**: after Foundational.
- **US2 (Phase 4)**: after Foundational. Independent of US1 (different primary files) — can run in parallel with US1 if two people, but both touch `ui.go` / `ui_test.go`, so coordinate.
- **US3 (Phase 5)**: after US1 (edits the same handler and template).
- **US4 (Phase 6)**: after Foundational. Independent of US1–US3 apart from shared `ui.go` / `server.go` / `ui_test.go`.
- **Polish (Phase 7)**: after every story being shipped.

### Within Each User Story

- Pure helpers before the handlers that use them (T014 before T016/T018).
- Handler before its dedicated test where the test asserts on real behaviour;
  `[P]` test tasks are written against the handler landed earlier in the phase.
- Story complete and its checkpoint validated before starting the next priority.

### Parallel Opportunities

- Setup: T001 and T002 together.
- Foundational: T004 can proceed once T003 lands, in parallel with T006's edits.
- US1: T010 (template) alongside T009 (handler); T013 after both.
- US2: T014 + T015 (diff + its tests) alongside T017 (template).
- US3: T021 + T022 alongside each other after T020.
- US4: T023, T024, T027 in parallel; T025/T026 after T023.
- Cross-story: after Foundational, US2 and US4 can be built in parallel with US1
  by separate developers (watch the shared `ui.go` / `ui_test.go`).

---

## Parallel Example: User Story 2

```bash
# Diff helper and its tests, plus the template, in parallel:
Task: "T014 Create internal/httpapi/ui_diff.go with diffSides(...)"
Task: "T015 Table-driven tests in internal/httpapi/ui_diff_test.go"
Task: "T017 Create internal/httpapi/templates/conflicts.tmpl"
# Then T016 (handler) and T018 (resolve handler), then T019 (handler tests).
```

---

## Implementation Strategy

### MVP (both P1 stories)

1. Phase 1: Setup.
2. Phase 2: Foundational — engine seam + wiring; confirm JSON API unchanged.
3. Phase 3: US1 — trigger + result view.
4. Phase 4: US2 — conflict review + resolve.
5. **STOP and VALIDATE**: `quickstart.md` Scenarios A and C (token unset).
6. Deploy/demo — a working browser UI for the two things the maintainer asked for.

### Incremental Delivery

1. Setup + Foundational → foundation ready.
2. US1 → validate → demo.
3. US2 → validate → demo (MVP complete).
4. US3 → dry-run preview → validate → demo.
5. US4 → gated access → validate → demo.
6. Polish.

---

## Notes

- No new Go dependency, no DB migration, no change to any existing JSON route's
  behaviour (only an added line in the `GET /` index list).
- `[P]` = different files, no dependency on an incomplete task.
- Constitution III: tests are included for `ui_diff.go`, `ui_session.go`, the
  `uiProtected` middleware, and handler behaviour; templates are not unit-tested.
- Commit after each task or logical group; run `gofmt` + `go vet` before each commit.
