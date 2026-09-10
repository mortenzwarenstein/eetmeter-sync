# Feature Specification: Recipe Sync

**Feature Branch**: `001-recipe-sync`

**Created**: 2026-09-10

**Status**: Draft

**Input**: User description: "There is an app called \"Mijn Eetmeter\". This app is a dutch calory tracker. You can also store recipes in it. My partner and I both use this, so I would like to be able to sync these recipes."

## User Scenarios & Testing *(mandatory)*

The users are two people (the maintainer and their partner) who each keep recipes
in their own "Mijn Eetmeter" account and want a shared, converged recipe
collection without copying recipes between accounts by hand.

### User Story 1 - New recipe appears in the other account (Priority: P1)

One partner adds a recipe in their Mijn Eetmeter account. Without anyone doing
anything else, that recipe shows up in the other partner's account. This works in
both directions: whoever creates a recipe, the other one gets it.

**Why this priority**: This is the core of the request. Even if nothing else
existed — no edit propagation, no conflict handling — automatically copying new
recipes both ways already removes the manual re-entry that motivated the feature.

**Independent Test**: Create a recipe in account A that does not exist in account
B, run a sync, and confirm an equivalent recipe now exists in account B with the
same content. Repeat with the roles reversed.

**Acceptance Scenarios**:

1. **Given** a recipe exists in account A and no recipe of that name exists in
   account B, **When** a sync runs, **Then** a matching recipe is created in
   account B and the two are recorded as linked.
2. **Given** a recipe exists in account B and not in account A, **When** a sync
   runs, **Then** a matching recipe is created in account A and linked.
3. **Given** a recipe was copied to the other account in a previous sync, **When**
   a later sync runs and neither side has changed, **Then** no duplicate is
   created and nothing is modified.

---

### User Story 2 - Edits propagate to the other account (Priority: P2)

A recipe that already exists in both accounts is edited by one partner. After the
next sync, the other partner's copy reflects the change.

**Why this priority**: Recipes get tweaked over time (portion sizes, ingredients,
notes). Without this, the two collections drift apart after the first copy and the
sync only helps once per recipe.

**Independent Test**: With a recipe already linked across both accounts, change it
in account A only, run a sync, and confirm account B's copy now matches. Repeat
with the change made in account B.

**Acceptance Scenarios**:

1. **Given** a linked recipe that has changed in account A only since the last
   successful sync, **When** a sync runs, **Then** account B's copy is updated to
   match account A and the new state is recorded as the synced baseline.
2. **Given** a linked recipe that has changed in account B only, **When** a sync
   runs, **Then** account A's copy is updated to match.
3. **Given** a linked recipe where the change on one side makes it identical to
   the other side's current content, **When** a sync runs, **Then** nothing is
   written and no change is reported.

---

### User Story 3 - Conflicting edits are flagged, not overwritten (Priority: P3)

Both partners edit the same recipe between two syncs. The tool does not pick a
winner on its own — it leaves both copies untouched and marks the recipe so the
maintainer can decide which version to keep.

**Why this priority**: Silently overwriting one partner's edit is the worst
outcome the feature could produce. It is lower priority than P1/P2 only because it
is less frequent, not because it is optional.

**Independent Test**: Change the same linked recipe in both accounts (differently)
between syncs, run a sync, and confirm neither copy was modified and the recipe
appears in a list of flagged conflicts. Then record a resolution and confirm the
next sync applies it.

**Acceptance Scenarios**:

1. **Given** a linked recipe that has changed on both sides since the last
   successful sync, **When** a sync runs, **Then** neither copy is modified and
   the recipe is added to the set of unresolved conflicts, with the run summary
   noting it was skipped.
2. **Given** an unresolved conflict, **When** the maintainer records that account
   A's version wins, **Then** the next sync copies account A's current content to
   account B, clears the conflict, and records the new synced baseline.
3. **Given** an unresolved conflict, **When** either partner edits that recipe
   again before it is resolved, **Then** it remains a single unresolved conflict
   and the resolution acts on whatever the content is at resolution time.

---

### User Story 4 - Run a sync on demand and see what happened (Priority: P2)

The maintainer wants to trigger a sync immediately (for example, right after
adding several recipes) rather than wait for the next morning, and wants to see
what each run did.

**Why this priority**: The scheduled daily run covers normal use, but an
on-demand trigger is needed for setup, testing, and impatience. It is cheap to
provide and makes the feature usable from day one.

**Independent Test**: Call the manual trigger from a browser, confirm a sync
starts, and confirm a summary of created / updated / flagged / skipped recipes
per account is available afterward.

**Acceptance Scenarios**:

1. **Given** no sync is currently running, **When** the maintainer calls the
   manual trigger, **Then** a sync starts and the request is acknowledged.
2. **Given** a sync is already running, **When** the manual trigger is called
   again, **Then** the second request is rejected with a clear indication that a
   sync is in progress, and the running sync is unaffected.
