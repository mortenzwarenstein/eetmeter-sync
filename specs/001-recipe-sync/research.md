# Research: Recipe Sync

**Feature**: 001-recipe-sync | **Date**: 2026-09-10

This document resolves the unknowns in the Technical Context and records the
design decisions taken before Phase 1.

## 1. Mijn Eetmeter API — how recipe data is reached

**Decision**: Treat "Mijn Eetmeter" as the mobile app's private JSON API at
`https://api3-mijn.voedingscentrum.nl/api/`. A "recipe" is what the API calls a
**combined product** (`combinedproduct`).

**Rationale**: The community project
[Vreetmeter](https://github.com/SimonVreman/vreetmeter) (an unofficial SwiftUI
client by Simon Vreman) exercises this API directly. Its
`EetmeterClient` / `EetmeterAPI` sources give us a concrete, working picture:

- **Base URL**: `https://api3-mijn.voedingscentrum.nl/api/`
- **Login**: `POST account/credentials` with JSON
  `{ "deviceId": <uuid>, "emailAddress": <email>, "password": <password> }`.
  Response contains a `token` string.
- **Authenticated requests**: header
  `Authorization: Basic <token>:<deviceId>` (the token and the device id used at
  login, joined by a colon — *not* standard HTTP Basic base64), plus
  `version: 4.6.0` and `platform: iOS`.
- **Auth failure**: HTTP 401 or 403.
- **List recipes**: `GET combinedproduct` →
  `{ "items": [ { "id": <uuid>, "name": <string>,
  "numberOfPortions": <int>, "items": [ <ingredient> ] } ] }`
- **Ingredient shape**:
  `{ "id": <uuid>, "brandName": <string?>, "brandProductId": <uuid?>,
  "productUnitId": <uuid>, "productName": <string>, "unitName": <string>,
  "amount": <double> }`
- `id` values are server-assigned and therefore **per account**. Ingredient
  references (`productUnitId`, `brandProductId`) point into the shared
  Voedingscentrum food database and are expected to be identical across accounts.

**Alternatives considered**:

- *Official Eetmeter XML export/import* (used by Simon Vreman's
  `eetmeter-analyser`). Rejected: it is a manual, whole-account file export with
  no programmatic write-back — unusable for an unattended two-way sync.
- *HTML scraping of the web app*. Rejected: more brittle than the JSON API and
  no simpler.

**Open risk — writing recipes is unverified**. Vreetmeter only *reads*
combined products and adds them to the diary
(`POST combinedproduct/{id}/addtodiary`). It never creates, edits, or deletes a
combined product. So the request/response shapes for **create** and **update** of
a recipe are not known from any existing source.

