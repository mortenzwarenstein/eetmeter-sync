# Contract: eetmeter-sync HTTP API

The service this feature builds. Small, JSON, unversioned. Intended to sit behind
a cluster Ingress and be poked from a browser or `curl`.

**Auth**: if `SYNC_API_TOKEN` is set, every route except `GET /healthz` requires
`Authorization: Bearer <token>` and returns `401` without it. If unset, all routes
are open.

**Content type**: responses are `application/json` unless noted. Request bodies,
where taken, are `application/json`.

**Errors**: non-2xx responses carry `{ "error": "<human readable>" }`. Error text
never contains a password.

---

## `GET /healthz`

Liveness/readiness. Pings the database.

| Response | Body |
|----------|------|
| `200 OK` | `{ "status": "ok" }` |
| `503 Service Unavailable` | `{ "status": "degraded", "error": "database unreachable" }` |

Never requires auth.

---

## `POST /sync`  (also accepts `GET /sync` for browser convenience)

Trigger a sync now.

| Response | When | Body |
|----------|------|------|
| `202 Accepted` | A run was started. | `{ "runId": "<uuid>", "startedAt": "<rfc3339>", "trigger": "manual" }` |
| `409 Conflict` | A run (scheduled or manual) is already in progress. | `{ "error": "sync already running", "runId": "<uuid>", "startedAt": "<rfc3339>" }` |
| `500` | Could not start (e.g. DB down). | `{ "error": "..." }` |

The call returns as soon as the run is started; it does not block until the run
finishes. Poll `GET /sync/last` for the outcome.

---

## `GET /sync/last`

The most recent run (running or finished).

| Response | Body |
|----------|------|
| `200 OK` | the run-summary JSON (see `data-model.md` → "Run summary JSON"), with `result` one of `running`, `succeeded`, `failed` |
| `404 Not Found` | `{ "error": "no run yet" }` |

---

## `GET /conflicts`

All unresolved conflicts (`recipe_link.status = 'conflict'`), each with both
current live versions so the maintainer can compare.

```json
{
  "conflicts": [
    {
      "linkId": 42,
      "name": "Curry",
      "detectedAt": "2026-09-09T06:00:03Z",
      "a": {
        "recipeId": "1c2f...", "numberOfPortions": 4,
        "items": [ { "productName": "Kokosmelk", "unitName": "ml", "amount": 400, "productUnitId": "…", "brandProductId": null } ]
      },
      "b": {
        "recipeId": "9a7d...", "numberOfPortions": 4,
        "items": [ { "productName": "Kokosmelk", "unitName": "ml", "amount": 200, "productUnitId": "…", "brandProductId": null } ]
      },
      "pendingResolution": null
    }
  ]
}
```

`pendingResolution` is `"a"`, `"b"`, or `null`.

| Response | Body |
|----------|------|
| `200 OK` | as above; `conflicts` is `[]` when there are none |

---

## `POST /conflicts/{linkID}/resolve`

Record which side wins. Applied on the **next** run, not immediately.

Body: `{ "winner": "a" }` or `{ "winner": "b" }`. Also accepts `?winner=a` as a
query parameter (so it is reachable from a browser address bar).

| Response | When | Body |
|----------|------|------|
| `200 OK` | Resolution recorded (or replaced an earlier pending one for this link). | `{ "linkId": 42, "winner": "a", "appliesOnNextRun": true }` |
| `400 Bad Request` | `winner` missing or not `a`/`b`. | `{ "error": "winner must be 'a' or 'b'" }` |
| `404 Not Found` | No link with that id, or it is not currently in `conflict`. | `{ "error": "no conflict for link 42" }` |

---

## `GET /`  *(optional, nice-to-have)*

`text/plain` (or minimal HTML) index listing the routes above, so opening the
service in a browser is not a dead end. No behavior depends on it; may be omitted
in the first cut.

---

## Runtime configuration (environment)

| Variable | Required | Default | Meaning |
|----------|----------|---------|---------|
| `DATABASE_URL` | yes | — | PostgreSQL connection string. |
| `EETMETER_ACCOUNT_A_TOKEN` + `EETMETER_ACCOUNT_A_DEVICE_ID` | yes | — | Account A auth: a device-bound pair captured from a logged-in app session. |
| `EETMETER_ACCOUNT_B_TOKEN` + `EETMETER_ACCOUNT_B_DEVICE_ID` | yes | — | Account B auth. |
| `EETMETER_ACCOUNT_{A,B}_EMAIL` | no | — | Label only; unused in token mode. Required only for the (unsupported) password login. |
| `EETMETER_ACCOUNT_{A,B}_PASSWORD` | no | — | Unused in token mode. |
| `EETMETER_ACCOUNT_{A,B}_LABEL` | no | `a` / `b` | Label in summaries. |
| `SYNC_DAILY_TIME` | no | `06:00` | Wall-clock `HH:MM` for the daily run. |
| `SYNC_TIMEZONE` | no | `Europe/Amsterdam` | IANA tz for `SYNC_DAILY_TIME`. |
| `SYNC_ON_STARTUP` | no | `false` | Run once immediately on boot. |
| `SYNC_DRY_RUN` | no | `false` | When truthy, every run authenticates and computes the plan but writes nothing (no Mijn Eetmeter calls that mutate, no `recipe_link` changes). The run summary carries `"dryRun": true`. |
| `HTTP_ADDR` | no | `:8080` | Listen address. |
| `SYNC_API_TOKEN` | no | — | If set, bearer token required on all routes except `/healthz`. |
| `EETMETER_API_BASE_URL` | no | `https://api3-mijn.voedingscentrum.nl/api/` | Overridable for tests/spike. |
| `EETMETER_APP_VERSION` | no | `4.6.0` | Sent as the `version` header. |
| `EETMETER_PLATFORM` | no | `iOS` | Sent as the `platform` header. |
| `LOG_LEVEL` | no | `info` | `debug` \| `info` \| `warn` \| `error`. |

Passwords appear only in process memory and outbound login requests — never in
logs, responses, or the database.
