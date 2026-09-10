# Quickstart & Validation: Recipe Sync

How to run `eetmeter-sync` locally and prove each user story works. Details of the
HTTP surface and data shapes live in `contracts/` and `data-model.md`.

## Prerequisites

- Go 1.25+ (1.26.x toolchain).
- A PostgreSQL database you can throw away. Example with Docker:
  ```sh
  docker run -d --name eetmeter-pg -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:17
  export DATABASE_URL='postgres://postgres:dev@localhost:5432/postgres?sslmode=disable'
  ```
- Two Mijn Eetmeter accounts you control (yours + partner's), or two throwaway
  accounts for the spike.

## Configure

```sh
export EETMETER_ACCOUNT_A_EMAIL='you@example.com'
export EETMETER_ACCOUNT_A_PASSWORD='...'
export EETMETER_ACCOUNT_A_LABEL='Morten'
export EETMETER_ACCOUNT_B_EMAIL='partner@example.com'
export EETMETER_ACCOUNT_B_PASSWORD='...'
export EETMETER_ACCOUNT_B_LABEL='Partner'
export SYNC_DAILY_TIME='06:00'
export SYNC_TIMEZONE='Europe/Amsterdam'
export SYNC_ON_STARTUP='false'
export HTTP_ADDR=':8080'
# export SYNC_API_TOKEN='local-dev-token'   # optional
```

Full variable list: `contracts/http-api.md` → "Runtime configuration".

## Run

```sh
go run ./cmd/eetmeter-sync
# migrations apply on boot; server listens on :8080; daily scheduler armed
```

Trigger a sync by hand and read the result:

```sh
curl -s -X POST localhost:8080/sync            # -> 202 { "runId": ... }
curl -s localhost:8080/sync/last | jq          # -> run summary
```

(With `SYNC_API_TOKEN` set, add `-H "Authorization: Bearer local-dev-token"`.)

## Step 0 — API spike (do this first, it feeds the client)

Goal: replace the UNVERIFIED sections of `contracts/eetmeter-api.md` with real
request/response captures, saved as fixtures under
`internal/eetmeter/testdata/`.

1. `POST account/credentials` for a throwaway account; confirm the token +
   `Authorization: Basic <token>:<deviceId>` scheme.
2. `GET combinedproduct`; save the response. Confirm every field in the contract.
3. Create a recipe through the real mobile app, then diff `GET combinedproduct`
   to see the exact stored shape.
4. Attempt a programmatic create (`POST combinedproduct`, then alternatives);
   record method, path, body, response, and whether the new `id` comes back.
5. Attempt a programmatic update; record whether it is whole-list replace or
   line-level, and whether per-line `id`s are required.
6. Create a recipe in account B using `productUnitId` / `brandProductId` values
   copied verbatim from an account A recipe; confirm it is accepted and reads
   back equivalent.

**Expected outcome**: `contracts/eetmeter-api.md` updated with confirmed shapes;
fixtures committed; `internal/eetmeter` client tests run against them.

## Story validation

Each maps to a user story in `spec.md`. Run against two accounts whose recipe
lists you can inspect in the app.

### US1 — New recipe propagates (P1)

1. In account A, create recipe **"Testbami"** (not present in B). In B, create
   **"Testsoep"** (not present in A).
2. `POST /sync`.
3. `GET /sync/last`: `counts.createdInB >= 1` and `counts.createdInA >= 1`;
   `items` contains `{name:"Testbami",action:"createdInB"}` and
   `{name:"Testsoep",action:"createdInA"}`.
4. In the app: "Testbami" now in account B, "Testsoep" now in account A, contents
   equivalent.
5. `POST /sync` again → both appear as `noop`; no duplicates created.

**Pass**: both recipes exist on both sides after one sync; second sync is a no-op.

### US2 — Edits propagate (P2)

1. With "Testbami" linked on both sides, in account A change its
   `numberOfPortions` from 4 to 6.
2. `POST /sync`.
3. `GET /sync/last`: `items` has `{name:"Testbami",action:"updatedInB"}`.
4. App: account B's "Testbami" now shows 6 portions; nothing else changed.
5. Reverse: edit an ingredient amount in account B, sync, confirm
   `action:"updatedInA"` and account A reflects it.
6. Make account A's copy byte-identical to B's current content, sync → `noop`,
   nothing written.

**Pass**: one-sided edits land on the other side within one sync; no false
positives.

### US3 — Conflicting edits are flagged (P3)

1. With "Testbami" agreed on both sides, edit it **differently** in A and in B
   (e.g. A → 8 portions, B → add an ingredient). Do not sync in between.
2. `POST /sync`.
3. `GET /sync/last`: `items` has `{name:"Testbami",action:"flaggedConflict"}`,
   `counts.flaggedConflicts >= 1`. App: neither copy changed.
4. `GET /conflicts`: one entry `name:"Testbami"` with both current versions.
5. `POST /conflicts/<linkId>/resolve` with `{"winner":"a"}`.
6. `POST /sync`.
7. `GET /sync/last`: `items` has `{name:"Testbami",action:"resolutionApplied"}`.
   App: account B now matches account A; `GET /conflicts` is empty.

**Pass**: dual edits never overwrite; conflict is listed; chosen side wins on the
next run.

### US4 — On-demand trigger + summary (P2)

1. `POST /sync` → `202` with a `runId`.
2. Immediately `POST /sync` again → `409 { "error": "sync already running" }`.
3. After it finishes, `GET /sync/last` shows per-account `counts` and an `items`
   list; `result: "succeeded"`.
4. Open `http://localhost:8080/sync` in a browser (GET) → a run starts (or `409`
   if one is going).

**Pass**: manual trigger works from browser and curl; concurrent trigger is
rejected; summary is retrievable.

### Edge checks (abbreviated)

- **Deletion**: delete a linked recipe in account A; sync → `action:"linkRetired"`
  for that name; account B's copy untouched; subsequent syncs leave it alone.
- **Duplicate name**: create two recipes both named "Dubbel" in account A; sync →
  `action:"skipped"`, `detail` mentions the duplicate; nothing copied for that
  name.
- **Auth failure**: set `EETMETER_ACCOUNT_B_PASSWORD` wrong; sync →
  `result:"failed"`, `error` set, no recipe created or changed in either account,
  `error` contains no password.
- **Crash mid-run**: kill the process during a sync that is creating recipes;
  restart; `POST /sync`; confirm the run reconciles (re-links the
  already-created recipe or flags it) and no recipe is left half-written.

## Automated tests

```sh
go test ./...                              # unit tests (engine, fingerprint, client, http)
TEST_DATABASE_URL="$DATABASE_URL" go test ./internal/store/...   # store integration tests
```

Test scope and rationale: `research.md` §12.

## Container build

```sh
docker build -t eetmeter-sync:dev .
docker run --rm -p 8080:8080 --env-file ./local.env eetmeter-sync:dev
```

Kubernetes manifests are maintained in a separate repo and are out of scope here;
the image needs only the environment variables above, a reachable Postgres, and a
liveness probe on `GET /healthz`.