3. **Given** a sync has finished (scheduled or manual), **When** the maintainer
   checks the result, **Then** they see a summary listing, per account, which
   recipes were created, updated, flagged as conflicts, and skipped.

---

### Edge Cases

- **Same name, independent origins**: Both partners create a recipe with the same
  name but different content before the two are ever linked. The recipes are
  linked by name on the first sync; because both differ from an empty baseline,
  the pair is treated as a conflict and flagged rather than one side overwriting
  the other.
- **Duplicate names within one account**: If an account contains two recipes with
  the same name, the match is ambiguous. That name is skipped and reported, and
  no copy or update is made for it until it is resolved manually.
- **Recipe renamed on one side**: A rename looks like the old recipe disappearing
  and a new one appearing. The old pair is retired (see deletion handling) and
  the renamed recipe is treated as new and copied back to the other account,
  producing a one-time duplicate under the old name on the renamer's side.
  Detecting renames is out of scope.
- **Deletion on one side**: If a previously linked recipe no longer exists in one
  account, the deletion is not propagated. That pair is retired from syncing, the
  surviving copy is left untouched, and the retirement is noted in the run
  summary.
- **External service unavailable or credentials rejected**: If either account
  cannot be reached or authentication fails, the run aborts without applying any
  changes to either account, and the failure is recorded in the run summary. The
  next run retries from scratch.
- **Run interrupted partway**: If a sync is killed mid-run (deployment, node
  eviction), the stored linking and baseline data is not left inconsistent, and
  the next run reconciles from the current live state of both accounts.
- **Overlapping runs**: A scheduled run and a manual trigger (or two manual
  triggers) cannot run at the same time; only one sync runs at a time.
- **First sync with pre-existing recipes on both sides**: Recipes that already
  exist in both accounts are linked by name; identical ones need no change,
  differing ones are flagged as conflicts; recipes on only one side are copied to
  the other. The run must complete or be safely resumable even if the collection
  is large.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST connect to two configured Mijn Eetmeter accounts using
  stored credentials for each.
- **FR-002**: System MUST retrieve the complete set of recipes, with their full
  content, from each account on every run.
- **FR-003**: System MUST link recipes across the two accounts by matching on
  recipe name on first encounter, and MUST persist those links so that later runs
  reuse them instead of re-matching.
- **FR-004**: System MUST retain, per linked recipe, the content of each side as
  of the last successful sync, and MUST use it to determine whether each side has
  changed since then.
- **FR-005**: When a recipe exists in one account, is not linked, and no
  same-named recipe exists in the other account, System MUST create an equivalent
  recipe in the other account and record the link.
- **FR-006**: When a linked recipe has changed on exactly one side since the last
  successful sync, System MUST update the other side to match and record the new
  content as the synced baseline for both sides.
- **FR-007**: When a linked recipe has changed on both sides since the last
  successful sync, System MUST leave both copies unmodified and record the recipe
  as an unresolved conflict.
- **FR-008**: System MUST let the maintainer see all unresolved conflicts and
  record a resolution for each that names which account's current version to
  keep.
- **FR-009**: On the next run after a conflict resolution is recorded, System MUST
  copy the chosen side's current content to the other side, clear the conflict,
  and record the new synced baseline.
- **FR-010**: System MUST NOT propagate deletions. A user deleting a recipe in
  one account MUST NOT cause its counterpart in the other account to be deleted
  (that case is handled by FR-011). The only delete System is permitted to issue
  is a delete-then-recreate of the **receiving** copy within an already-linked
  pair while applying an update to that pair, and only where the API offers no
  in-place update; whether that fallback is needed is settled by the API spike.
- **FR-011**: When a previously linked recipe no longer exists in one account,
  System MUST retire that link, leave the surviving copy untouched, and report
  the retirement.
- **FR-012**: System MUST run a sync automatically once per day at a configured
  time.
- **FR-013**: System MUST expose an on-demand trigger that starts a sync and can
  be invoked from a web browser.
- **FR-014**: System MUST run at most one sync at a time; a trigger received while
  a sync is in progress MUST be rejected with a response indicating a sync is
  already running, without disturbing the running sync.
- **FR-015**: System MUST produce, for each run, a summary that lists per account
  the recipes created, updated, flagged as conflicts, skipped (with reason), and
  links retired, and whether the run succeeded or failed.
- **FR-016**: System MUST make the most recent run summary available to the
  maintainer after the run completes.
- **FR-017**: If an account is unreachable or authentication fails during a run,
  System MUST abort the run without having applied any change to either account,
  and MUST record the failure in the run summary.
- **FR-018**: A failed, aborted, or interrupted run MUST NOT leave the stored
  links, baselines, or conflict records in an inconsistent state; the next run
  MUST be able to reconcile purely from the current live state of both accounts
  plus the stored links.
