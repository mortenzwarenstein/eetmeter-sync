# Feature Specification: Sync & Conflict UI

**Feature Branch**: `002-sync-conflict-ui`

**Created**: 2026-09-10

**Status**: Draft

**Input**: User description: "I want to have a minimal UI to resolve conflicts as well as to trigger the sync"

## User Scenarios & Testing *(mandatory)*

The single actor is the maintainer, working from a web browser. Today the two
things they most often need to do by hand — start a sync now, and decide which
version of a conflicted recipe wins — are only reachable as raw API calls. This
feature gives those two actions (plus a dry-run option) a minimal set of browser
pages served by the same service, so no command-line tool is needed.

### User Story 1 - Trigger a sync from the browser and see what it did (Priority: P1)

The maintainer opens the UI, clicks a button to start a sync, gets an immediate
on-screen confirmation that the run has started, and can then watch the same page
show the run's per-account summary once it finishes — all without touching a
terminal.

**Why this priority**: "Trigger the sync" is half of the stated request, and it
is the action taken most often (after adding recipes, during setup, when
impatient). A page that only did this would already replace the current
`curl`-the-endpoint workflow and deliver value on its own.

**Independent Test**: With the service running and no sync in progress, open the
UI, start a sync, confirm the page acknowledges it started, wait for the run to
finish, and confirm the page then shows how many recipes were created, updated,
flagged, skipped, and retired per account.

**Acceptance Scenarios**:

1. **Given** no sync is running, **When** the maintainer starts a sync from the
   UI, **Then** the page confirms within a few seconds that a run has started and
   does not block until the run completes.
2. **Given** a sync started from the UI is still running, **When** the maintainer
   stays on the result page, **Then** the page updates to show the finished
   summary once the run completes, without a manual reload.
3. **Given** a sync is already running (scheduled or manual), **When** the
   maintainer tries to start another from the UI, **Then** the UI declines and
   states that a sync is already in progress, and the running sync is unaffected.
4. **Given** no run has ever completed, **When** the maintainer opens the result
   view, **Then** it shows a plain "no runs yet" state, not an error.

---

### User Story 2 - Review a conflict and choose which version wins (Priority: P1)

A recipe was edited on both sides between syncs and is now a flagged conflict. The
maintainer opens the UI, sees the conflict with both accounts' current versions
side by side and the differing fields marked, picks the side that should win, and
is then offered a one-click "resolve and sync now" so the choice takes effect
immediately instead of waiting for the daily run.

**Why this priority**: "Resolve conflicts" is the other half of the request, and
it is the action that currently requires the most care to do by hand (find the
link id, read two JSON blobs, POST the winner). Presenting both versions and
accepting a choice in the browser removes the most error-prone manual step.

**Independent Test**: With at least one unresolved conflict present, open the UI,
confirm both versions of that recipe are shown side by side with differences
marked, choose a winner, accept "sync now", and confirm that after the triggered
run the losing side matches the winning side and the conflict is gone.

**Acceptance Scenarios**:

1. **Given** one or more unresolved conflicts, **When** the maintainer opens the
   conflicts view, **Then** each conflict lists the recipe name, when it was
   detected, and both accounts' current portions and ingredient lists side by
   side, with the fields that differ visually marked.
2. **Given** a displayed conflict, **When** the maintainer selects account A (or
   B) as the winner, **Then** the choice is recorded and the UI offers to start a
   sync immediately.
3. **Given** a recorded resolution and the maintainer choosing "sync now" with no
   run in progress, **When** the triggered run completes, **Then** the losing
   side's recipe matches the winning side and the conflict no longer appears.
4. **Given** the maintainer chooses "sync now" but a run is already in progress,
   **When** the request is made, **Then** the resolution is still recorded and the
   UI reports that a sync is already running.
5. **Given** there are no unresolved conflicts, **When** the maintainer opens the
   conflicts view, **Then** it shows a plain "no conflicts" state.
6. **Given** a conflict shown on the page has already been resolved or its link
   retired elsewhere, **When** the maintainer submits a winner for it, **Then**
   the UI shows a clear "this conflict is no longer open" message and re-displays
   the current list, not an error page.

---

### User Story 3 - Start a dry run to preview changes safely (Priority: P2)

Before letting the sync write anything, the maintainer wants to see what a run
*would* do. From the same trigger control they tick a "dry run" option; the run
computes and reports its plan but changes nothing, and the result is clearly
labelled as a dry run so it can't be confused with a real one.

**Why this priority**: Useful for setup, for sanity-checking after bulk edits,
and for confidence generally, but the feature is usable without it — hence P2, not
P1.

**Independent Test**: Start a sync from the UI with the dry-run option on, confirm
the result view labels it a dry run and lists intended changes, then confirm
neither account and no stored sync state was modified.

**Acceptance Scenarios**:

