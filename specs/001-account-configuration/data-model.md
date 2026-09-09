# Phase 1 Data Model: Account Configuration

**Date**: 2026-09-09 | **Feature**: `001-account-configuration`

Derived from [spec.md](./spec.md) § Key Entities / Functional Requirements and
[research.md](./research.md). Types are described conceptually; Go structs live in
`internal/account`, `internal/store`, and `internal/config`.

---

## 1. Entities

### Operator

The party that owns a set of Eetmeter accounts. Exactly one exists initially;
every account and every stored status row is scoped to an operator id so a second
operator is a configuration change, not a restruct_ure.

| Field | Type | Notes |
|---|---|---|
| `id` | string (slug) | Stable identifier, e.g. `household`. Supplied by config file and by the `operatorId:token` pairs in `EETMETER_SYNC_API_TOKENS`. |

Relationships: one Operator has many Accounts (0..N).

### Account (Eetmeter Account)

A configured connection to a single Mijn Eetmeter account.

| Field | Type | Source | Rules |
|---|---|---|---|
| `operatorId` | string | config / API token | Must match an existing Operator. |
| `label` | string | operator-supplied | Non-empty after trim; unique within the operator's set (FR-017). Used as the URL path key. Charset: letters, digits, `-`, `_`, `.`; 1–64 chars. |
| `login` | string | operator-supplied | Non-empty; the Eetmeter account login identifier (an e-mail address in practice). Unique within the operator's set (FR-017 / spec Q3). Compared case-insensitively, trimmed. |
| `secret` | string (write-only) | operator-supplied | Non-empty. Never returned, logged, or put in metrics/fixtures (FR-006). Stored only as ciphertext (see §3). |
| `deviceId` | string (UUID) | operator-supplied **or** derived | If absent, `UUIDv5(projectNS, operatorId + "\x00" + lower(login))` (research D8). Stable across restarts. |
| `configSource` | enum `runtime` \| `env` \| `file` \| `default` | computed | The source that supplied the value currently in effect (FR-004). |
| `secretUpdatedAt` | timestamp | computed | When the secret was last set/changed. Exposed; the secret itself is not (FR-006). |
| `status` | enum (see §2) | computed | Current connection status (FR-010). |
| `failureCategory` | enum `credentials_rejected` \| `service_unavailable` \| none | computed | Set when `status` is a failure; cleared on success (FR-011). |
| `lastAuthenticatedAt` | timestamp \| null | computed | Last time step 1 + step 2 both succeeded (FR-010). |
| `lastCheckedAt` | timestamp \| null | computed | Last time a verification attempt completed, success or failure (FR-010). |

**Identity.** `(operatorId, label)` is the primary key. `(operatorId, lower(login))`
carries a uniqueness constraint. There is no separate surrogate id in this
feature.

**Lifecycle of the configuration row.** Created by `LoadFromConfig` at start-up or
by `POST /v1/accounts`; mutated by `PUT /v1/accounts/{label}`; destroyed by
`DELETE /v1/accounts/{label}` (removal also destroys the stored ciphertext and the
status row — FR & spec US4). Runtime create/update/delete do not survive a
restart (spec Q4).

### VerificationResult

The outcome of one attempt to verify an account.

| Field | Type | Notes |
|---|---|---|
| `status` | enum (see §2) | Terminal status reached by this attempt. |
| `failureCategory` | enum \| none | Present iff `status` is a failure. |
| `checkedAt` | timestamp | When the attempt completed. |
| `authenticatedAt` | timestamp \| null | Set iff both steps succeeded. |
| `detail` | string | Short, non-sensitive, for logs/response (e.g. `"eetmeter returned 503"`). Never contains the secret, token, or personal data. |

The most recent result per account is always available (it is projected onto the
Account fields above and persisted in the state store). Earlier results MAY be
kept as history — out of scope for this feature; the store schema leaves room
(§3, `history` reserved) but the feature writes only the latest.

---

## 2. Status state machine

States (FR-010): `never_verified`, `connected`, `credentials_rejected`,
`temporarily_unavailable`.

