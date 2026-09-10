# Implementation Plan: Sync & Conflict UI

**Branch**: `002-sync-conflict-ui` | **Date**: 2026-09-10 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-sync-conflict-ui/spec.md`

## Summary

Add a minimal, server-rendered web UI to the existing eetmeter-sync service so the
maintainer can, from a browser: trigger a sync (with an optional dry-run), watch
the latest run's per-account summary, and review conflicts side by side and choose
a winner (optionally kicking off a sync straight away). It is additive — the
existing JSON endpoints are untouched. Pages are rendered with the standard
library's `html/template` from an `embed`ded template set, styled by one inline
stylesheet, with no client-side framework and no build step. When `SYNC_API_TOKEN`
is configured, the UI is gated by a sign-in form backed by an in-memory session
cookie; when it is not, the UI is open, mirroring the JSON API.

One internal seam changes: dry-run becomes a per-run argument to the sync engine
instead of a construction-time flag, so the UI can request a dry run for a single
trigger. The JSON `POST /sync` keeps its current behaviour (honours the
`SYNC_DRY_RUN` env default).

## Technical Context

**Language/Version**: Go 1.25 (module `github.com/mortenzwarenstein/eetmeter-sync`).

**Primary Dependencies**: none added. Standard library only: `html/template`,
`embed`, `net/http` (`ServeMux` method+wildcard routing), `crypto/rand`,
`crypto/subtle`, `time`, `log/slog`. Existing sole runtime dependency
(`github.com/jackc/pgx/v5`) is unaffected.

**Storage**: PostgreSQL, unchanged. **No schema change and no new migration.** The
UI reads existing state (`sync_run`, `recipe_link`, `conflict_resolution`) through
the same `httpapi.Store` / `httpapi.Engine` interfaces the JSON handlers use.
Sessions are in-memory only.

**Testing**: stdlib `testing` + `net/http/httptest`. Table-driven tests for the
A/B conflict diff helper and the session store; handler tests for each UI route
(happy path, auth redirect, 409-already-running, stale-conflict) using fake
Engine/Store.

**Target Platform**: same Linux container in the maintainer's k3s cluster, behind
a cluster Ingress that terminates TLS.

**Project Type**: single Go module — a long-lived web service. This feature adds
HTML handlers inside the existing `internal/httpapi` package.

**Performance Goals**: not performance-sensitive. One operator; a few hundred
recipes; a handful of conflicts rendered on one page with no pagination. The
result view refreshes every few seconds while a run is in progress (meets SC-003's
10-second bound).

**Constraints**:
- Existing JSON routes (`/healthz`, `/`, `/sync`, `/sync/last`, `/conflicts`,
  `/conflicts/{linkID}/resolve`) keep their current behaviour (FR-020).
- Core actions (trigger, resolve, sign in) work with plain form submission; only
  the auto-refresh of the result view uses a client-side mechanism (FR-021) — and
  that mechanism is a `<meta http-equiv="refresh">`, not scripting.
- No account password or the API token appears in any rendered page, URL, or log
  line (FR-019, SC-008).
- At most one sync runs at a time — already enforced by the engine mutex; the UI
  surfaces the "already running" case (FR-004, FR-014).

**Scale/Scope**: 1 operator, 4 UI pages (dashboard, conflicts, sign-in, plus the
implicit "already running" / flash states). ~400–600 LOC including templates and
tests.

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

Constitution v1.0.0.

### I. Simplicity & YAGNI (KISS)

| Check | Status |
|-------|--------|
| Only current, stated requirements are built | PASS — scope is exactly the spec's four stories (trigger + result, resolve, dry-run toggle, gated access). Out-of-scope list is explicit: no config UI, no run history, no retired-link management, no recipe editing, no multi-user. |
| Fewest moving parts among options that satisfy the requirement | PASS — server-rendered `html/template` (vs. an SPA + build step + JS deps), one inline stylesheet (vs. a CSS framework), `<meta refresh>` while running (vs. JS polling / SSE / WebSocket), in-memory sessions (vs. a sessions table or a signed-JWT scheme with key management). Each alternative is named and rejected in `research.md`. |
| stdlib is the default; every third-party dep justified in its commit | PASS — **zero** new dependencies. |
| No abstraction/interface/knob without a present need | PASS — no new config knob (session TTL is a hardcoded 30-day sliding window; add an env var only if a need appears). No new store interface — the UI reuses `httpapi.Store` and `httpapi.Engine`. The one signature change (per-run dry-run) is forced by FR-008, not speculative. |
| Dead code deleted, not parked | PASS (discipline item). The construction-time `Engine.dryRun` field is removed, not left alongside the new parameter. |

### II. Don't Assume

| Check | Status |
|-------|--------|
| Unclear requirements confirmed before building | PASS — the four product decisions (UI served by the same service; scope = the two actions + dry-run toggle; sign-in form + session cookie when a token is set; "resolve & sync now") were put to the maintainer as questions and are recorded in the spec's Assumptions. |
| External API behavior inspected against real data before parsing code | N/A — this feature calls no external API. It drives the already-built sync engine and reads already-defined store state. |
| Assumptions recorded where they live | PASS — spec Assumptions section; `research.md` records the session, CSRF, and auto-refresh decisions with rationale; the session TTL and the "UI dry-run toggle OR global dry-run" rule are noted in `research.md` and will carry a code comment at the decision site. |

### III. Pragmatic Testing

| Check | Status |
|-------|--------|
| Non-trivial logic has automated tests | PASS (planned) — the A/B field-diff helper (`ui_diff.go`) and the in-memory session store (expiry, sliding renewal, logout revocation) are the non-trivial pieces and get table-driven tests. |
| Every bug fix adds a failing-then-passing test | PASS (discipline item). |
| Trivial glue not over-tested | PASS — template rendering gets one assertion per route (status + a key string / redirect target); struct-to-view mapping is untested. |
| No test-first mandate, no coverage target imposed | PASS — none imposed. |

### IV. Idiomatic Go

| Check | Status |
|-------|--------|
| `gofmt` + `go vet` clean before commit | PASS (discipline item). |
| Standard project layout and naming | PASS — new files live in the existing `internal/httpapi` package; templates in `internal/httpapi/templates/` behind `embed.FS`. No bespoke structure. |
| Clear over clever; exported identifiers documented | PASS (discipline item). |

**Gate result: PASS.** No violations; Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/002-sync-conflict-ui/
├── plan.md              # this file
├── spec.md              # feature spec
├── research.md          # Phase 0 — rendering, session/auth, CSRF, auto-refresh, dry-run wiring
├── data-model.md        # Phase 1 — reused state, in-memory session, template view models
├── quickstart.md        # Phase 1 — per-story manual validation (token set and unset)
├── contracts/
│   └── ui.md            # Phase 1 — the UI routes, forms, redirects, cookie, auth rule
└── checklists/
    └── requirements.md  # spec quality checklist (from /speckit-specify)
```

