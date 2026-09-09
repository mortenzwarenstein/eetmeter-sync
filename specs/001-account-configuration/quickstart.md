# Quickstart & Validation: Account Configuration

**Date**: 2026-09-09 | **Feature**: `001-account-configuration`

A run-and-verify guide that proves the feature end to end. Contract details are in
[contracts/openapi.yaml](./contracts/openapi.yaml); shapes and rules are in
[data-model.md](./data-model.md). No implementation code here.

---

## Prerequisites

- Go 1.26 (`go version`).
- Docker + `docker compose` (provides Postgres for dev and tests).
- A reachable Eetmeter API, **or** the recorded fixtures used by the contract
  tests. For a live run you need real Mijn Eetmeter credentials for a throwaway or
  personal account — never commit them.
- `curl` and `jq` for the manual checks.

## One-time setup

```sh
# Postgres for local dev + integration tests
docker compose up -d db
# compose publishes: postgres://eetmeter:eetmeter@localhost:5432/eetmeter
export EETMETER_SYNC_DATABASE_URL="postgres://eetmeter:eetmeter@localhost:5432/eetmeter?sslmode=disable"

# 32-byte AES key, base64
export EETMETER_SYNC_CRED_KEY="$(head -c 32 /dev/urandom | base64)"

# one operator "household" with a bearer token
export EETMETER_SYNC_API_TOKENS="household:$(head -c 24 /dev/urandom | base64 | tr -d '/+=')"

export EETMETER_SYNC_LISTEN_ADDR="127.0.0.1:8080"
# optional: point at a mock during local testing
# export EETMETER_SYNC_EETMETER_BASE_URL="http://127.0.0.1:9099/api/"

TOKEN="${EETMETER_SYNC_API_TOKENS#household:}"
```

Optional declarative accounts file:

```sh
cat > /tmp/accounts.json <<'JSON'
{ "operators": [ { "id": "household", "accounts": [
  { "label": "kitchen", "login": "person-a@example.com", "secret": "REPLACE" }
] } ] }
JSON
export EETMETER_SYNC_CONFIG_FILE=/tmp/accounts.json
```

## Build & test

```sh
gofmt -l .            # expect no output
go vet ./...          # expect clean

# unit + Eetmeter fixture tests only (no database needed):
go test ./... -race

# include the Postgres-backed store tests:
EETMETER_SYNC_TEST_DATABASE_URL="$EETMETER_SYNC_DATABASE_URL" go test ./... -race

go build ./cmd/eetmeter-sync
```

The store's `postgres_test.go` `t.Skip`s when `EETMETER_SYNC_TEST_DATABASE_URL`
is unset, so a plain `go test ./...` still passes. Each test runs migrations into
a scratch schema and tears it down.

## Run

```sh
./eetmeter-sync &     # or: go run ./cmd/eetmeter-sync
                      # embedded migrations apply automatically at start-up
curl -s localhost:8080/healthz | jq .          # {"status":"ok"}
```

---

## Validation scenarios

Each maps to a user story / success criterion in [spec.md](./spec.md).

### V1 — Configure and verify the first account (US1, SC-002, SC-003)

```sh
curl -s -X POST localhost:8080/v1/accounts \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"label":"kitchen","login":"person-a@example.com","secret":"<valid>"}' | jq .
```

Expect: `201`, body `status:"connected"`, `lastAuthenticatedAt` set, `secretSet:true`,
**no** `secret` field anywhere. Repeat with a wrong `secret` under label `phone`:
expect `201` with `status:"credentials_rejected"`,
`failureCategory:"credentials_rejected"`. Grep the process log and the response —
the secret string must appear zero times (SC-003).

### V2 — Deploy with accounts already supplied (US2, SC-001)

Stop the process. With `EETMETER_SYNC_CONFIG_FILE` set to a file listing two
accounts, start it again. Without any API call:

```sh
curl -s localhost:8080/v1/accounts -H "Authorization: Bearer $TOKEN" | jq '.accounts[] | {label,status,configSource}'
```

Expect both accounts present within ~30 s of start-up, each with a definitive
`status` and `configSource:"file"`.

### V3 — Missing config field is reported, not fatal (US2 scenario 2, FR-015)

