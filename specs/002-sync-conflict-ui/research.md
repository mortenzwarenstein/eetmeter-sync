# Phase 0 Research: Sync & Conflict UI

No `NEEDS CLARIFICATION` items remained after the spec's Q&A. This file records
the design decisions the plan rests on, each as Decision / Rationale /
Alternatives considered.

---

## 1. Rendering approach

**Decision**: Server-rendered HTML via the standard library `html/template`, with
the template set `embed`ded into the binary and a single inline `<style>` block in
the layout. No JavaScript framework, no bundler, no build step.

**Rationale**: The spec's scope is four small pages for one operator. `html/template`
gives contextual auto-escaping (covers FR-019 for rendered values). Embedding
matches the existing pattern in `internal/store` (embedded migrations) and keeps
the deploy artifact a single binary. Zero new dependencies satisfies Constitution
principle I.

**Alternatives considered**:
- *Single-page app (React/Svelte/etc.)* — rejected: adds a toolchain, a build
  step, and multiple dependencies for four pages; contradicts principle I and the
  spec's "styling is minimal, no external UI framework" assumption.
- *Separate frontend service* — rejected by the maintainer during spec Q&A ("pages
  from the same service").
- *A templating dependency (templ, quicktemplate)* — rejected: `html/template` is
  sufficient and already in the stdlib.

---

## 2. Authenticated access when a token is configured

**Decision**: When `SYNC_API_TOKEN` is set, `/ui/*` (except `/ui/login`) requires a
valid session cookie. Sign-in is a one-field form; the submitted secret is
compared to `SYNC_API_TOKEN` with `crypto/subtle.ConstantTimeCompare`. On success
the server creates an in-memory session — an opaque 256-bit id from
`crypto/rand`, held in a mutex-guarded `map[string]session` with a 30-day sliding
expiry — and sets it as cookie `eetmeter_ui_session` (`HttpOnly`, `SameSite=Lax`,
`Path=/ui`, `Max-Age` 30 days, `Secure` when the request arrived over HTTPS or
carries `X-Forwarded-Proto: https`). `POST /ui/logout` deletes the server-side
entry and expires the cookie. When `SYNC_API_TOKEN` is empty, `/ui/*` is open and
`/ui/login` redirects to `/ui`.

**Rationale**: A session cookie is what a browser can carry across plain
navigations; the JSON API's `Authorization: Bearer` header cannot be set by a
link click. In-memory sessions need no schema change, no migration, and no signing
key. For a single operator, losing sessions on a redeploy (forcing a re-login) is
an acceptable cost and is simpler than any persistent or stateless scheme. The
constant-time compare avoids a timing oracle on the shared secret.

**Alternatives considered**:
- *Cookie value = the API token itself, middleware compares it* — rejected: parks
  a long-lived secret in the browser cookie jar indefinitely and offers no
  server-side revocation on logout.
- *Signed stateless cookie / JWT* — rejected: introduces signing-key generation
  and storage, more code, and still no clean revocation; disproportionate to one
  user.
- *HTTP Basic auth* — rejected by the maintainer during spec Q&A; also has no
  logout and prompts on every browser that lacks the cached credential.
- *Persistent sessions table* — rejected: a migration and store code for a
  benefit (surviving redeploys) that one operator does not need.

**Note for implementation**: the 30-day sliding TTL is hardcoded. Add a
`UI_SESSION_TTL` env var only if a concrete need appears (principle I).

---

## 3. CSRF protection

**Decision**: Rely on `SameSite=Lax` on the session cookie plus the fact that all
state-changing actions are `POST`. No per-form CSRF token.

**Rationale**: `SameSite=Lax` stops the session cookie from riding along on
cross-site `POST` form submissions, which is the CSRF vector here. The service is
a single-user tool behind a cluster Ingress, not a multi-tenant app. A CSRF-token
scheme would need a per-session secret threaded into every form and validated on
every POST — real plumbing for a threat this deployment does not meaningfully
face.

**Alternatives considered**:
- *Synchronizer / double-submit CSRF token* — rejected as disproportionate; can be
  added later if the deployment model changes (e.g. shared access).

---

## 4. Live result view while a run is in progress

**Decision**: The layout emits `<meta http-equiv="refresh" content="3">` only when
the page being rendered reflects a run still in progress (engine reports running,
or the latest summary's `result == "running"`). Once the run finishes, the next
render omits the tag and the page is static.

**Rationale**: Satisfies FR-006 and SC-003 (reflect completion within 10 s) with
zero JavaScript, keeping the core experience within FR-021's "plain navigation".
A 3-second cadence is well inside the 10-second bound.

**Alternatives considered**:
- *JavaScript `fetch` polling of `/sync/last`* — rejected: pulls scripting into a
  path the spec wants navigable without it; no user benefit over meta-refresh at
  this cadence.
- *Server-Sent Events / WebSocket* — rejected: connection lifecycle and
  reconnection handling are complexity out of proportion to "show me when it's
  done".

---

## 5. Per-run dry-run wiring

**Decision**: Make dry-run a parameter:
`Engine.Trigger(trigger string, dryRun bool)` and
`Engine.RunSync(ctx, trigger string, dryRun bool)`. Thread it through
`execute`/`apply`; remove the construction-time `Engine.dryRun` field and the
`dryRun` argument to `New`. Call sites:
- `main.go` scheduler closure and startup trigger pass `cfg.DryRun`.
- JSON `POST /sync` passes `cfg.DryRun` (behaviour identical to today — the
  `SYNC_DRY_RUN` env default still governs it).
- `POST /ui/sync` passes `formCheckbox || cfg.DryRun` — the UI toggle can turn a
  single run into a dry run, but cannot turn a real run on when the whole service
  is deployed in dry-run mode.

**Rationale**: FR-008 requires the dry-run choice per triggered run, which a
construction-time flag cannot express. Passing a bool is the minimal change. The
`formCheckbox || cfg.DryRun` rule keeps a global `SYNC_DRY_RUN=true` deployment
safe from being overridden in the browser.

**Alternatives considered**:
- *Add `?dryRun=1` to the JSON `POST /sync`* — rejected: changes the 001 HTTP
  contract for no requirement (the UI has its own route).
- *Second engine instance constructed with `dryRun=true`* — rejected: two engines
  sharing one store and one mutex is more surface than a bool.

---

## 6. Route layout

**Decision**: All UI routes under `/ui/`:
`GET /ui`, `POST /ui/sync`, `GET /ui/conflicts`,
`POST /ui/conflicts/{linkID}/resolve`, `GET /ui/login`, `POST /ui/login`,
`POST /ui/logout`. The existing `GET /` JSON index is unchanged except for one
added line in its `routes` list pointing a human to `/ui`.

**Rationale**: A dedicated prefix keeps the HTML surface clearly separate from the
JSON surface, lets the auth middleware wrap exactly `/ui/*`, and leaves every 001
route byte-for-byte compatible (FR-020).

**Alternatives considered**:
- *Content negotiation on `/` (HTML for browsers, JSON for API clients)* —
  rejected: `Accept`-header branching is fiddly and changes the 001 contract's
  response for `GET /`.

---

## 7. Flash messages after POST

**Decision**: Post/Redirect/Get with a one-shot `?msg=<code>` query parameter
(`started`, `already-running`, `resolved`, `stale`, `bad-credentials`). The target
page maps the code to a short banner. No server-side flash storage.

**Rationale**: Avoids re-POST on refresh, needs no session-tied flash store, and
the set of outcomes is tiny and closed.

**Alternatives considered**:
- *Session-stored flash* — rejected: more state for a fixed handful of messages.