1. **Given** the trigger control, **When** the maintainer enables the dry-run
   option and starts a sync, **Then** the run reports what would change and makes
   no change to either account or to stored sync state.
2. **Given** a completed dry run, **When** the maintainer views its result,
   **Then** the result is unmistakably marked as a dry run.

---

### User Story 4 - Access is protected when a token is configured (Priority: P2)

When the service is deployed with an access token set, the UI must not be usable
by anyone who simply reaches its URL. The maintainer logs in once with the shared
secret; the browser then remembers that for the session, and there is a way to log
out. When no token is configured, the UI is open, matching the API's behaviour.

**Why this priority**: The service is intended to sit behind a cluster Ingress and
may already be network-restricted, so this is a safety net rather than the primary
gate — but "anyone with the URL can trigger a sync or change a resolution" is not
acceptable when a token has been deliberately configured.

**Independent Test**: With an access token configured, open any UI page in a fresh
browser session and confirm a login is required; log in with the wrong secret and
confirm rejection; log in with the correct secret and confirm pages load and stay
accessible without re-entering it; log out and confirm access is gone. Repeat with
no token configured and confirm no login step appears.

**Acceptance Scenarios**:

1. **Given** an access token is configured and the browser is not logged in,
   **When** the maintainer opens any UI page, **Then** they are shown a login
   prompt and cannot trigger a sync or resolve a conflict until they log in.
2. **Given** the login prompt, **When** the maintainer submits an incorrect
   secret, **Then** login is refused and no action is performed.
3. **Given** a successful login, **When** the maintainer navigates between UI
   pages during the session, **Then** they are not asked to log in again, and a
   logout control is available.
4. **Given** no access token is configured, **When** the maintainer opens any UI
   page, **Then** it loads with no login step.

---

### Edge Cases

