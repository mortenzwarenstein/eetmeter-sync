# Phase 1 Data Model: Sync & Conflict UI

## Persistent state — no changes

This feature introduces **no database schema change and no migration**. It reads
and writes existing state through the same `httpapi.Store` and `httpapi.Engine`
interfaces the JSON handlers already use:

| State | Table | Used for | Access |
|-------|-------|----------|--------|
| Latest run summary | `sync_run` (`summary` jsonb) | dashboard: trigger type, start/end, result, per-account counts, items | `Store.LatestRunSummary` |
| Conflicts | `recipe_link` where `status = 'conflict'` (+ captured A/B snapshots) | conflicts page: both versions, detected-at | `Store.ListConflicts` |
| Pending resolution | `conflict_resolution` | shown as "resolved, applies next run"; written on winner submit | `Store.RecordResolution` |

The run summary JSON shape is defined in
[`specs/001-recipe-sync/data-model.md`](../001-recipe-sync/data-model.md) and
served verbatim by `GET /sync/last`; the UI renders the same structure as HTML.

The conflict shape (`Conflict`, `ConflictSide`, `ConflictItem`) is defined in
`internal/httpapi/server.go` and reused unchanged.

## In-memory state — Operator Session

Lives only in the process; lost on restart (a single operator re-signs-in). Guards
`/ui/*` when `SYNC_API_TOKEN` is set.

**`session`**

| Field | Type | Notes |
|-------|------|-------|
| `id` | `string` | 256 bits from `crypto/rand`, URL-safe base64; the cookie value |
| `createdAt` | `time.Time` | for absolute audit only; expiry is sliding |
| `lastSeen` | `time.Time` | updated on each authenticated request |

**`sessionStore`**

- `map[string]*session` guarded by a `sync.Mutex`.
- `create() (id string)` — new random id, inserted with `createdAt = lastSeen = now`.
- `valid(id string) bool` — present and `now - lastSeen <= 30 days`; on success sets
  `lastSeen = now` (sliding renewal); on expiry deletes the entry.
- `revoke(id string)` — delete (logout).
- Opportunistic sweep of expired entries on each `valid` call; no background
  goroutine (one operator, at most a few entries).

**Cookie**: name `eetmeter_ui_session`, value = `session.id`, attributes
`HttpOnly`, `SameSite=Lax`, `Path=/ui`, `Max-Age = 30 days`, `Secure` when the
request is HTTPS or carries `X-Forwarded-Proto: https`.

## Transient view models (template inputs)

Not persisted; assembled per request from the state above. Shapes only —
implementation in `internal/httpapi/ui.go`.

**`dashboardView`**

| Field | Source |
|-------|--------|
| `HasRun bool` | `LatestRunSummary` found |
| `Running bool` | summary `result == "running"` or `Engine.Status().running` — drives the `<meta refresh>` |
| `Summary` | decoded run summary (run id, trigger, `dryRun`, result, error, started/finished, per-account `Counts`, `Items`) |
| `LabelA, LabelB string` | run summary `accounts`, falling back to configured labels |
| `Flash string` | mapped from `?msg=` |

**`conflictsView`**

| Field | Source |
|-------|--------|
| `LabelA, LabelB string` | configured labels |
| `Conflicts []conflictRow` | from `Store.ListConflicts` |
| `Flash string` | mapped from `?msg=` |

**`conflictRow`**

| Field | Source / meaning |
|-------|------------------|
| `LinkID int64` | link id (form target) |
| `Name string` | recipe name |
| `DetectedAt *time.Time` | when flagged |
| `Portions diffPair` | A vs B `numberOfPortions`, with `Differs bool` |
| `Items []itemDiffRow` | union of A/B ingredient lines, aligned by product; each marks `OnlyA` / `OnlyB` / `AmountDiffers` / `UnitDiffers` |
| `Pending string` | `"a"` / `"b"` / `""` — if set, row shows "resolved, applies next run" |

**Diff helper (`ui_diff.go`)** — pure function
`diffSides(a, b *ConflictSide) (portions diffPair, items []itemDiffRow)`:
- Portions: compare the two ints.
- Items: index each side by `productName` (fall back to `productUnitId` when
  names collide); walk the sorted union; for a product on both sides compare
  `amount` and `unitName`; otherwise mark `OnlyA` / `OnlyB`.
- Deterministic ordering (sorted by product name) so rendering and tests are
  stable.

## State transitions

The UI adds no new state machine. A winner submission is exactly the existing
transition: `conflict_resolution` upserted for a link still in `status='conflict'`;
the link itself moves to `active` only on the **next run** when the engine applies
the resolution (`ApplyResolution` in `internal/syncengine/engine.go`). "Save &
sync now" simply triggers that run immediately.