### Source Code (repository root)

```text
cmd/
└── eetmeter-sync/
    └── main.go                     # CHANGED: pass cfg.DryRun to engine.Trigger calls;
                                    # pass account labels + default dry-run into httpapi.New

internal/
├── syncengine/
│   ├── engine.go                   # CHANGED: Trigger(trigger string, dryRun bool) and
│   │                               # RunSync(ctx, trigger string, dryRun bool); thread dryRun
│   │                               # through execute/apply; drop construction-time e.dryRun
│   └── engine_test.go              # CHANGED: updated call sites
├── scheduler/
│   └── scheduler.go                # unchanged (its callback signature already opaque);
│                                   # main.go's closure passes cfg.DryRun
└── httpapi/
    ├── server.go                   # CHANGED: register /ui/* routes; New() gains
    │                               #   defaultDryRun bool, labelA, labelB string;
    │                               #   JSON handleSync passes defaultDryRun
    ├── handlers.go                 # CHANGED: handleSync passes the dry-run default
    ├── adapter.go                  # unchanged
    ├── ui.go                       # NEW: HTML handlers — dashboard, conflicts, sync,
    │                               #   resolve, login, logout; flash via ?msg=
    ├── ui_session.go               # NEW: in-memory session store (crypto/rand id,
    │                               #   30-day sliding TTL, logout revokes) + cookie helpers
    ├── ui_diff.go                  # NEW: pure A/B recipe field/item diff for the conflict view
    ├── ui_test.go                  # NEW: route + auth-middleware tests (httptest + fakes)
    ├── ui_diff_test.go             # NEW: table-driven diff tests
    └── templates/                  # NEW: embed.FS
        ├── layout.tmpl             # shell + inline <style> + optional <meta refresh>
        ├── dashboard.tmpl          # last-run summary, trigger form, dry-run checkbox
        ├── conflicts.tmpl          # conflict list, side-by-side A/B, winner form
        └── login.tmpl              # single-secret sign-in form
```

**Structure Decision**: The UI is HTML handlers added to the existing
`internal/httpapi` package, not a new package or a separate frontend project.
That is the smallest division that works: the handlers reuse the package's
`Store` and `Engine` interfaces and its JSON helpers, and share the one
`http.ServeMux`. Templates are `embed`ded so the binary stays a single artifact
with no runtime file dependency, matching how migrations are already embedded in
`internal/store`. New concerns each get their own file (`ui.go`, `ui_session.go`,
`ui_diff.go`) so the pure, testable pieces (session lifecycle, field diff) stay
isolated from request plumbing.

## Complexity Tracking

No constitution violations. Nothing to justify.

## Phase sequence for implementation (feeds `/speckit-tasks`)

1. **Per-run dry-run seam** — change `Engine.Trigger` / `RunSync` to take
   `dryRun bool`, thread it through `execute`/`apply`, drop `e.dryRun`; fix
   `engine_test.go` and `main.go`. JSON `POST /sync` behaviour unchanged.
2. **Session store** — `ui_session.go`: create/lookup/renew/revoke, TTL sweep,
   cookie read/write helpers; tests.
3. **Auth middleware** — `/ui/*` gate: open when no token; else valid cookie or
   303 to `/ui/login`. `GET`/`POST /ui/login`, `POST /ui/logout`.
4. **Templates + layout** — `layout.tmpl` (inline CSS, conditional meta-refresh),
   `login.tmpl`.
5. **Dashboard** — `GET /ui`, `POST /ui/sync` (dry-run checkbox → 303 with
   `?msg=`), last-run summary rendering, "no runs yet" empty state, meta-refresh
   while running.
6. **Conflict diff** — `ui_diff.go` + table tests.
7. **Conflicts page** — `GET /ui/conflicts`, side-by-side A/B with differences
   marked, winner form with "Save" and "Save & sync now", stale-conflict and
   empty-state handling, 303 with `?msg=`.
8. **Wiring** — `httpapi.New` signature, `main.go` passes labels + default
   dry-run; `GET /` index line pointing to `/ui`.
9. **Quickstart walkthrough** — run through `quickstart.md` end to end with
   `SYNC_API_TOKEN` set and unset.
