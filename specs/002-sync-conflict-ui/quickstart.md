# Quickstart & Validation: Sync & Conflict UI

Manual walkthrough that proves the feature end to end. Run it once with
`SYNC_API_TOKEN` unset and once with it set.

## Prerequisites

- PostgreSQL reachable via `DATABASE_URL` (the existing `docker-compose.yml`
  service is fine).
- A `.env` with both accounts configured (`EETMETER_ACCOUNT_{A,B}_TOKEN` +
  `_DEVICE_ID`), as for recipe-sync.
- Go 1.25 toolchain.

## Build & run

```sh
make test        # unit tests incl. ui_test.go, ui_diff_test.go, ui_session_test.go
go run ./cmd/eetmeter-sync
```

The service listens on `HTTP_ADDR` (default `:8080`). Open `http://localhost:8080/ui`.

Contracts referenced below: [`contracts/ui.md`](./contracts/ui.md). State shapes:
[`data-model.md`](./data-model.md).

---

## Scenario A — Trigger a sync and see the result (User Story 1)

1. Open `/ui`. With no prior run, confirm a plain "no runs yet" message, not an
   error.
2. Click **Trigger sync** (dry-run unchecked). Expect a redirect back to `/ui`
   with a "sync started" banner within a couple of seconds — the page does not
   hang waiting for the run.
3. While the run is in progress, confirm the page reloads itself every few
   seconds (view source: a `<meta http-equiv="refresh">` is present).
4. When the run finishes, confirm the refresh tag is gone and the page shows:
   trigger `manual`, start and finish times, `succeeded`, and per-account counts
   for created / updated / flagged / skipped / retired. Skipped entries show
   their reason.
5. Click **Trigger sync** again immediately (before the next run would finish, if
   you can) — or trigger twice in quick succession: the second attempt redirects
   with an "a sync is already running" banner and does not start a second run.
   Confirm from the logs that only one run executed.

**Pass**: SC-001 (ack < 5 s), SC-002 (summary visible in browser only), SC-003
(completion shown within ~10 s without manual reload), SC-009 (no double run).

---

## Scenario B — Dry run (User Story 3)

1. Open `/ui`, tick **Dry run**, click **Trigger sync**.
2. When it finishes, confirm the result is labelled clearly as a **dry run /
   preview** and lists intended changes.
3. Confirm nothing changed: `GET /conflicts` and `GET /sync/last` show no new
   writes, and neither Mijn Eetmeter account was modified (check the app, or run
   a second real sync and see it still wants to make the same changes).

**Pass**: SC-006.

---

## Scenario C — Review and resolve a conflict (User Story 2)

Setup: create a conflict — edit the same linked recipe differently in both
accounts, then run a sync (`/ui`, Trigger). The run summary should show one
`flaggedConflict`.

1. Open `/ui/conflicts`. Confirm the conflicted recipe appears with account A and
   account B versions **side by side** (portions + ingredient lines), and that
   the fields/lines that differ are visually marked.
2. Choose account A as the winner and click **Save**. Expect a redirect to
   `/ui/conflicts` with a "resolution recorded" banner; the entry now shows
   "resolved — applies on next run (A)".
3. Trigger a sync from `/ui`. After it finishes, reopen `/ui/conflicts` and
   confirm the conflict is gone; check account B now matches account A.
4. Repeat the setup, but this time click **Save & sync now**. Confirm a run
   starts immediately and, once done, the conflict is resolved with no separate
   trigger step.
5. Repeat the setup once more. In a second browser tab, resolve and sync the
   conflict away. Back in the first tab (still showing the conflict), submit a
   winner: confirm a "this conflict is no longer open" banner and a refreshed
   list — not an error page.
6. With no conflicts present, confirm `/ui/conflicts` shows a plain empty state.

**Pass**: SC-004 (view + record winner < 1 min in browser), SC-005 ("resolve &
sync now" applies by end of the triggered run), FR-014 / FR-015 behaviour.

---

## Scenario D — Gated access (User Story 4)

Restart the service with `SYNC_API_TOKEN=some-long-secret` set.

1. In a fresh browser session, open `/ui`. Expect a redirect to `/ui/login`.
2. Submit a wrong secret. Expect the login page again with an error banner; no
   cookie set.
3. Submit the correct secret. Expect a redirect to `/ui` and a session cookie
   (`eetmeter_ui_session`, `HttpOnly`).
4. Navigate between `/ui` and `/ui/conflicts` and trigger a sync — no further
   sign-in prompt.
5. Click **Log out**. Expect a redirect to `/ui/login` and the cookie cleared;
   reopening `/ui` redirects to login again.
6. Confirm no page source, URL, or log line contains the secret or an account
   password.
7. Stop the service, unset `SYNC_API_TOKEN`, restart. Confirm `/ui` loads with no
   sign-in step and `/ui/login` redirects to `/ui`.

**Pass**: SC-007, SC-008.

---

## Regression check — JSON API unchanged

With the service running, confirm these still behave exactly as
`001-recipe-sync/contracts/http-api.md` specifies:

```sh
curl -s localhost:8080/healthz
curl -s localhost:8080/sync/last
curl -s localhost:8080/conflicts
curl -s -X POST localhost:8080/sync         # 202 or 409
```

(When `SYNC_API_TOKEN` is set, add `-H "Authorization: Bearer $SYNC_API_TOKEN"`.)

**Pass**: FR-020.
