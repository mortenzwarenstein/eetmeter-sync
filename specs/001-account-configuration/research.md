# Phase 0 Research: Account Configuration

**Date**: 2026-09-09 | **Feature**: `001-account-configuration`

All Technical Context unknowns are resolved below. Sources: the project
constitution (`.specify/memory/constitution.md` v1.0.0), the read-only Eetmeter
references named there (`SimonVreman/vreetmeter`, `../eetmeter-share`), and the
clarifications recorded in [spec.md](./spec.md) § Clarifications (Session
2026-09-09).

---

## Decision 1 — Eetmeter authentication contract

**Decision.** Authenticate with `POST {base}/account/credentials`, JSON body
`{"deviceId","emailAddress","password"}`. Base URL
`https://api3-mijn.voedingscentrum.nl/api/`, overridable via
`EETMETER_SYNC_EETMETER_BASE_URL` (required for fixture/integration runs).
Required headers on every request: `version: 4.6.0`, `platform: iOS`, plus
`User-Agent: eetmeter-sync/<version> (+https://github.com/…)`. On success the
response body is `{"token": "<opaque>"}`. The credential presented on all
subsequent requests is the **pair** `token + ":" + deviceId`, sent as
`Authorization: Basic <token>:<deviceId>` (not Base64 — the header value is the
literal pair, matching the reference client). On `401`/`403` the client discards
the stored token and re-authenticates once before surfacing a failure.

**Rationale.** This is exactly what the reference SwiftUI client
(`Vreetmeter/Api/EetmeterClient.swift`, `EetmeterAPI.login`) and the earlier Go
attempt (`eetmeter-share/internal/voedingscentrum/api.go`) do. The constitution
names these as the working contract references and forbids adding headers beyond
what the API needs to function.

**Alternatives considered.** OAuth-style bearer token (not what the API uses);
Base64-encoding the pair (the reference sets the header to the raw pair; fixtures
will confirm and win if reality differs).

**Fixtures to record (scrubbed of real tokens/e-mail/personal data):**
`auth_success.json`, `auth_invalid_credentials.json` (HTTP 401), plus the golden
request (headers + body shape) for `POST account/credentials`.

---

## Decision 2 — What "verify" calls (the read-back step)

**Decision.** Verification is two steps (spec Q5): (1) `POST account/credentials`
to obtain a token, then (2) one authenticated `GET {base}/userfavorite` using the
`token:deviceId` credential. Both must return success for status `connected`. The
read-back response body is not otherwise used in this feature.