Per constitution Principle II ("External API responses are inspected against real
data before code is written to parse them"), the first implementation task is a
**spike against a real account** that captures, as saved fixtures:

1. `GET combinedproduct` — full response for an account with several recipes.
2. Create a recipe — method, path, request body, response (does it return the new
   `id`?). Candidates to try: `POST combinedproduct`, `PUT combinedproduct`.
3. Update a recipe — method, path, body. Candidates: `PUT combinedproduct/{id}`,
   `POST combinedproduct/{id}`.
4. Whether a recipe created in account B with ingredient `productUnitId` /
   `brandProductId` values copied verbatim from account A is accepted and reads
   back with equivalent content.

Until the spike lands, the client's write methods are written against the
best-guess shapes above and marked as unverified in code comments; the spike task
corrects them and adds the fixtures the client tests run on.

**Note on a Vreetmeter quirk**: Vreetmeter smuggles its own metadata into the
`name` field using a bracketed marker pattern. If either account has
Vreetmeter-created recipes, names may carry such a suffix. This tool treats the
`name` verbatim (marker included) as the match key and the content to copy — it
does not parse or strip markers. Recorded as an assumption in the spec.

## 2. Language, runtime, project shape

**Decision**: Go 1.25+ (build with the 1.26.x toolchain), single module, standard
`cmd/` + `internal/` layout. One long-lived process that serves HTTP and runs an
in-process daily scheduler.

**Rationale**: Fixed by the constitution (Idiomatic Go, stdlib-first) and by the
spec (a REST service that also runs on a schedule). A single binary with an
in-process timer is the fewest moving parts.

## 3. HTTP routing

**Decision**: stdlib `net/http.ServeMux` with method+wildcard patterns
(`POST /conflicts/{linkID}/resolve`), available since Go 1.22.

**Rationale**: Constitution — the standard library is the default and no
third-party router earns its place for ~6 routes.

**Alternatives considered**: `chi`, `gorilla/mux`. Rejected: unnecessary
dependency.

## 4. Persistence

**Decision**: PostgreSQL, accessed with `github.com/jackc/pgx/v5` (`pgxpool` +
hand-written SQL, no ORM). Schema created by a tiny embedded migration runner
(`embed` a set of numbered `.sql` files, apply the unapplied ones inside a
transaction, track them in a `schema_migrations` table).

**Rationale**: Chosen by the maintainer (Postgres already available in the
cluster). `pgx` is the actively maintained, de-facto standard driver; `lib/pq` is
in maintenance-only mode. This is the project's **one runtime dependency** and
will be justified in the commit that adds it, per the constitution.

**Alternatives considered**:

- *SQLite on a PersistentVolume* — fewer infra pieces, but the maintainer prefers
  Postgres.
- *Plain JSON file* — would need hand-rolled atomic writes to meet the
  crash-safety requirement (FR-018); a transactional store gives that for free.
- *An ORM (GORM, ent, sqlc)* — rejected. The query surface is a handful of
  statements; raw SQL is less to learn and less to hide behind.

## 5. Scheduling the daily run

**Decision**: An in-process goroutine. On start and after each run it computes the
next occurrence of a configured wall-clock time (`SYNC_DAILY_TIME`, e.g. `06:00`)
in a configured timezone (`SYNC_TIMEZONE`, e.g. `Europe/Amsterdam`), waits with a
`time.Timer`, then invokes the same entry point the HTTP trigger uses.

**Rationale**: Chosen by the maintainer. The process is already long-lived for
the HTTP API, so a timer adds nothing to deploy and keeps the "one sync at a
time" guarantee (FR-014) as a single in-process mutex rather than a distributed
lock.

**Alternatives considered**: Kubernetes `CronJob` hitting the trigger endpoint —
rejected by the maintainer; it splits the app across two deploy objects and makes
single-flight a cross-process problem.

**Alternatives considered for the timer itself**: `robfig/cron`. Rejected: one
fixed daily time needs ~15 lines of stdlib `time`, not a dependency.

## 6. Single-flight / concurrency

**Decision**: One `sync.Mutex` + a `running bool` (or `atomic.Bool`) guarding the
engine's `Run` method. HTTP trigger while running → `409 Conflict`. Scheduled
tick while running → log and skip. All DB writes for a given recipe pair commit
in their own transaction (see §7).

**Rationale**: Meets FR-014 with no new concepts.

## 7. Crash-safety and reconciliation (FR-016, FR-018)

**Decision**: The sync is **stateless between runs except for the `recipe_link`
rows**. Every run rebuilds its picture from the live recipe lists of both
accounts plus the stored links, then processes each recipe pair independently and
commits that pair's link + baseline update in its own transaction. The run
summary row is upserted as progress is made and finalized at the end.

**Consequences**:

- A process killed mid-run leaves at most one recipe pair in an "Eetmeter write
  done, link not yet committed" state. Next run sees the just-created/updated
  recipe on the far side with no (or stale) link and re-links it: equal content →
  clean link; unequal content → flagged as a conflict (safe, no data lost, the
  maintainer resolves it).
- An aborted run (auth failure, network) has made no writes and committed no link
  changes for the accounts it never reached.
- No migration or long-held transaction spans the whole run, so a deploy during a
  sync is safe.

**Rationale**: Idempotent reconciliation is simpler to reason about than a
write-ahead log or a resumable checkpoint, and the spec explicitly allows "the
next run reconciles from the actual state of both accounts".

## 8. Change detection — the recipe fingerprint

**Decision**: For each recipe compute `SHA-256` over a canonical serialization of
its **portable content**:

- `numberOfPortions` (integer)
- `items`: the ingredient list, each reduced to
  `{ brandProductId, productUnitId, productName, unitName, amount }`, with
  `amount` formatted to a fixed precision (3 decimal places) to remove float
  noise, and the list sorted by
  `(productUnitId, brandProductId, amount, productName)`.

The per-ingredient `id` and the recipe `id` are **excluded** (server-assigned,
per-account). The recipe `name` is excluded from the hash because it is the match
key and is equal by construction for any linked pair; it is still compared
directly so a name-only edit is caught as "the pair no longer matches by name" →
handled as delete+create per the spec.

"Changed since last sync" for side X = `fingerprint(current) != baseline_hash_X`
stored on the link.

**Rationale**: No per-recipe modification timestamp is exposed by the API
(confirmed absent from the Vreetmeter models), so content hashing is the only
reliable signal. Canonicalization (sorted ingredients, fixed float precision)
prevents spurious "changed" results from key ordering or float formatting.

**Alternatives considered**: storing the full last-synced recipe JSON and
deep-comparing. Rejected: larger rows for no benefit; a hash is enough to detect
change, and for conflict display we re-fetch both live versions anyway.

## 9. Conflict review & resolution interface

**Decision**: HTTP endpoints. `GET /conflicts` lists unresolved conflicts with
both current versions inline; `POST /conflicts/{linkID}/resolve` with
`{"winner":"a"|"b"}` (or `?winner=a`) records a pending resolution. The **next**
run applies it: copy the winner's current content to the other side, set both
baselines to the winner's fingerprint, clear the conflict.

**Rationale**: Chosen by the maintainer. Matches the spec's "browser-reachable",
keeps resolution inside the app and unit-testable, and deferring application to
the next run reuses the normal write path instead of adding an out-of-band one.

## 10. Endpoint authentication

**Decision**: Optional static bearer token. If `SYNC_API_TOKEN` is set, all
routes except `GET /healthz` require `Authorization: Bearer <token>`; if unset,
the API is open (suitable when the Ingress is not publicly exposed).

**Rationale**: The mutating endpoints act on real accounts and will sit behind an
Ingress. A single-token check is ~15 lines and no dependency. Making it
conditional keeps local runs and tests friction-free. This is a small, present
need, not speculative generality.

**Alternatives considered**: OAuth/OIDC, per-user accounts. Rejected hard — one
operator, one token.

## 11. Deployment packaging

**Decision**: A multi-stage `Dockerfile` in this repo producing a distroless
static image. Kubernetes manifests are **out of scope for this feature** — the
maintainer keeps them in a separate repo and will generate them later.

**Rationale**: Maintainer's call. The plan still documents the runtime contract
the manifests must satisfy (env vars, `/healthz`, port, a Postgres reachable via
`DATABASE_URL`) in `quickstart.md`.

## 12. Testing approach

**Decision** (following constitution Principle III):

- **Reconciliation engine** — table-driven unit tests covering every branch:
  create A→B and B→A, update A→B and B→A, both-changed-to-equal, both-changed
  conflict, conflict persists across runs, pending-resolution applied,
  one-sided deletion retires the link, first sync with equal names, first sync
  with clashing names, duplicate name within one account. This is the core
  branchy logic and MUST be covered.
- **Fingerprint** — unit tests: ingredient reordering yields an equal hash,
  amount float noise is absorbed, a real content change flips the hash.
- **Eetmeter client** — tests against `httptest.Server` replaying the fixtures
  captured by the spike (auth header construction, list parsing, create/update
  request bodies, 401 handling).
- **Store** — a few integration tests that run only when `TEST_DATABASE_URL` is
  set (skipped otherwise): link upsert, status transitions, run-summary
  finalize. No container library dependency.
- **HTTP handlers** — one happy-path test per route plus the `409` concurrent
  trigger; thin glue otherwise.

No test-first mandate, no coverage target.

**Alternatives considered**: `testcontainers-go` for hermetic Postgres tests.
Rejected: a heavy test dependency; a disposable database URL is enough for a solo
project.