Add a third file entry with an empty `secret`. Restart. Expect: a clear startup
log line naming operator `household` and label of the bad entry, **no** secret in
the message, the process still serving, and the two good accounts still listed.

### V4 — Fix an account after a password change (US3, SC-005)

Put `phone` into `credentials_rejected` (V1). Then:

```sh
curl -s -X PUT localhost:8080/v1/accounts/phone \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"secret":"<now-correct>"}' | jq '{status,lastAuthenticatedAt}'
```

Expect: `200`, `status:"connected"`, fresh `lastAuthenticatedAt`, no restart.

### V5 — Inspect & manage (US4, SC-008)

`GET /v1/accounts` shows label, status, `lastAuthenticatedAt`, `lastCheckedAt`,
`failureCategory` for every account and no secret. Then:

```sh
curl -s -X DELETE localhost:8080/v1/accounts/phone -H "Authorization: Bearer $TOKEN" -o /dev/null -w '%{http_code}\n'  # 204
curl -s localhost:8080/v1/accounts -H "Authorization: Bearer $TOKEN" | jq '.accounts[].label'  # "phone" gone
```

### V6 — On-demand re-check (US4 scenario 3, FR-009)

```sh
curl -s -X POST localhost:8080/v1/accounts/kitchen/verify -H "Authorization: Bearer $TOKEN" | jq '{status,lastCheckedAt}'
curl -s -X POST localhost:8080/v1/accounts/verify        -H "Authorization: Bearer $TOKEN" | jq '.accounts[].lastCheckedAt'
```

Expect `lastCheckedAt` to advance. Fire the single-account call ~10 times in a
row inside one `EETMETER_SYNC_EETMETER_VERIFY_WINDOW`: the first 6 run, the 7th
and beyond return `429` with no Eetmeter call, and `lastCheckedAt` stops
advancing once the budget is spent (SC-007, FR-018). A re-check of a *different*
account in the same window still runs.

### V7 — Status survives a restart; runtime changes do not (FR-016, spec Q4)

With `kitchen` from the config file `connected` and a runtime-only account `adhoc`
added via `POST`, restart the process (leave Postgres running). Expect: `kitchen`
present with its `status` restored from the `account_status` table (not
`never_verified` until re-checked); `adhoc` **absent** (runtime add did not
survive). Confirm directly:
`docker compose exec db psql -U eetmeter -c "select operator_id,label,status from account_status;"`.

### V8 — Encryption key problems are safe (spec edge case, FR-005)

Restart with `EETMETER_SYNC_CRED_KEY` unset, then with a different 32-byte key.
Expect: the process refuses to start with a clear non-sensitive error and writes
nothing to the database (no `account_status` rows added or changed, `schema_migrations`
untouched).

### V9 — Auth is enforced (constitution Security & Deployment)

```sh
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/v1/accounts            # 401
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/healthz                # 200
```

Send ~10 bad tokens quickly: later attempts get `429` and the log never prints
the presented token.

---

## Contract test expectations (Eetmeter, fixture-based — Principle II)

Recorded under `internal/eetmeter/testdata/`, all scrubbed:

| Fixture | Drives |
|---|---|
| `auth_success.json` | token parsed; credential becomes `token:deviceId` |
| `auth_invalid_credentials.json` (401) | → `credentials_rejected` |
| `userfavorite_success.json` | read-back ok → `connected` |
| `userfavorite_unauthorized.json` (401) | one re-auth, then classify |
| `userfavorite_server_error.json` (503) | bounded in-attempt retry → `temporarily_unavailable` |

Decoding must tolerate unknown JSON fields and fail loudly on a missing/
type-mismatched field the client depends on (`token`).

## Store test expectations (Postgres, `docker-compose.yml`)

`internal/store/postgres_test.go`, gated by `EETMETER_SYNC_TEST_DATABASE_URL`:

| Check | Expectation |
|---|---|
| Migration runner on an empty DB | `schema_migrations` + `account_status` created; runner is idempotent on a second call |
| Upsert then reload for an operator | round-trips status, category, both timestamps, `detail` |
| Concurrent runner invocations | `pg_advisory_lock` serialises them; no duplicate-version error |
| `failure_category` CHECK / `status` CHECK | rejects out-of-enum values |
| Delete on removal | row gone; other operators' rows untouched |

## Teardown

```sh
docker compose down -v      # stop Postgres and drop its volume
```

