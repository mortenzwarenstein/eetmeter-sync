# Data Model: Recipe Sync

**Feature**: 001-recipe-sync | **Date**: 2026-09-10

Two kinds of data:

1. **Remote** — recipes as they exist in each Mijn Eetmeter account. Not stored by
   this service beyond a fingerprint; fetched fresh every run.
2. **Local** — the sync bookkeeping, in PostgreSQL.

## Remote (Mijn Eetmeter) — reference only

### Recipe (`combinedproduct`)

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | Server-assigned, **per account**. Never copied between accounts. |
| `name` | string | Match key across accounts. May contain a Vreetmeter metadata marker; treated verbatim. |
| `numberOfPortions` | int | Part of portable content. |
| `items` | list of Ingredient | Part of portable content. |

### Ingredient

| Field | Type | Notes |
|-------|------|-------|
| `id` | UUID | Per-account line id. Excluded from fingerprint; not copied. |
| `productUnitId` | UUID | Reference into the shared Voedingscentrum food DB. Copied verbatim. |
| `brandProductId` | UUID or null | Reference for brand products. Copied verbatim. |
| `brandName` | string or null | Display text. |
| `productName` | string | Display text; included in fingerprint as a tie-breaker. |
| `unitName` | string | e.g. "gram", "portie". |
| `amount` | number | Quantity; fingerprinted at 3-decimal precision. |

**Portable content** = `numberOfPortions` + normalized `items`. This is what the
fingerprint covers and what a create/update on the other account must reproduce.

## Local (PostgreSQL)

### `account`

The two accounts, seeded from environment configuration on startup (not managed
via the API). Kept in a table so links can foreign-key to a stable id.

| Column | Type | Constraints |
|--------|------|-------------|
| `id` | text | PK. Fixed values `a` and `b`. |
| `label` | text | Human label for summaries (defaults to the id). |
| `email` | text | NOT NULL. The login; password is **not** stored in the DB, only in process memory from the environment. |
| `created_at` | timestamptz | NOT NULL default `now()` |
| `updated_at` | timestamptz | NOT NULL default `now()` |

Exactly two rows. Startup upserts `a` and `b` from config; if an `email` changes,
the row is updated and a warning is logged (existing links are kept — they key on
recipe name, not account identity).

### `recipe_link`

One row per recipe that sync knows about — i.e. per distinct recipe `name` that
has been seen on at least one side and linked.

| Column | Type | Constraints |
|--------|------|-------------|
| `id` | bigint | PK, identity. |
| `name` | text | NOT NULL, UNIQUE. The match key. |
| `a_recipe_id` | uuid | NULL until the recipe exists in account A. |
| `b_recipe_id` | uuid | NULL until the recipe exists in account B. |
| `a_baseline_hash` | bytea | Fingerprint of account A's content as of the last successful sync of this pair. NULL if never synced on A. |
| `b_baseline_hash` | bytea | Same for account B. |
| `status` | text | NOT NULL. One of `active`, `conflict`, `retired`. |
| `last_synced_at` | timestamptz | When this pair last reached agreement. NULL if never. |
| `conflict_detected_at` | timestamptz | Set when `status` becomes `conflict`, cleared otherwise. |
| `note` | text | Free text for the summary, e.g. `"duplicate name in account A — skipped"`. |
| `created_at` | timestamptz | NOT NULL default `now()` |
| `updated_at` | timestamptz | NOT NULL default `now()` |

**Status meaning**

| Status | Meaning | How it is left / exited |
|--------|---------|--------------------------|
| `active` | Both sides agree, or only one side exists yet and is being propagated normally. | Stays `active` after each clean sync. |
| `conflict` | Both sides changed since the last successful sync and differ. Neither side is written. | Cleared to `active` when a recorded resolution is applied on a later run, or when the maintainer edits one side so the two agree again. |
| `retired` | The recipe was deleted on one side. No further syncing for this name; the surviving side is left untouched. | Terminal. If a recipe with the same name reappears on the deleted side, a **new** link is *not* created for the existing name while a `retired` row holds it — the reappearance is reported and ignored until the maintainer deletes the `retired` row. (Keeps a resurrected deletion from silently re-propagating.) |

**State transitions (per run, per link)**