```text
                 ┌─────────────────┐
   configure ───▶│ never_verified  │
                 └───────┬─────────┘
                         │ verify (event-driven only)
          ┌──────────────┼───────────────────────┐
          ▼              ▼                        ▼
   ┌────────────┐  ┌───────────────────┐  ┌───────────────────────┐
   │ connected  │  │credentials_rejected│  │temporarily_unavailable│
   └─────┬──────┘  └─────────┬─────────┘  └───────────┬───────────┘
         │                   │                        │
         │ session refused   │ secret updated ──▶ verify│ operator re-check ─▶ verify
         │ by Eetmeter (401) │ operator re-check ─▶ verify│ secret updated ─▶ verify
         ▼                   │                        │
   re-auth from stored       └────────────┬───────────┘
   credentials (FR-013):                  │
     success ─▶ connected                 ▼
     401/403 ─▶ credentials_rejected   (no timed transition; a failure
                                        state is left only by an event)
```

**Transition triggers** (spec Q1 — event-driven only, no scheduler, no auto-retry):

| Trigger | From | To |
|---|---|---|
| Account configured (`POST`/`LoadFromConfig`) | — | `never_verified` then immediately `verify` (FR-008) |
| Secret changed (`PUT` with new secret) | any | `verify` (FR-008) |
| Operator re-check (`POST …/verify` or `POST /v1/accounts/verify`) | any | `verify` (FR-009) |
| Eetmeter refuses a live session (401/403 during use) | `connected` | re-auth once (FR-013); then `connected` or `credentials_rejected` |
| `verify` result = both steps ok | any | `connected`, clear `failureCategory`, set `lastAuthenticatedAt` |
| `verify` result = 401/403 | any | `credentials_rejected` |
| `verify` result = transient (timeout/5xx/429/other 4xx) after bounded in-attempt retries | any | `temporarily_unavailable` |
| Account removed | any | row deleted; no status written back even if a verify was mid-flight (spec edge case) |

`lastCheckedAt` is set on every completed `verify` regardless of outcome.

A verification trigger that exceeds the per-account burst cap
(`EETMETER_SYNC_EETMETER_VERIFY_BURST` / `_WINDOW`, SC-007) is refused with 429
before any Eetmeter contact; no status or timestamp changes.

---

## 3. Persisted state — PostgreSQL

Substrate: PostgreSQL, connection from `EETMETER_SYNC_DATABASE_URL` via
`pgxpool` (research D9). Schema is owned by `db/migrations/NNNN_*.sql`, embedded
in the binary and applied at start-up under a `pg_advisory_lock`. The database
**holds no secrets and no account configuration** — only per-account verification
status that must survive a restart (FR-016). Configuration is rebuilt from the
authoritative start-up source on every boot (spec Q4).

