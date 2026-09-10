# Feature Specification: Readable Action Labels

**Feature Branch**: `003-readable-action-labels`

**Created**: 2026-09-10

**Status**: Draft

**Input**: User description: "make the labels the actions get \"createdInB\" Human readable in the UI. Use the labels from the env and add readable labels in the UI to say whether createdInB means \"created at partner's\" (for example, use something better)"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Understand what happened to each recipe (Priority: P1)

After a sync runs, the operator opens the dashboard and reads the per-recipe
"Details" list. Today each row shows a raw internal code such as `createdInB`,
`updatedInA`, `flaggedConflict`, or `noop`. The operator wants each row to state,
in plain language and naming the actual account, what happened — for example
"Added to Partner's recipes" instead of `createdInB`.

**Why this priority**: This is the whole point of the request. The Details list
is the primary place raw codes leak into the UI, and it is the view the operator
uses to confirm a run did what they expected. Without readable text the operator
has to memorise which letter is which account and what each camelCase token means.

**Independent Test**: Trigger a sync (real or dry run) that produces at least one
of each action, open the dashboard, and confirm every row of the Details table
reads as a natural phrase that names the affected account by its configured
label, with no camelCase token or bare "A"/"B" visible.

**Acceptance Scenarios**:

1. **Given** account A's label is "You" and account B's label is "Partner", **When** a run records a recipe with action `createdInB`, **Then** the Details row for that recipe reads as a plain-language phrase meaning the recipe was newly added to Partner's account (e.g. "Added to Partner's recipes").
2. **Given** the same labels, **When** a run records actions `updatedInA`, `linked`, `flaggedConflict`, `resolutionApplied`, `linkRetired`, `skipped`, and `noop`, **Then** each corresponding Details row shows a distinct, readable phrase and none shows the raw code.
3. **Given** a recipe row also has a "Note"/detail string, **When** the row is displayed, **Then** the readable action and the note remain separately legible.
4. **Given** an operator hovers or otherwise inspects a readable action, **When** they want the underlying code, **Then** the raw action token is still discoverable (e.g. as a tooltip / title attribute) without cluttering the default view.

---

### User Story 2 - Account names come from configuration, not hard-coded (Priority: P1)

The operator has set `EETMETER_ACCOUNT_A_LABEL` and `EETMETER_ACCOUNT_B_LABEL`
(defaulting to `a` / `b`). Every readable action phrase, and every other place the
UI refers to "account A" or "account B", must use these labels so the two
accounts are identifiable by name throughout the interface.

**Why this priority**: A readable phrase that still says "created in B" solves
nothing. The labels already exist in config and are already surfaced in run
summaries; this feature must consume them consistently.

**Independent Test**: Set the two label variables to recognisable values, restart
the service, and confirm those exact values appear in the readable action
phrases (and anywhere else A/B is referenced), and that no letter "A"/"B"
account identifier remains in the rendered pages.

**Acceptance Scenarios**:

1. **Given** `EETMETER_ACCOUNT_B_LABEL=Partner`, **When** the dashboard renders an action that targets account B, **Then** the phrase contains "Partner" and not "B".
2. **Given** the labels are left at their defaults (`a` / `b`), **When** any account-targeting action renders, **Then** the phrase still forms a grammatical sentence using "a" / "b" as names (no broken output).
3. **Given** a stored run summary carries its own account labels, **When** that historical run is displayed, **Then** the labels shown for that run are consistent with how the rest of the page names the accounts.

---

### User Story 3 - Consistent readable actions across every UI surface (Priority: P2)

Wherever an action or account identifier is shown outside the Details table — the
"Per account" and "Run totals" count tables, the conflicts page, any flash
messages — the wording uses the same readable vocabulary and the same account
labels, so the operator learns one set of terms.

**Why this priority**: Lower than P1 because the count tables and conflicts page
are already mostly readable; this is about removing the last inconsistencies and
making the Details phrasing and the summary phrasing agree. Still valuable, but
the feature delivers its core value with User Stories 1 and 2 alone.

**Independent Test**: Walk every rendered UI page after a run that produced
conflicts and resolutions, and confirm the same action concept is described with
the same words in each place and every account reference uses the configured
label.

**Acceptance Scenarios**:

1. **Given** the Details table calls action `flaggedConflict` "Flagged as a conflict", **When** the "Run totals" table shows the same concept, **Then** it uses matching wording.
2. **Given** the conflicts page names the resolution winner, **When** it is rendered, **Then** it uses the configured account label (this already holds today and MUST NOT regress).

---

### Edge Cases

- **Unknown / future action code**: a run summary contains an action string the UI
  does not recognise. The UI MUST fall back to showing the raw string rather than
  rendering blank or erroring.
- **Empty label**: a label variable is set to an empty string. The UI MUST fall
  back to the default identifier (`a` / `b`) so phrases stay grammatical.