```
                         one side changed
        ┌───────────────────────────────────────────────┐
        │                                               ▼
   [active] ──both sides changed & differ──► [conflict] ──resolution applied──► [active]
        │                                               │
        │                                     sides made equal again
        │                                               │
        │◄──────────────────────────────────────────────┘
        │
        └── a linked side's recipe vanished ──► [retired]  (terminal)
```

### `sync_run`

One row per run, scheduled or manual. Only the latest is required to be
queryable (FR-016); older rows may be pruned but pruning is not required.

| Column | Type | Constraints |
|--------|------|-------------|
| `id` | uuid | PK, generated at run start. |
| `trigger` | text | `scheduled` or `manual`. |
| `started_at` | timestamptz | NOT NULL. |
| `finished_at` | timestamptz | NULL while running. |
| `status` | text | `running`, `succeeded`, `failed`. |
| `error` | text | NULL unless `failed`; human-readable, **never contains a password**. |
| `summary` | jsonb | The per-account outcome (schema below). Upserted as the run progresses. |

Exactly one row may have `status = 'running'` at a time (enforced by the
in-process mutex, and defensively by a partial unique index
`CREATE UNIQUE INDEX one_running_sync ON sync_run ((status)) WHERE status = 'running'`).

### `conflict_resolution`

A pending decision recorded via the HTTP API, consumed by the next run.

| Column | Type | Constraints |
|--------|------|-------------|
| `link_id` | bigint | PK, FK → `recipe_link.id`. At most one pending resolution per link. |
| `winner` | text | `a` or `b`. |
| `requested_at` | timestamptz | NOT NULL default `now()`. |

Deleted in the same transaction that applies it. If the referenced link is no
longer in `conflict` when the run reaches it (e.g. the maintainer already made the
sides agree), the row is discarded and noted in the summary.

### `schema_migrations`

| Column | Type | Constraints |
|--------|------|-------------|
| `version` | int | PK. |
| `applied_at` | timestamptz | NOT NULL default `now()`. |

## Run summary JSON (`sync_run.summary`)

```json
{
  "accounts": { "a": { "label": "Morten" }, "b": { "label": "Partner" } },
  "counts": {
    "createdInA": 0, "createdInB": 2,
    "updatedInA": 1, "updatedInB": 0,
    "flaggedConflicts": 1,
    "skipped": 1,
    "linksRetired": 0,
    "resolutionsApplied": 0
  },
  "items": [
    { "name": "Bami", "action": "createdInB" },
    { "name": "Havermout", "action": "updatedInA" },
    { "name": "Curry", "action": "flaggedConflict",
      "detail": "changed on both sides since 2026-09-08T06:00:00Z" },
    { "name": "Soep", "action": "skipped",
      "detail": "duplicate name in account A" }
  ],
  "startedAt": "2026-09-10T06:00:00Z",
  "finishedAt": "2026-09-10T06:00:12Z",
  "trigger": "scheduled",
  "result": "succeeded"
}
```

`action` values: `createdInA`, `createdInB`, `updatedInA`, `updatedInB`,
`flaggedConflict`, `resolutionApplied`, `linkRetired`, `skipped`, `linked`
(first-time link, no content change), `noop`.

## Validation rules (from spec requirements)

- **FR-003**: `recipe_link.name` is UNIQUE — a name maps to exactly one link.
- **FR-003**: a name seen on both sides for the first time creates one link with
  both `*_recipe_id` set; equal fingerprints → `active`, unequal → `conflict`.
- **FR-005**: a name on exactly one side with no link → create on the other side,
  then insert the link with both baselines = the source fingerprint.
- **FR-007 / FR-010**: entering `conflict` writes nothing to either account and
  never deletes.
- **FR-011**: if `a_recipe_id` (or `b_recipe_id`) was non-NULL on the link but the
  recipe is absent from the live list this run, set `status = 'retired'`; do not
  clear the surviving `*_recipe_id`, do not write the surviving side.
- **FR-014**: at most one `sync_run.status = 'running'`.
- **FR-017 / FR-020 / SC-007**: `sync_run.error` and all log output are scrubbed
  of passwords; only `email` / `label` may appear.
- **FR-019**: a create/update must send the full portable content; after writing,
  the far side is re-fetched and its fingerprint stored as the new baseline (so a
  lossy round-trip surfaces immediately as a mismatch rather than silently).
- **FR-021**: all of the above tables are in PostgreSQL and survive restarts.