- **FR-019**: When System copies or updates a recipe, it MUST reproduce every
  piece of recipe content that Mijn Eetmeter stores, so that the two copies are
  equivalent and no detail is dropped.
- **FR-020**: Account credentials MUST be stored so that they are not exposed in
  run summaries, logs, or error messages.
- **FR-021**: Stored links, baselines, and conflict records MUST survive restarts
  and redeployments of the system.

### Key Entities *(include if feature involves data)*

- **Account**: One of the two Mijn Eetmeter user accounts being kept in sync.
  Identified by its login; has credentials. Exactly two exist.
- **Recipe**: A named dish stored in an account, with whatever content Mijn
  Eetmeter holds for it (name, ingredients/components, portions, and any other
  fields). The exact set of fields is determined by Mijn Eetmeter.
- **Recipe Link**: The association between a recipe in account A and its
  counterpart in account B. Holds the last-synced content (or a fingerprint of
  it) for each side, and a status: active, retired, or in conflict.
- **Conflict**: A recipe link where both sides changed since the last successful
  sync. Holds enough to show the maintainer both versions and to accept a
  resolution (which side wins). Cleared once resolved and applied.
- **Sync Run**: One execution of the sync, scheduled or manual. Records start and
  end time, trigger type, success or failure, and the per-account summary of what
  changed.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A recipe created in one account and not present in the other is
  present in the other account after the next scheduled run (within 24 hours with
  no manual action), and within 5 minutes when a manual trigger is used, for a
  combined collection of up to 200 recipes.
- **SC-002**: An edit made to a recipe on one side only is reflected on the other
  side after the next sync, with every other field of that recipe unchanged.
- **SC-003**: In 100% of cases where both partners change the same recipe between
  two syncs, neither copy is modified and the recipe is reported as a conflict.
- **SC-004**: The maintainer can start a sync from a browser, get an
  acknowledgement within 5 seconds, and view a per-account summary of the run
  once it completes.
- **SC-005**: Over 30 days of normal use, the two accounts' recipe collections
  contain the same recipes with the same content, except for recipes that are
  currently flagged as conflicts and pairs retired due to a one-sided deletion.
- **SC-006**: Across 50 consecutive runs, including runs deliberately interrupted
  partway, no run leaves a recipe half-written in either account and no run leaves
  the stored links or conflict records unusable by the following run.
- **SC-007**: No run summary, log line, or error message produced by the system
  contains an account password.

## Assumptions

- **API access**: Mijn Eetmeter exposes an HTTP API that the mobile app uses and
  that accepts the same account credentials the partners already use. Its
  behavior is understood from the community "Vreetmeter" reverse-engineering
  project (an unofficial iOS client). Per the project constitution, the actual
  request and response shapes will be verified against the live API before any
  parsing is built, during the planning phase — this spec does not assume the
  reverse-engineered details are complete or current.
- **Two accounts, both the maintainer's to configure**: There are exactly two
  accounts (the maintainer and their partner), configured once at deployment.
  Supporting more accounts is explicitly out of scope for now.
- **Recipe identity**: Recipe names are effectively unique within an account and
  are stable. A recipe rename is treated as a delete plus a create; rename
  detection is not attempted.
- **Direct editing continues**: Both partners keep editing recipes directly in
  the Mijn Eetmeter app between runs; the sync is the only automated writer.
- **Scope is recipes only**: Diary entries, consumed meals, weight, goals, and
  every other kind of Mijn Eetmeter data are out of scope.
- **Deletions are never propagated**: A recipe removed on one side is never
  removed on the other; the pair is simply retired from syncing.
- **Change detection by content**: "Changed since last sync" is determined by
  comparing a recipe's current content against a stored representation (its
  content or a fingerprint of it) captured at the last successful sync. There is
  no reliable per-recipe modification timestamp assumed to be available.
- **Hosting environment**: The system runs as a long-lived service in the
  maintainer's Kubernetes cluster, with persistent storage that survives restarts
  for the links/baselines/conflicts, a scheduler for the daily run, and outbound
  network access to the Mijn Eetmeter API.
- **Single resolver**: Only the maintainer resolves conflicts. No multi-person
  review or approval workflow is needed.
- **History depth**: Only the most recent run summary needs to be retained;
  keeping a longer history of past runs is not required.

## Out of Scope

- Syncing anything other than recipes.
- More than two accounts, or accounts not controlled by the maintainer.
- Real-time or near-real-time sync (a daily run plus a manual trigger is
  sufficient).
- Detecting that a recipe was renamed rather than deleted and recreated.
- Propagating deletions between accounts.
- A polished user interface; the manual trigger and the conflict review can be
  minimal browser-reachable endpoints.
- An audit trail or run history beyond the latest run summary.
- Resolving conflicts by merging field-by-field; resolution is whole-recipe,
  one side wins.