- **Long or unusual label**: a label containing spaces, punctuation, or many
  characters MUST render as text without breaking page layout, and MUST be
  escaped so it cannot inject markup.
- **Identical labels**: both accounts share the same label. The phrases still
  render; the operator accepts the ambiguity (out of scope to disambiguate).
- **Historical run vs. current config**: a stored summary's embedded labels differ
  from the current environment variables. The displayed run uses the labels
  embedded in that summary (assumption below).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The UI MUST render every per-recipe action from a run summary as a human-readable phrase. The raw action tokens (`createdInA`, `createdInB`, `updatedInA`, `updatedInB`, `linked`, `flaggedConflict`, `resolutionApplied`, `linkRetired`, `skipped`, `noop`) MUST NOT appear as the primary visible text of a Details row.
- **FR-002**: For the four side-specific actions (`createdInA`, `createdInB`, `updatedInA`, `updatedInB`), the readable phrase MUST identify which account was affected by that account's configured label, not by the letter "A" / "B" and not by the internal id.
- **FR-003**: Account labels MUST be taken from the existing configuration (`EETMETER_ACCOUNT_A_LABEL` / `EETMETER_ACCOUNT_B_LABEL`), and where a displayed run summary carries its own embedded labels, those MUST be used for that run.
- **FR-004**: Each distinct action MUST map to a distinct readable phrase, so an operator can tell two different actions apart from the text alone.
- **FR-005**: The readable phrases MUST read as natural language for a non-technical reader (e.g. "Added to Partner's recipes", "Updated in your recipes", "Linked as the same recipe", "Flagged as a conflict", "No change"), not merely a spaced-out version of the code (not "created in b").
- **FR-006**: The underlying raw action code MUST remain discoverable for troubleshooting (e.g. via a `title` tooltip on the row) without being shown as the default text.
- **FR-007**: If the UI encounters an action string it does not recognise, it MUST display that string as-is and MUST NOT error or render an empty cell.
- **FR-008**: If an account label is empty, the UI MUST substitute the default identifier (`a` / `b`) so every phrase stays well-formed.
- **FR-009**: Account labels MUST be HTML-escaped wherever they are interpolated into a phrase.
- **FR-010**: Action wording used in the "Per account" and "Run totals" count tables MUST be consistent with the wording used for the same concept in the Details table.
- **FR-011**: The machine-readable API responses (e.g. `GET /sync/last`) MUST continue to expose the raw action tokens unchanged; this feature changes presentation only (assumption below).
- **FR-012**: The existing behaviour of naming the conflict-resolution winner by account label MUST be preserved.

### Key Entities *(include if data involves data)*

- **Action**: one of a fixed, known set of outcome codes attached to a recipe in a
  run summary. Has: a stable raw token (unchanged by this feature), a readable
  phrase, and — for side-specific actions — a notion of which account it targets.
- **Account label**: the human-facing name of one of the two synced accounts.
  Sourced from environment configuration, with a fallback identifier; may also be
  embedded in a stored run summary.
- **Readable action rendering**: the mapping from (raw action token + the two
  account labels) to the phrase shown in the UI, plus the retained raw token for
  inspection.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On the dashboard after a run, 100% of Details rows show a readable phrase; 0% show a raw camelCase action token as their primary text.
- **SC-002**: For every side-specific action, the affected account is named using the configured label in 100% of rendered rows; the letters "A" / "B" appear as account identifiers in 0 places across the rendered UI.
- **SC-003**: A first-time operator who has never seen the internal codes can, without documentation, correctly state which account each Details row refers to and what happened, for all 10 action types.
- **SC-004**: Changing an account label in configuration and restarting is reflected in every readable phrase on the next page load, with no code change.
- **SC-005**: An unrecognised action token and an empty label each render a sensible page (no error, no blank cell) in 100% of cases.

## Assumptions

- **UI-only change**: The internal action constants and the JSON API contract stay
  as they are. Only what the browser sees changes. (Consistent with the
  constitution: the summary JSON is "served verbatim".)
- **Two accounts, labelled A and B**: The system syncs exactly two accounts,
  identified internally as `a` / `b`, each with one configurable label. No
  multi-account or per-account-per-run label management is introduced.
- **Label semantics**: `...InB` means the recipe was created/updated in account
  B's data (the "other" side from A's perspective). The readable phrasing will
  reflect "the recipe now exists / was changed in <label>'s recipes".
- **Possessive / grammatical phrasing** is chosen at implementation time from the
  label as a plain name; labels like "You" / "Partner" are expected but the
  phrasing must not break for arbitrary strings.
- **Historical runs** display using the labels embedded in their stored summary;
  if absent, the current configured labels are used, then the `a` / `b` fallback.
- **No localisation**: phrases are English only, matching the rest of the UI.
- **Reuses** the existing `EETMETER_ACCOUNT_A_LABEL` / `_B_LABEL` configuration and
  the existing summary/label plumbing already surfaced in the UI.