**Rationale.** The references expose **no** `account` or `profile` GET endpoint.
`GET userfavorite` is the call the reference app issues immediately after login
(`EetmeterAPI.login` → `fetchFavorites`) to confirm the session is live; it is
lightweight and returns a small list. Per the constitution ("When a reference
disagrees with reality, the recorded fixtures win and the discrepancy is noted in
the spec or plan"), this plan records the discrepancy: the spec's phrase
"account/profile record" is realized as `GET userfavorite`. The intent in the
spec and Q5 — prove the issued token actually works and the account is reachable —
is met. An empty favourites list still counts as `connected` (spec edge case:
"valid but empty/restricted → connected").

**Alternatives considered.**
- `GET combinedproduct` (the recipe list a later sync needs): heavier, and spec Q5
  explicitly did **not** choose the "probe the recipe list" option. Rejected for
  this feature; the sync feature will exercise it.
- Auth call only (Q5 option A): rejected by the operator — a returned token does
  not prove the token is accepted.

**Fixtures to record:** `userfavorite_success.json`,
`userfavorite_unauthorized.json` (HTTP 401, drives the re-auth path),
`userfavorite_server_error.json` (HTTP 503, drives the transient path).

**Follow-up (applied).** The spec Assumptions / FR-007 wording "account/profile
record" has been tightened to "one lightweight authenticated read" with the exact
endpoint deferred to the recorded fixtures (this decision).

---

## Decision 3 — Failure classification

**Decision.** Map verification outcomes to the spec's categories (FR-011) as:

| Observation | Status | Cause category |
|---|---|---|
| Both steps 2xx | `connected` | — |
| `POST account/credentials` → 401/403, or body without a token | `credentials_rejected` | `credentials_rejected` |
| Read-back → 401/403 after a fresh token | `credentials_rejected` | `credentials_rejected` |
| Connection error, DNS failure, TLS failure, timeout | `temporarily_unavailable` | `service_unavailable` |
| Any 5xx, or 429 | `temporarily_unavailable` | `service_unavailable` |
| Other 4xx (400, 404, 422) on either call | `temporarily_unavailable` | `service_unavailable` |

**Rationale.** The operator needs exactly one bit — "fix the credentials" vs
"wait / retry later" (FR-011). Only 401/403 is unambiguously a credential problem.
Everything else is treated as transient so the operator is never told to change a
correct password because Eetmeter had a bad minute. "Other 4xx" is unexpected
against this API and is treated as transient (safe default) with the raw status
logged (no body, no secret) for diagnosis.

**Alternatives considered.** Treating unexpected 4xx as a hard failure — rejected;
it risks mislabeling an API-shape change as a credential error.

---

## Decision 4 — Secret handling (encryption in memory; never persisted)

**Decision.** In this feature eetmeter-sync **does not persist account secrets at
all** — configuration is re-read from the authoritative start-up source on every
boot (spec Q4) and runtime changes are session-only, so there is no at-rest copy.
While the process holds a secret it keeps it as AES-256-GCM ciphertext in
`internal/secret` and decrypts it only for the moment of an Eetmeter auth call.
Key material comes only from `EETMETER_SYNC_CRED_KEY`, a standard-Base64 encoding
of exactly 32 bytes (a KMS source may replace this later without changing
callers). Each ciphertext is `keyVersion(1 byte) || nonce(12) || GCM
ciphertext+tag`, Base64-encoded. The key is never written to the database, the
config file, logs, or the repository. If the key is absent or malformed at
start-up, the service logs a clear non-sensitive error and **refuses to start**;
it never rewrites or deletes stored data (spec edge case). The same format is
what a later feature that persists runtime-added accounts will write to its own
column, so that change is purely additive.

**Rationale.** AES-256-GCM is authenticated encryption in the Go standard library
(`crypto/aes`, `crypto/cipher`) — no dependency, no home-rolled crypto. Not
persisting the secret is a stronger position than "encrypted at rest" (Principle
V): there is nothing on disk to steal. Holding it encrypted in memory and
decrypting late is cheap defence against accidental logging of a struct and
casual heap inspection. The key-version byte keeps rotation a later config change
rather than a data migration.

**Alternatives considered.** Persisting the encrypted secret in Postgres now —
rejected: it would contradict spec Q4 (config authoritative, runtime session-only)
and add a boot-time reconcile step for no MVP benefit. NaCl `secretbox` via
`golang.org/x/crypto` — adds a dependency for no gain over stdlib AES-GCM.
Age/PGP — overkill for one field.

---

## Decision 5 — Start-up configuration format & precedence

**Decision.** Two start-up inputs plus runtime:

1. **Config file** (`EETMETER_SYNC_CONFIG_FILE`, optional): JSON, the structured
   account list. Shape:
   ```json
   {
     "operators": [
       { "id": "household",
         "accounts": [
           { "label": "kitchen", "login": "a@example.com",
             "secret": "…", "deviceId": "optional" }
         ] } ]
   }
   ```
   Intended to be mounted as a secret file; excluded by `.gitignore`.
2. **Environment**: singletons and tuning only — `EETMETER_SYNC_CRED_KEY`,
   `EETMETER_SYNC_API_TOKENS`, `EETMETER_SYNC_DATABASE_URL`,
   `EETMETER_SYNC_EETMETER_BASE_URL`, `EETMETER_SYNC_LISTEN_ADDR`,
   `EETMETER_SYNC_TLS_CERT_FILE` / `_KEY_FILE`,
   `EETMETER_SYNC_EETMETER_MIN_INTERVAL`, `EETMETER_SYNC_EETMETER_MAX_CONCURRENCY`,
   `EETMETER_SYNC_EETMETER_MAX_ATTEMPTS`. A single account may also be injected
   via `EETMETER_SYNC_ACCOUNT` (JSON object) for simple single-account
   deployments; this overrides a same-label account from the file.
3. **Runtime**: the HTTP API (`POST/PUT/DELETE /v1/accounts…`).

**Precedence.** Within a running session: runtime change > environment >
config file > built-in default, and the effective source is recorded per account
and reportable (FR-004). At start-up: environment > config file > default; any
prior runtime change is **discarded** — the account set is rebuilt from the
start-up inputs every boot (spec Q4). Only verification status/timestamps survive
a restart, from the state store.

**Rationale.** Expressing a list of accounts purely in environment variables is
awkward and error-prone; a mounted JSON file is the clean declarative path and
matches "config from env, no baked secrets" (the file is a mounted secret, not
baked into the image). Env stays for the handful of singletons. The precedence
mirrors the constitution's `explicit > env > file > default`, with "explicit"
meaning a live API call, and Q4 fixing the restart semantics.

**Alternatives considered.** Indexed env vars
(`EETMETER_SYNC_ACCOUNT_0_LABEL=…`) — verbose, fragile ordering. TOML/YAML —
would add a parser dependency; JSON is stdlib.

---

## Decision 6 — Authentication for the eetmeter-sync HTTP API

**Decision.** Static bearer tokens from `EETMETER_SYNC_API_TOKENS`: a
comma-separated list of `operatorId:token` pairs. A request must send
`Authorization: Bearer <token>`; the matched pair selects the operator whose
account set the request operates on. `/healthz` is unauthenticated; every other
endpoint requires a valid token. Failed auth attempts are rate-limited per client
IP (token bucket, e.g. 5/min) and logged without the presented token. If
`EETMETER_SYNC_API_TOKENS` is empty the service refuses to start unless
`EETMETER_SYNC_LISTEN_ADDR` is a loopback address **and**
`EETMETER_SYNC_DEV_ALLOW_NO_AUTH=1` is set (developer convenience, never in a
container image default).

**Rationale.** The spec defers operator sign-in to a later feature, but the
constitution is absolute that every endpoint except health is authenticated, with
no network-location bypass and no shipped default credential. A bearer token per
operator, supplied out of band, is the minimum that satisfies this and still lets
the HTTP API be the only way in. A real IdP/session design slots in later behind
the same middleware.

**Alternatives considered.** mTLS — heavier operationally for a two-account MVP;
can be added at the proxy. No auth / trust the network — forbidden by the
constitution.

---

## Decision 7 — Retry, rate-limiting, and concurrency (good-citizen controls)

**Decision.** `internal/eetmeter` owns all outbound restraint:

- **Per-account serialization**: at most one in-flight request per account; a
  configurable minimum interval (`EETMETER_SYNC_EETMETER_MIN_INTERVAL`, default
  5 s) between the start of consecutive requests for the same account.
- **Per-account verification-trigger cap**: separate from the min-interval, each
  account admits at most `EETMETER_SYNC_EETMETER_VERIFY_BURST` verification
  triggers (default 6) per rolling `EETMETER_SYNC_EETMETER_VERIFY_WINDOW`
  (default 10m). Every trigger draws from this budget — configure, secret change,
  and operator re-check alike (the in-attempt re-auth on a 401 read-back is part
  of one attempt, not a new trigger). A trigger over the cap is refused before
  any Eetmeter request with HTTP 429 and leaves the account's status and
  timestamps unchanged. Automatic triggers never approach the cap in normal use.
- **In-attempt retry only**: a single verification may retry a *transient*
  failure (connection error / timeout / 5xx / 429) up to
  `EETMETER_SYNC_EETMETER_MAX_ATTEMPTS` (default 3) with exponential backoff +
  full jitter, base 500 ms, ceiling 8 s. 401/403 is never retried this way — it
  triggers exactly one re-auth. There is **no** scheduled/background
  re-verification (spec Q1); once attempts are exhausted the account is recorded
  `temporarily_unavailable` and stays there until an operator acts.
- **Global concurrency cap**: a weighted semaphore bounds total simultaneous
  outbound Eetmeter requests across all accounts
  (`EETMETER_SYNC_EETMETER_MAX_CONCURRENCY`, default 4).
- **Headers**: only `version`, `platform`, `User-Agent`, and (when a token is
  held) `Authorization`. Nothing else.

**Rationale.** Reconciles Principle VI ("retry only with bounded exponential
backoff + jitter and a capped attempt count") with spec Q1 ("no automatic retry
loop"): the bounded retry lives *inside one verification attempt* to ride out a
blip; it is not a standing loop. SC-007's "≤ 6 attempts in any 10-minute window"
is enforced directly by the per-account verification-trigger cap above (default 6
per rolling 10 minutes): the 5 s minimum interval only prevents back-to-back
requests; the cap bounds the 10-minute total.

**Alternatives considered.** A background retry queue for `temporarily_unavailable`
accounts — explicitly rejected by spec Q1. No in-attempt retry at all — brittle
against normal transient network noise and arguably violates Principle VI's
"retry … with backoff" expectation.

---

## Decision 8 — Device identifier

**Decision.** When the operator supplies a `deviceId`, use it verbatim. When they
do not, derive it deterministically: `deviceId = UUIDv5(ns, operatorId + "\x00" +
login)` with a fixed project namespace UUID. It is therefore stable across
restarts by construction and never needs to be persisted or reconciled against
start-up config.

**Rationale.** FR-001 requires a stable generated device id reused on every
authentication. A deterministic derivation satisfies that without adding
device-id rows to the state store (which would otherwise be config-derived state
that Q4 says is rebuilt from config anyway). Aligns with the constitution's
preference for pure, deterministic functions over persisted guesses.

**Alternatives considered.** Random UUIDv4 persisted in the state store — works,
but adds persistence and a restart-reconciliation question for no benefit at this
scale. Requiring the operator to always supply one — worse ergonomics; FR-001
says the system must generate it.

---

## Decision 9 — Persistence substrate

**Decision.** PostgreSQL, one database, reached through `github.com/jackc/pgx/v5`
(`pgxpool`), configured by `EETMETER_SYNC_DATABASE_URL`. A repo-root
`docker-compose.yml` runs Postgres 16 (named volume, healthcheck) for local
development and integration tests; production points the URL at a managed
instance. Schema is owned by ordered SQL files under `db/migrations/`, embedded
into the binary with `//go:embed` and applied at start-up by a ~50-line runner in
`internal/store/migrate.go`:

1. `SELECT pg_advisory_lock($appId)` so concurrent instances don't race.
2. `CREATE TABLE IF NOT EXISTS schema_migrations (version bigint PRIMARY KEY,
   applied_at timestamptz NOT NULL DEFAULT now())`.
3. For each embedded file whose numeric prefix `> COALESCE(max(version), 0)`: run
   its SQL and insert the version, in one transaction per file.
4. `pg_advisory_unlock`.

This feature's schema: `schema_migrations` + `account_status` (see
[data-model.md](./data-model.md) §3). **No secrets and no account configuration
are stored** — configuration is authoritative from the start-up source on every
boot (spec Q4); the database holds only verification status that must survive a
restart (FR-016).

**Rationale.** The maintainer chose Postgres for the MVP. It is the constitution's
"defined store outside the repository … stateless process — all state lives in the
external store" taken literally, it is the substrate the coming sync-snapshot and
run-history features need anyway, and `pgx` is pure Go (no CGo), the de-facto
standard, and already the sibling project's driver. Embedding plain SQL
migrations with a tiny runner avoids taking on `golang-migrate` for what is a
handful of statements. An advisory lock keeps auto-migrate safe when more than one
replica starts at once.

**Alternatives considered.**
- **File-backed JSON on a volume** (previous plan revision): zero dependencies and
  fine for this feature's two status rows, but a dead end for the relational
  snapshot/run-history queries that are one feature away, and an awkward fit for a
  "stateless process, state in an external store" deployment. Superseded by the
  maintainer's decision.
- **SQLite** (`modernc.org/sqlite`): real SQL with no server, but still a sizeable
  dependency and it would have to be migrated to Postgres for multi-replica
  cloud deployment later. Skipped in favour of going straight to Postgres.
- **`golang-migrate` / `goose`** for migrations: well-tested, but adds a
  dependency and a CLI to the toolchain for ~10 lines of `CREATE TABLE`. The
  embedded runner is enough; revisit if migrations grow complex (down-migrations,
  data backfills).
- **`database/sql` + `lib/pq`**: works, but `lib/pq` is in maintenance mode and
  `pgx`'s native API + pool are a better fit.

---

## Resolved unknowns checklist

| Technical Context item | Status |
|---|---|
| Eetmeter auth endpoint, body, headers, token form | Resolved (D1) |
| Verification read-back endpoint | Resolved (D2) |
| Failure → category mapping | Resolved (D3) |
| Secret handling (in-memory encryption; not persisted) | Resolved (D4) |
| Start-up config format + precedence + restart semantics | Resolved (D5, spec Q4) |
| API authentication model | Resolved (D6) |
| Retry / rate-limit / concurrency parameters | Resolved (D7, spec Q1) |
| Device identifier generation | Resolved (D8) |
| Persistence substrate (Postgres + docker-compose + embedded migrations) | Resolved (D9) |
| Language / tooling / test framework | Resolved (constitution: Go 1.26, stdlib + pgx, `go test -race`) |

No `NEEDS CLARIFICATION` remain.
