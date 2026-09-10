# Contract: Mijn Eetmeter API (external, partially reverse-engineered)

**Status**: Auth, READ, and WRITE (create / update / delete) paths are all
**CONFIRMED** against the live API (spike, 2026-09-10 — see "Spike log" at the
bottom). One residual unknown: whether `isCustom` (own-product) ingredients
survive a cross-account copy.

Base URL (overridable via `EETMETER_API_BASE_URL`):
`https://api3-mijn.voedingscentrum.nl/api/`

Every request MUST send these headers or the server replies `426 Upgrade
Required` (confirmed — `version` must be exactly `4.6.0`; `5.0.0`, `4.7.0`, empty,
or a lowercased `platform` all 426):

| Header | Value |
|--------|-------|
| `version` | `4.6.0` (config `EETMETER_APP_VERSION`) — pinned |
| `platform` | `iOS` (config `EETMETER_PLATFORM`) |
| `Authorization` | `Basic <token>:<deviceId>` — literal string, **not** base64 |
| `Content-Type` | `application/json` on requests with a body |

---

## Authentication — CONFIRMED

The API uses a **device-bound token**: an app registers a `deviceId` once and
gets a `token` that then never changes. Every call — *including the login call
itself* — must carry `Authorization: Basic <token>:<deviceId>`.

A bare credential login with **no** `Authorization` header returns `404` (empty
body) regardless of whether the credentials are valid — the device-registration
step that mints the first token is not reproducible from a server. **This service
therefore requires a `token` + `deviceId` captured from a logged-in app**
(`EETMETER_ACCOUNT_x_TOKEN` / `EETMETER_ACCOUNT_x_DEVICE_ID`). With those set the
client skips login entirely.

### `POST account/credentials` (only used when a token is present, to validate)

Headers include `Authorization: Basic <token>:<deviceId>`. Body:

```json
{ "deviceId": "<the same deviceId>",
  "emailAddress": "<email>",
  "password": "<password>" }
```

Response `200` — the full Account object (PascalCase). Only `Token` is read:

```json
{ "Token": "d18d31b5-…", "EmailAddress": "you@example.com",
  "DateOfBirth": "1996-03-31T00:00:00.000", "Gender": 0, "Length": 179,
  "Weight": 90.0, "TotpState": 0, "WebAccountId": "00000000-0000-0000-0000-000000000000",
  "…": "…" }
```

- Well-formed body, correct headers, wrong/missing token → `404` empty body.
- Malformed body (e.g. `{}`) → `500` `"An error has occurred."`
- `401` / `403` with `WWW-Authenticate: Basic realm="MijnVC"` on any authenticated
  call ⇒ auth failure: abort the run (FR-017), record `failed`, make no writes.
  (In practice a bad token gives `401` on `GET combinedproduct`.)

---

## List recipes — CONFIRMED

### `GET combinedproduct`

`Authorization` header only — no prior login call needed. Returns **every** recipe
in one response; no pagination observed (44 recipes ≈ 122 KB in the test account).

```json
{
  "Items": [
    {
      "Id": "8311d5aa-7ce6-4c3c-a1a0-d3a4e563054c",
      "Name": "Auberginecurry (indorock)",
      "NumberOfPortions": 5,
      "WebAccountId": "00000000-0000-0000-0000-000000000000",
      "Items": [
        {
          "Id": "aaa1280d-d3be-4cbe-86c4-757a2e237276",
          "CombinedProductId": "8311d5aa-7ce6-4c3c-a1a0-d3a4e563054c",
          "Amount": 1700.0,
          "ProductName": "Aubergine",
          "UnitName": "gram",
          "ProductUnitId": "455cc5a2-a64e-45c2-bf05-0126d8924683",
          "BrandProductId": null,
          "BrandName": null,
          "BaseProductSynonymId": null,
          "OwnProductUnitId": null,
          "PreparationMethodName": null,
          "IsCustom": false
        }
      ]
    }
  ]
}
```

Wire format is **PascalCase**. Per-account server values: recipe `Id`, ingredient
`Id`, ingredient `CombinedProductId`. Shared food-DB references (portable between
accounts): `ProductUnitId`, `BrandProductId`, `BaseProductSynonymId`. Unknown
whether `OwnProductUnitId` (a per-account custom unit) or `IsCustom` items port —
the write spike answers that.

### `GET combinedproduct/{id}` — CONFIRMED reachable

Returns a single recipe (`200`). Same shape as one `Items` entry.

---

## Create / update a recipe — CONFIRMED

### `PUT combinedproduct`  (collection path — same call for create and update)

Body is **camelCase** (the read format is PascalCase):