### `db/migrations/0001_account_status.sql`

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     bigint      PRIMARY KEY,
    applied_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE account_status (
    operator_id           text        NOT NULL,
    label                 text        NOT NULL,
    status                text        NOT NULL
        CHECK (status IN ('never_verified','connected',
                          'credentials_rejected','temporarily_unavailable')),
    failure_category      text
        CHECK (failure_category IN ('credentials_rejected','service_unavailable')),
    last_authenticated_at timestamptz,
    last_checked_at       timestamptz,
    detail                text        NOT NULL DEFAULT '',
    updated_at            timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (operator_id, label)
);
```

Access pattern (in `internal/store`, all statements parameterised):

| Operation | SQL shape |
|---|---|
| Load all status for an operator at boot / on `GET /v1/accounts` | `SELECT … FROM account_status WHERE operator_id = $1` |
| Upsert after a verification | `INSERT … ON CONFLICT (operator_id, label) DO UPDATE SET …` |
| Drop on account removal | `DELETE FROM account_status WHERE operator_id = $1 AND label = $2` |

Rules:

- A row whose `(operator_id, label)` has no matching configured account at boot is
  left in place but inert; it re-attaches if that label is configured again. A
  later feature may add a prune step.
- `failure_category` is `NULL` exactly when `status` is `connected` or
  `never_verified`.
- Row writes are single-statement upserts/deletes — no long transactions, safe to
  interrupt. `updated_at` is bumped on every write.
- Migrations: the runner creates `schema_migrations`, then for each embedded file
  with numeric prefix greater than `MAX(version)` runs the file and records the
  version, one transaction per file. Unknown/way-ahead versions in
  `schema_migrations` (a newer binary ran before) → log and refuse to start rather
  than downgrade.

### In-memory secret representation (not persisted)

Because configuration is authoritative from the start-up source (spec Q4) and the
database holds no secrets, ciphertext lives only in the in-memory account set for
the life of the process. Format produced by `internal/secret` (research D4):

```text
base64std( keyVersion(1 byte) || nonce(12 bytes) || AES-256-GCM ciphertext+tag )
```

A future feature that persists runtime-added accounts will add a column holding
exactly this string, so that change is additive.

---

## 4. Start-up configuration schema

Config file (`EETMETER_SYNC_CONFIG_FILE`, JSON — research D5):

```json
{
  "operators": [
    {
      "id": "household",
      "accounts": [
        {
          "label": "kitchen",
          "login": "person-a@example.com",
          "secret": "…",
          "deviceId": "optional-uuid"
        },
        { "label": "phone", "login": "person-b@example.com", "secret": "…" }
      ]
    }
  ]
}
```

Single-account env shortcut: `EETMETER_SYNC_ACCOUNT` = one JSON object
`{ "operatorId","label","login","secret","deviceId"? }`; overrides a same-label
file entry (env > file).

Validation at load (FR-015): each account must have non-empty `label`, `login`,
`secret`; `label` charset/length as in §1; `label` unique per operator; `login`
unique per operator; `operatorId` known. A failing account is rejected with a
clear, secret-free error naming the operator and label; other accounts still
load.

### Tuning / singleton environment variables

| Variable | Default | Purpose |
|---|---|---|
| `EETMETER_SYNC_CRED_KEY` | — (required) | Base64 of 32 bytes; AES-256-GCM key. |
| `EETMETER_SYNC_API_TOKENS` | — (required unless loopback dev) | `operatorId:token[,operatorId:token…]`. |
| `EETMETER_SYNC_DATABASE_URL` | — (required) | Postgres DSN, e.g. `postgres://eetmeter:eetmeter@localhost:5432/eetmeter?sslmode=disable` for the compose DB. |
| `EETMETER_SYNC_CONFIG_FILE` | — | Path to the JSON account list. |
| `EETMETER_SYNC_ACCOUNT` | — | Single-account JSON shortcut. |
| `EETMETER_SYNC_LISTEN_ADDR` | `127.0.0.1:8080` | Bind address. Non-loopback requires TLS. |
| `EETMETER_SYNC_TLS_CERT_FILE` / `_KEY_FILE` | — | PEM paths for a non-loopback bind. |
| `EETMETER_SYNC_EETMETER_BASE_URL` | `https://api3-mijn.voedingscentrum.nl/api/` | Overridable for fixtures/integration. |
| `EETMETER_SYNC_EETMETER_MIN_INTERVAL` | `5s` | Min gap between requests for one account. |
| `EETMETER_SYNC_EETMETER_MAX_CONCURRENCY` | `4` | Global outbound cap. |
| `EETMETER_SYNC_EETMETER_MAX_ATTEMPTS` | `3` | In-attempt transient retries per verification. |
| `EETMETER_SYNC_EETMETER_VERIFY_BURST` | `6` | Max verification triggers per account per rolling window (SC-007). Over the cap → 429, no Eetmeter call, no state change. |
| `EETMETER_SYNC_EETMETER_VERIFY_WINDOW` | `10m` | Rolling window for `EETMETER_SYNC_EETMETER_VERIFY_BURST`. |
| `EETMETER_SYNC_DEV_ALLOW_NO_AUTH` | unset | `=1` + loopback bind only: run without API tokens (dev). |

---

## 5. Requirement → data-model traceability

| Requirement | Where satisfied |
|---|---|
| FR-001 login/secret/deviceId; generated stable deviceId | Account fields; deviceId derivation §1 |
| FR-004 precedence + effective source | `configSource` field; §4 precedence |
| FR-005 / FR-006 secret protected, never exposed | secret never persisted (§3 in-memory representation, research D4); `secret` write-only; `detail` scrubbed |
| FR-007 two-step verify + recorded result | VerificationResult; state machine §2 |
| FR-008 verify on configure & on secret change | Transition table §2 |
| FR-009 on-demand re-check (one / all) | Transition table §2 |
| FR-010 status + timestamps exposed | Account `status`, `lastAuthenticatedAt`, `lastCheckedAt` |
| FR-011 cause classification | `failureCategory`; research D3 |
| FR-012 no auto-retry; paced attempts | State machine (no timed edges); per-account verification-trigger cap §4; research D7 |
| FR-013 re-auth before reporting failure | `connected` → re-auth edge §2 |
| FR-014 per-operator isolation | `operator_id` in every query and the `account_status` PK §3 |
| FR-015 validate on load/change, isolate failures | §4 validation rules |
| FR-016 status survives restart; config re-derived | `account_status` table §3; §4 (config authoritative) |
| FR-017 label unique; login unique | §1 identity constraints |
| FR-018 good-citizen outbound | research D7 (client-owned) |