- **Trigger while a run is active**: starting a sync (directly or via "resolve and
  sync now") when one is already running never starts a second run; the UI says a
  sync is in progress and the running one is untouched.
- **No history yet**: the result view before the first completed run shows an
  empty state, not an error.
- **No conflicts**: the conflicts view with nothing to resolve shows an empty
  state.
- **Stale conflict**: a conflict resolved or retired in another tab (or by a
  scheduled run) between page load and submission produces a clear "no longer
  open" message and a refreshed list, not a failure page.
- **Run finishes while the page is open**: the result view reflects completion
  within a short interval without the maintainer reloading.
- **Dry-run result must not masquerade as real**: a dry run's result is labelled
  as such everywhere it is shown.
- **Wrong or missing login**: with a token configured, an unauthenticated or
  wrongly-authenticated request reaches no action and no data.
- **Large but bounded data**: a few hundred recipes and a handful of conflicts
  render on one page without pagination; ingredient lists of any realistic length
  stay readable.
- **Service or database unavailable**: if the underlying sync state cannot be
  read, the UI shows a plain error message rather than a blank or broken page.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST serve a browser-reachable UI from the same service that
  performs the sync, with nothing for the maintainer to install or build.
- **FR-002**: The UI MUST provide a control that starts a sync immediately,
  equivalent to the service's existing on-demand trigger.
- **FR-003**: When a sync is started from the UI, the UI MUST confirm the run has
  started without waiting for it to finish.
- **FR-004**: When a sync is already in progress, the UI MUST NOT start another
  and MUST tell the maintainer that a sync is already running, leaving the
  running sync unaffected.
- **FR-005**: The UI MUST show the outcome of the most recent run: its trigger
  type, start and end time, whether it succeeded or failed, and per account the
  counts of recipes created, updated, flagged as conflicts, skipped (with
  reason), and links retired.
- **FR-006**: While a run is in progress, the UI's result view MUST update to
  show the finished summary once the run completes, without the maintainer
  reloading the page.
- **FR-007**: When no run has ever completed, the UI MUST show a clear empty
  state instead of an error.
- **FR-008**: The UI MUST offer a dry-run option when starting a sync; a dry run
  MUST compute and report its intended changes while writing nothing to either
  account or to stored sync state.
- **FR-009**: The UI MUST label a dry-run result unmistakably as a dry run
  wherever that result is displayed.
- **FR-010**: The UI MUST list every unresolved conflict, showing for each the
  recipe name, when it was detected, and both accounts' current versions
  (portions and ingredient list) side by side.
- **FR-011**: For each listed conflict, the UI MUST visually mark which fields or
  ingredient items differ between the two versions.
- **FR-012**: For each listed conflict, the UI MUST let the maintainer choose
  which account's current version wins.
- **FR-013**: After a winner is chosen, the UI MUST record the resolution and
  then offer to start a sync immediately so the resolution takes effect without
  waiting for the scheduled run.
- **FR-014**: If the maintainer opts to sync immediately after resolving while a
  run is already in progress, the resolution MUST still be recorded and the UI
  MUST report that a sync is already running.
- **FR-015**: If a conflict shown on the page is no longer open when the
  maintainer submits a winner (already resolved elsewhere, or its link retired),
  the UI MUST show a clear message and re-display the current list rather than an
  error page.
- **FR-016**: When an access token is configured for the service, the UI MUST
  require the maintainer to authenticate before any UI page is shown or any
  action is performed, and MUST reject an incorrect secret without performing an
  action.
- **FR-017**: After a successful authentication, the UI MUST keep the maintainer
  signed in across page loads for the browser session and MUST provide a way to
  sign out.
- **FR-018**: When no access token is configured, the UI MUST be reachable
  without any authentication step.
- **FR-019**: The UI MUST NOT expose account passwords or the access token in
  page content, addresses, or run summaries.
- **FR-020**: The service's existing machine-readable endpoints for triggering a
  sync, reading the last run, and listing and resolving conflicts MUST remain
  available with unchanged behaviour; the UI is additive.
- **FR-021**: The core actions — starting a sync and resolving a conflict — MUST
  work through ordinary page navigation and form submission; only the
  auto-updating result view may depend on client-side refreshing.
- **FR-022**: If the underlying sync state cannot be read, the UI MUST show a
  plain error message and remain navigable, not a blank or broken page.

### Key Entities *(include if feature involves data)*

- **Sync Run**: One execution of the sync (as defined by the recipe-sync
  feature). The UI reads and displays the most recent one; it stores no run data
  of its own.
- **Conflict**: A recipe link where both sides changed since the last successful
  sync (as defined by the recipe-sync feature), carrying both current versions
  and an optional pending resolution. The UI displays these and submits a chosen
  winner.
- **Operator Session**: The signed-in state of one browser, created by
  authenticating when an access token is configured and ended by signing out or
  by expiry. It represents a single shared operator identity and holds no
  personal data.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: From a browser and nothing else, the maintainer can start a sync
  and see on-screen confirmation that it has started within 5 seconds.
- **SC-002**: After a run finishes, the maintainer can read its full per-account
  summary in the browser without using any other tool.
- **SC-003**: The result view reflects a run's completion within 10 seconds of
  the run finishing, with no manual reload.
- **SC-004**: For any unresolved conflict, the maintainer can view both versions
  and record which side wins in under 1 minute, entirely in the browser.
- **SC-005**: Choosing "resolve and sync now" for a conflict results in the
  winning version being applied to the other account by the end of the triggered
  run, with no scheduled run needed.
- **SC-006**: A dry run started from the UI makes zero changes to either account
  and to stored sync state, and its result is labelled as a dry run in 100% of
  views.
- **SC-007**: With an access token configured, no UI page or action is reachable
  without a valid sign-in; with no token configured, every UI page loads with no
  sign-in step.
- **SC-008**: No page, address, or summary shown by the UI contains an account
  password or the access token.
- **SC-009**: Attempting to start a second sync while one is running is refused
  in 100% of cases, and the running sync always completes normally.

## Assumptions

- The UI is served by the existing eetmeter-sync service and shares its
  deployment, configuration, and network exposure; it is not a separately
  deployed application.
- There is one shared operator identity and one secret — the access token the
  service already accepts. The sign-in step collects that single secret; there
  are no per-user accounts, roles, or password-recovery flows.
- The signed-in state lasts for the browser session and expires after a period of
  inactivity; the exact lifetime is an implementation detail.
- The dry-run option maps to the sync's existing dry-run behaviour, applied to a
  single triggered run rather than only as a service-wide setting.
- The conflict comparison shows the same recipe content the existing conflict
  listing exposes (portions and ingredient items). Resolution is whole-recipe,
  one side wins — no field-by-field merge — consistent with the recipe-sync
  feature.
- Difference marking is a simple visual indicator of which fields or items
  differ, not a full text diff.
- The combined collection is at most a few hundred recipes and unresolved
  conflicts are few, so the UI needs no pagination, search, or filtering.
- Only the most recent run is shown; run history remains out of scope, consistent
  with the recipe-sync feature.
- Use is from a current desktop browser; a mobile-optimised layout is not
  required, though pages should stay legible on a narrow screen.
- Styling is minimal and self-contained; no design system or external UI
  framework is required.

## Out of Scope

- Any UI for configuring the service, managing credentials, or changing the daily
  schedule.
- Run history, an audit trail, or trends beyond the single most recent run.
- Viewing, un-retiring, or otherwise managing retired recipe links.
- Editing recipe content in the browser; resolving a conflict means choosing a
  side, not merging.
- Multiple user accounts, roles, permissions, or password recovery.
- Real-time push updates; periodic refresh while a run is active is sufficient.
- A mobile-first or offline-capable experience.
- Any change to, or removal of, the existing machine-readable endpoints.
