# Contract: eetmeter-sync Web UI

Minimal, server-rendered HTML added to the same service. Additive — every route in
[`../001-recipe-sync/contracts/http-api.md`](../001-recipe-sync/contracts/http-api.md)
keeps its current behaviour. All UI routes live under `/ui/`.

**Content type**: `text/html; charset=utf-8`.

**Auth**: when `SYNC_API_TOKEN` is set, every `/ui/*` route **except `/ui/login`**
requires a valid session cookie; without one the response is `303 See Other` to
`/ui/login`. When `SYNC_API_TOKEN` is empty, all `/ui/*` routes are open and
`GET /ui/login` redirects to `/ui`. The JSON routes' `Authorization: Bearer`
scheme is unaffected and unchanged.

**Session cookie**: `eetmeter_ui_session`, opaque value, `HttpOnly`,
`SameSite=Lax`, `Path=/ui`, `Max-Age` 30 days, `Secure` when the request is HTTPS
or carries `X-Forwarded-Proto: https`. Sliding 30-day expiry, in-memory
server-side; lost on service restart.

**Flash**: after a `POST`, handlers `303` to a `GET` with `?msg=<code>`; the page
renders a short banner. Codes: `started`, `already-running`, `resolved`, `stale`,
`bad-credentials`. Unknown/absent `msg` renders no banner.

**Secret hygiene**: no rendered page, redirect `Location`, form field value, or log
line contains an account password or `SYNC_API_TOKEN` (FR-019). The sign-in field
is `type="password"` and its value is never echoed back.

---

## `GET /ui`  — dashboard

Latest run summary + a trigger form.

- **200 OK** — renders:
  - If no run has ever completed: a "no runs yet" state (FR-007).
  - Otherwise: trigger type, started/finished timestamps, `succeeded` / `failed`
    / `running`, error text if failed, and per account the counts of created,
    updated, flagged-conflict, skipped (with reasons from `items`), and retired.
  - A **dry run** result is labelled unmistakably as a preview (FR-009).
  - A trigger form: `POST /ui/sync` with a `dryRun` checkbox.
  - A link to `/ui/conflicts`.
  - While a run is in progress the page includes `<meta http-equiv="refresh"
    content="3">` and drops it once the run finishes (FR-006, SC-003).
- **303 → /ui/login** — token configured, no valid session.

---

## `POST /ui/sync`  — trigger a sync

Form fields:

| Field | Required | Meaning |
|-------|----------|---------|
| `dryRun` | no | present/`on` ⇒ this run is a dry run. Effective dry-run = `checked OR SYNC_DRY_RUN` (a globally dry deployment cannot be overridden to a real run from the UI). |

| Response | When |
|----------|------|
| `303 → /ui?msg=started` | A run was started. |
| `303 → /ui?msg=already-running` | A sync (scheduled or manual) is already in progress; nothing else happens (FR-004). |
| `303 → /ui/login` | Token configured, no valid session. |
| `500` (HTML) | Could not start (e.g. DB down). |

Does not block until the run finishes; the dashboard's refresh shows progress.

---

## `GET /ui/conflicts`  — review conflicts

- **200 OK** — one entry per unresolved conflict (`recipe_link.status='conflict'`):
  recipe name, detected-at, and both accounts' current versions
  (`numberOfPortions` + ingredient lines) **side by side**, with differing fields
  and ingredient lines visually marked (FR-010, FR-011). Each entry has a winner
  form. An entry that already has a pending resolution is shown as "resolved —
  applies on next run" with the chosen side indicated. Empty state when there are
  no conflicts (FR-015 scenario 5 / spec edge case).
- **303 → /ui/login** — token configured, no valid session.

---

## `POST /ui/conflicts/{linkID}/resolve`  — choose a winner

`{linkID}` is the integer `recipe_link` id.

Form fields:

| Field | Required | Meaning |
|-------|----------|---------|
| `winner` | yes | `a` or `b` — which account's current version wins |
| `action` | yes | `save` (record only) or `save_and_sync` (record, then trigger a real run now) |

| Response | When |
|----------|------|
| `303 → /ui/conflicts?msg=resolved` | Resolution recorded (`action=save`), or recorded and a run started (`action=save_and_sync`). |
| `303 → /ui/conflicts?msg=already-running` | `action=save_and_sync` but a run was already in progress — the resolution is still recorded (FR-014). |
| `303 → /ui/conflicts?msg=stale` | No link with that id is currently in conflict (already resolved elsewhere, or retired) — list re-rendered, no error page (FR-015). |
| `400` (HTML) | `winner` missing or not `a`/`b`; `linkID` not an integer. |
| `303 → /ui/login` | Token configured, no valid session. |

The resolution is applied to Mijn Eetmeter on the next run, exactly as
`POST /conflicts/{linkID}/resolve` does today; `save_and_sync` only makes that run
happen immediately.

---

## `GET /ui/login`  — sign-in form

- **200 OK** — a single `type="password"` field (`secret`) and a submit button.
  Renders `?msg=bad-credentials` as an error banner.
- **303 → /ui** — `SYNC_API_TOKEN` is not set (nothing to sign in to).

---

## `POST /ui/login`

Form field: `secret` (required).

| Response | When |
|----------|------|
| `303 → /ui` + `Set-Cookie` | `secret` equals `SYNC_API_TOKEN` (constant-time compare); a session is created. |
| `303 → /ui/login?msg=bad-credentials` | Wrong secret; no session, no cookie. |
| `303 → /ui` | `SYNC_API_TOKEN` not set. |

---

## `POST /ui/logout`

| Response | When |
|----------|------|
| `303 → /ui/login` + `Set-Cookie` (expired) | Always; server-side session entry deleted if present. |

---

## Runtime configuration

No new environment variables. Behaviour derives from existing config:

| Variable | Effect on the UI |
|----------|------------------|
| `SYNC_API_TOKEN` | set ⇒ UI requires sign-in; unset ⇒ UI open. Same value used for the JSON `Authorization: Bearer` scheme. |
| `SYNC_DRY_RUN` | when truthy, every UI-triggered run is a dry run regardless of the checkbox. |
| `EETMETER_ACCOUNT_{A,B}_LABEL` | column headings / account names in the UI. |

Session TTL is a hardcoded 30-day sliding window (see `research.md` §2).