```json
{
  "id": "<UUID>",
  "name": "Bami",
  "numberOfPortions": 4,
  "items": [
    { "id": "<UUID>", "productUnitId": "<catalog uuid>", "amount": 300 }
  ]
}
```

- **The recipe `id` is client-generated and honoured.** Reuse the same `id` to
  update; use a fresh one to create. There is no `/{id}` in the path.
- Each item needs an `id` in the body, but **the server assigns its own** and
  ignores the one sent.
- Minimum per item: `id`, `productUnitId`, `amount`. The server fills
  `productName` / `unitName` / `brandName` from `productUnitId`.
- **`preparationMethodName` sent in the body is ignored** — the server keeps its
  default ("Gekookt of gewokt (zonder olie of vet)"). So preparation method
  cannot be propagated and MUST NOT be part of the change fingerprint.
- Response: `200`, the full recipe in read (PascalCase) format, `Id` = the one
  sent. `numberOfPortions` and per-item `amount` changes round-trip correctly
  (verified).
- Cross-account: copy each source item's `productUnitId` / `brandProductId` /
  `baseProductSynonymId` verbatim (they are shared-catalog references).
  `isCustom` items were not exercised.

### `DELETE combinedproduct/{id}` — CONFIRMED

`200`, body `{ "Id": "<id>", "Type": 5 }`. The recipe leaves `GET combinedproduct`
(the list) immediately.

**Caveat:** `GET combinedproduct/{id}` still returns `200` with the old body
shortly after a delete — it is stale. **The list is the source of truth**, and
the sync only reads the list, so this does not matter here.

Not used by the sync (deletions are never propagated) but kept as
`Client.DeleteRecipe` for spike/manual cleanup.

---

## Not used by this feature

- `POST combinedproduct/{id}/addtodiary` — adds a recipe to the food diary.
  Confirmed to exist (Vreetmeter uses it); irrelevant here.
- Everything under `consumption`, `product`, `baseproduct`, `brandproduct`,
  `userfavorite`, `search`, `consumptiondaynote` — out of scope (recipes only).

---

## Client behavior requirements

- **Timeouts**: every HTTP call has a context deadline (suggest 30s); a run has an
  overall deadline (suggest 10m) after which it aborts as `failed`.
- **Retries**: one retry on a connection error or `5xx`, with a short backoff.
  No retry on `4xx`.
- **Rate**: sequential calls, no parallel fan-out across recipes in the first
  cut (≤200 recipes × a few calls is fine serially and is gentle on the API).
- **Read-back after write**: after a create or update, re-fetch the affected
  recipe (or re-list) and compute its fingerprint from what the server actually
  stored — that fingerprint becomes the new baseline, so a lossy write shows up
  as a mismatch next run instead of silently.

---

## Spike log

**2026-09-10 — auth + read paths (blind probe + one real account, token mode)**

Confirmed:

- Base URL `https://api3-mijn.voedingscentrum.nl/api/` live over HTTP/2.
- `version: 4.6.0` + `platform: iOS` are mandatory; anything else → `426`.
- `POST account/credentials` is POST-only (`OPTIONS` → `405 Allow: POST`).
- Login needs `Authorization: Basic <token>:<deviceId>` too; without it → `404`
  for any credential body (valid or not). So the token is device-bound and
  pre-existing — this service takes it as config, it cannot mint one.
- Login `200` body is the full Account object, PascalCase, `Token` field.
- `GET combinedproduct` with just the `Authorization` header → `200`,
  `{ "Items": [...] }`, all recipes in one response, PascalCase, ingredient
  fields as documented above (more than Vreetmeter showed).
- `GET combinedproduct/{id}` → `200`.
- Verified in code: `internal/eetmeter` (token mode) parses all 44 real recipes —
  see `live_test.go` (`EETMETER_LIVE_TOKEN` / `EETMETER_LIVE_DEVICE_ID`).

**2026-09-10 — write paths (one throwaway recipe, created → updated → deleted)**

Confirmed:

- Create + update are the **same** call: `PUT combinedproduct` (collection),
  camelCase body, client-generated recipe `id` honoured, item `id`s re-assigned
  by the server.
- `numberOfPortions` and item `amount` round-trip. `preparationMethodName` in the
  body is **ignored** (server keeps its default) → excluded from the fingerprint.
- `DELETE combinedproduct/{id}` → `200 {"Id":…,"Type":5}`; the recipe leaves the
  list at once. `GET …/{id}` is briefly stale afterwards (list is authoritative).
- The engine's delete-then-recreate update fallback is therefore not needed.

Not exercised: creating an `isCustom` / own-product ingredient on another account.
