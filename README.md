# eetmeter-sync

Keeps the **recipe** collections of two [Mijn Eetmeter](https://mijn.voedingscentrum.nl)
accounts in sync — both directions — so a couple sharing meal plans doesn't have
to re-enter every recipe twice.

- New recipe in either account → copied to the other on the next sync.
- Edit on one side → propagated to the other.
- Both sides edited the same recipe → flagged as a conflict, nothing overwritten,
  resolved by you via an HTTP call.
- Deletions are never propagated.

Runs as one small Go service: an HTTP API plus a daily scheduled sync. State
lives in PostgreSQL.

> Status: **all Mijn Eetmeter API paths are confirmed against the live API**
> (`internal/eetmeter/live_test.go` — a read test and a self-cleaning
> create→update→delete write test). Auth is a device-bound **token + device id**
> captured from the app — a bare password login is not reproducible from a
> server, so `EETMETER_ACCOUNT_x_TOKEN` + `_DEVICE_ID` are how you connect.
> A full engine run against a real account (44 recipes) links cleanly with no
> writes. It also runs end-to-end via `make dev` against the bundled fake API.

## Quick start (no accounts needed)

Needs Go 1.25+ and Docker.

```sh
make dev
```

This starts PostgreSQL (in Docker), a **fake Eetmeter API** with seeded recipes,
and the service — then runs one sync immediately. Then, in another terminal:

```sh
curl -s localhost:8080/sync/last | jq      # what the last run did
curl -s -X POST localhost:8080/sync        # run another
open http://localhost:8080/sync            # or just trigger it from a browser
```

`Ctrl-C` stops everything. `make db-reset` wipes the database.

## Run against your real accounts

For each account, capture a `token` + `deviceId` pair from a logged-in Mijn
Eetmeter app session (e.g. with an HTTPS proxy — the `Authorization: Basic
<token>:<deviceId>` header on any request). Then:

```sh
cp .env.example .env      # then fill EETMETER_ACCOUNT_{A,B}_TOKEN + _DEVICE_ID
make run                  # starts Postgres in Docker, the app on the host
```

The first sync merges both collections in both directions and does not propagate
deletes, so preview it first: set `SYNC_DRY_RUN=1` (and `SYNC_ON_STARTUP=1`),
`make run`, read the `sync run finished` log / `GET /sync/last`, then unset
`SYNC_DRY_RUN` when it looks right.

Or build the container:

```sh
make docker-build
docker run --rm -p 8080:8080 --env-file .env eetmeter-sync:dev
```

Kubernetes manifests are intentionally out of scope here (kept in a separate
repo). The container needs only the environment below, a reachable Postgres, and
a liveness probe on `GET /healthz`.

## HTTP API

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/healthz` | liveness; pings the database |
| `POST` / `GET` | `/sync` | start a sync now → `202` (or `409` if one is running) |
| `GET` | `/sync/last` | summary of the most recent run |
| `GET` | `/conflicts` | unresolved conflicts, both versions inline |
| `POST` | `/conflicts/{linkID}/resolve` | body `{"winner":"a"\|"b"}` or `?winner=a`; applied on the next run |

If `SYNC_API_TOKEN` is set, every route except `/healthz` requires
`Authorization: Bearer <token>`.

Full request/response shapes: [`specs/001-recipe-sync/contracts/http-api.md`](specs/001-recipe-sync/contracts/http-api.md).

## Web UI

Open `http://localhost:8080/ui` in a browser for a minimal, no-JavaScript UI:

- **Dashboard** (`/ui`) — trigger a sync (with an optional **Dry run** checkbox)
  and watch the latest run's per-account summary; the page auto-refreshes while a
  run is in progress.
- **Conflicts** (`/ui/conflicts`) — every unresolved conflict with both accounts'
  current versions side by side and the differences marked; pick a winner and
  either **Save** (applied on the next sync) or **Save & sync now**.

When `SYNC_API_TOKEN` is set the UI requires a one-field sign-in (the token is
the secret); the browser stays signed in for 30 days and **Log out** ends it.
With no token configured the UI is open, like the JSON API. Setting
`SYNC_DRY_RUN` forces every UI-triggered run to a dry run regardless of the
checkbox.

Route details: [`specs/002-sync-conflict-ui/contracts/ui.md`](specs/002-sync-conflict-ui/contracts/ui.md).

## Configuration (environment)

Read from `.env` (parsed literally — `$`, `#`, quotes are safe, no shell
expansion) and then the process environment.

| Variable | Required | Default | Notes |
|----------|:--------:|---------|-------|
| `DATABASE_URL` | ✅ | — | PostgreSQL connection string |
| `EETMETER_ACCOUNT_A_TOKEN` + `_DEVICE_ID` | ✅ | — | device-bound pair from a logged-in app session — the auth path |
| `EETMETER_ACCOUNT_A_EMAIL` / `B_EMAIL` | | — | label only; unused in token mode |
| `EETMETER_ACCOUNT_A_PASSWORD` / `B_PASSWORD` | | — | unused in token mode (and bare password login is rejected by the live API anyway) |
| `EETMETER_ACCOUNT_A_LABEL` / `B_LABEL` | | `a` / `b` | shown in run summaries |
| `SYNC_DAILY_TIME` | | `06:00` | local wall-clock time of the daily run |
| `SYNC_TIMEZONE` | | `Europe/Amsterdam` | IANA name |
| `SYNC_ON_STARTUP` | | `false` | run once on boot |
| `SYNC_DRY_RUN` | | `false` | every run previews (logs what it would do, writes nothing) |
| `HTTP_ADDR` | | `:8080` | listen address |
| `SYNC_API_TOKEN` | | — | bearer token for the mutating endpoints |
| `EETMETER_API_BASE_URL` | | `https://api3-mijn.voedingscentrum.nl/api/` | point at the fake for local dev |
| `LOG_LEVEL` | | `info` | `debug` \| `info` \| `warn` \| `error` |

Passwords and the token never appear in logs, responses, or the database.

## Development

```sh
make test        # unit tests, no database
make test-all    # + store/engine integration tests (spins up Postgres)
make check       # gofmt + go vet
```

Design docs (spec, plan, data model, contracts, task list) are under
[`specs/001-recipe-sync/`](specs/001-recipe-sync/) and
[`specs/002-sync-conflict-ui/`](specs/002-sync-conflict-ui/).

Layout:

```
cmd/eetmeter-sync    the service
cmd/fake-eetmeter    in-memory stand-in for the Mijn Eetmeter API (dev + tests)
internal/config      env → Config
internal/dotenv      literal .env loader (no shell expansion)
internal/eetmeter    API client (auth, list/create/update recipes)
internal/fingerprint recipe content hash for change detection
internal/syncengine  the reconciliation: reconcile.go decides, engine.go acts
internal/store       PostgreSQL persistence + migrations
internal/httpapi     the HTTP handlers
internal/scheduler   the daily timer
```
