# Feature Specification: Account Configuration

**Feature Branch**: `001-account-configuration`

**Created**: 2026-09-09

**Status**: Draft

**Input**: User description: "eetmeter-sync: let's first make the spec about the base; Fetch the accounts that are supplied, make sure there is a way to configure these accounts"

## Clarifications

### Session 2026-09-09

- Q: Are healthy accounts re-verified on a schedule, and are failed accounts retried automatically? → A: No. Verification is event-driven only — on configure, on secret change, on an operator's on-demand re-check, and when Eetmeter refuses a previously accepted session. Any failure is recorded immediately with its cause category and stays in that state until the operator updates the secret or requests a re-check; there is no background scheduler and no automatic retry loop.
- Q: When an add or rename would reuse a label already in the operator's set, does the system reject or accept-and-flag? → A: Reject the write outright with a clear, non-sensitive error; the existing account is untouched and no second account is created.
- Q: When a second account would use an Eetmeter login identifier already configured under another label, reject or flag? → A: Reject it, the same way a duplicate label is rejected. The login identifier is unique within an operator's set. No "duplicate login" state exists.
- Q: On restart, when a start-up config source still describes an account that was changed or deleted at runtime, which wins? → A: Start-up config wins. On every boot the account set is rebuilt from the start-up configuration sources (deployment environment / config file); runtime add/update/remove changes made since the last start are discarded. Runtime changes are session-only — they exist so no restart is needed (FR-003) — and only verification status is persisted across restarts.
- Q: Does "verify" mean the auth call alone, or auth plus an authenticated read-back? → A: Auth call plus one authenticated read-back. The system authenticates to obtain a session, then uses that session to retrieve the account/profile record; both must succeed for "connected". Two Eetmeter contracts are recorded and tested against fixtures.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Configure and verify the first account (Priority: P1)

An operator has an Eetmeter account and wants eetmeter-sync to act on it. They
supply that account's credentials to the system. The system stores the
credentials safely, contacts Eetmeter to confirm the credentials work, and
reports the account as connected — or, if the credentials are wrong, tells the
operator exactly that.

**Why this priority**: Nothing else in the product is possible until the system
can hold an Eetmeter account and prove it can reach it. This is the minimum
viable slice: one account in, verified status out.

**Independent Test**: Supply one account with valid credentials and confirm its
status becomes "connected" with a last-authenticated time; supply one account
with invalid credentials and confirm its status becomes "credentials rejected"
and that no credential material appears in any response, log, or error.

**Acceptance Scenarios**:

1. **Given** no accounts are configured, **When** the operator supplies a valid
   set of Eetmeter credentials for an account, **Then** the account appears in
   the system with status "connected" and a recorded time of last successful
   authentication.
2. **Given** an account is being configured with an incorrect secret, **When**
   the system attempts to authenticate it, **Then** the account is recorded with
   status "credentials rejected", the operator can see which account failed and
   why, and no working session is retained.
3. **Given** an account has just been configured, **When** the operator views the
   account, **Then** the stored secret is never shown — only an indicator that a
   secret is set and when it was last updated.

---

### User Story 2 - Deploy with accounts already supplied (Priority: P1)

An operator deploys eetmeter-sync to a hosting environment and provides the
account credentials as part of the deployment configuration. When the service
starts, it already knows about the accounts, verifies each one, and is ready —
with no manual step after deployment.

**Why this priority**: The service is a long-lived, cloud-hosted process. Being
able to hand it its accounts at deploy time (rather than only through live calls
afterwards) is essential for unattended operation and is equal in importance to
configuring an account interactively.

**Independent Test**: Start the system with two accounts defined purely in
deployment configuration and confirm both appear with a definitive status
(connected, or a clearly classified failure) without any interactive call being
made.

**Acceptance Scenarios**:

1. **Given** two accounts are defined in the deployment configuration, **When**
   the service starts, **Then** both accounts are loaded and each is verified
   against Eetmeter automatically.
2. **Given** the deployment configuration is missing a required field for an
   account, **When** the service starts, **Then** it reports a clear error
   identifying the offending account and the missing field, without printing any
   secret, and does not present that account as usable.
3. **Given** an account is defined both in deployment configuration and changed
   later through a live update, **When** the operator inspects the account,
   **Then** the live update is the value in effect and the system can report
   which source supplied it — until the next restart, after which the deployment
   configuration value is in effect again.

---

### User Story 3 - Fix an account after its password changes (Priority: P2)

An operator changed the password on one of the Eetmeter accounts directly in
Eetmeter. eetmeter-sync now fails to authenticate that account. The operator
updates the stored secret, and the system recovers on its own.

**Why this priority**: Credentials will drift over the life of the service.
Recovering without a restart or a support request is important, but it builds on
the P1 ability to configure and verify.

**Independent Test**: Put an account into "credentials rejected" state, update its
secret to the correct value, and confirm the account returns to "connected" via
the automatic re-verification that the secret update triggers, with no restart.

**Acceptance Scenarios**:

1. **Given** an account has status "credentials rejected", **When** the operator
   updates that account's secret, **Then** the system re-verifies the account
   automatically and, on success, clears the failure and records a new
   last-authenticated time.
2. **Given** an account is "connected", **When** its session is later refused by
   Eetmeter, **Then** the system attempts to re-authenticate using the stored
   credentials before reporting a failure to the operator.

---

### User Story 4 - Inspect and manage the configured accounts (Priority: P2)

An operator wants to see every account eetmeter-sync is configured with, each
one's connection health, and be able to remove an account that is no longer
wanted.

**Why this priority**: Visibility and cleanup are needed for day-to-day
operation, but the system delivers value with just P1 in place.

**Independent Test**: With several accounts configured, retrieve the account list
and confirm each entry shows a status, a last-successful-authentication time, and
a last-checked time, with no secret material; then remove one account and confirm
it disappears from the list and its stored secret is unrecoverable.

**Acceptance Scenarios**:

1. **Given** multiple accounts are configured, **When** the operator lists them,
   **Then** each account shows its label, status, last successful authentication
   time, last check time, and last failure category (if any) — and no secret.
2. **Given** an account is configured, **When** the operator removes it, **Then**
   the account and its stored secret are dropped from the running process and it
   no longer appears anywhere in the system's account views. (If the account also
   exists in the start-up configuration, it returns on the next restart — a
   durable removal is a start-up configuration change.)
3. **Given** any set of configured accounts, **When** the operator requests an
   on-demand re-check of one account or of all accounts, **Then** the system
   re-verifies the requested account(s) and updates their status and timestamps.

---

### Edge Cases

- **Eetmeter unreachable or slow during verification**: the account is recorded
  as "temporarily unavailable" (a transient failure), not "credentials
  rejected", and stays in that state until the operator re-checks it or updates
  its secret; the system does not retry automatically.
- **Same Eetmeter account configured twice** (same login identifier under two
  labels): the system rejects the second account with a clear, non-sensitive
  error rather than maintaining two copies; the first account is unaffected.
- **Encryption key absent or changed at startup**: the system cannot read stored
  secrets; it reports a clear error and refuses to start, and it does not delete
  or overwrite the stored data.
- **Malformed configuration value** (e.g. blank secret, missing device
  identifier): rejected at load time or on the update call with a clear,
  non-sensitive message; existing good accounts are unaffected.
- **Credentials are valid but the account is empty or restricted**: still counts
  as "connected" — authentication succeeded.
- **Rapid repeated verification requests for one account**: the system paces the
  attempts rather than issuing them all to Eetmeter.
- **Session token expires naturally during a long deployment**: the next use
  transparently re-authenticates from stored credentials without operator
  action.
- **Removing an account that is mid-verification**: the removal takes effect and
  no status is written back for the deleted account.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Operators MUST be able to register an Eetmeter account by supplying
  the information required to authenticate with Eetmeter: an account login
  identifier, a secret, and a device identifier. If no device identifier is
  supplied, the system MUST generate a stable one for that account and reuse it
  on every subsequent authentication.
- **FR-002**: The system MUST accept account configuration from a start-up
  configuration source (deployment environment or configuration file) so that a
  freshly deployed instance has its accounts without any manual follow-up step.
- **FR-003**: The system MUST allow operators to add, update, and remove account
  configurations while the system is running, without requiring a restart. These
  runtime changes take effect immediately for the running process but are
  session-only: they are not persisted independently of the start-up
  configuration, and a restart rebuilds the account set from the start-up
  configuration sources (see FR-004 and FR-016).
- **FR-004**: Within a running session, a runtime change to an account MUST take
  precedence over any value from the start-up configuration sources, and the
  system MUST be able to report which source supplied the value currently in
  effect. Across a restart, the start-up configuration sources are authoritative
  and any prior runtime change is discarded; precedence among the start-up
  sources is deployment environment over configuration file over built-in
  default.
- **FR-005**: Every account secret MUST be protected by authenticated encryption
  whenever the system holds it outside the start-up configuration source. In this
  feature the secret is never written to persistent storage (see FR-016 and the
  Assumptions); it exists only as ciphertext in the running process and is
  decrypted solely for an Eetmeter authentication call. The encryption key MUST
  come from outside any stored data (deployment environment or a key service) and
  MUST NOT be written to persistent storage, a configuration file, or the
  repository. Any future feature that persists a runtime-supplied secret MUST
  store only that ciphertext, under the same key rule.
- **FR-006**: The system MUST NOT reveal a stored secret in any interface
  response, log entry, error message, metric, or diagnostic output. Where a
  secret must be referred to, only a non-reversible indicator is exposed (for
  example: secret set / not set, and when it was last updated).
- **FR-007**: For each configured account, the system MUST verify the supplied
  credentials in two steps: (1) authenticate with Eetmeter to obtain a session,
  then (2) use that session to perform one lightweight authenticated read that
  confirms the session works and the account is reachable (the recorded fixtures
  define the exact endpoint — see plan.md and research.md Decision 2). Both steps
  MUST succeed for the account to be "connected". The system MUST record the
  result (success, or failure with a cause category) together with the time the
  check was made.
- **FR-008**: The system MUST verify an account automatically when it is first
  configured and again whenever that account's credentials change.
- **FR-009**: Operators MUST be able to trigger re-verification on demand, for a
  single named account or for all configured accounts.
- **FR-010**: The system MUST expose, for each account, its current connection
  status (at least: never verified, connected, credentials rejected, temporarily
  unavailable), the time of its last successful authentication, and the time of
  its last verification attempt.
- **FR-011**: On a failed verification, the system MUST classify the cause at
  least as "credentials rejected" versus "service unavailable / transient", so
  the operator knows whether to correct credentials or to wait.
- **FR-012**: The system MUST NOT automatically retry a failed verification: an
  account that fails verification stays in its recorded failure state until the
  operator updates its secret or requests an on-demand re-check. The system MUST
  pace authentication attempts for a given account so that repeated
  operator-triggered re-checks are not all issued to Eetmeter at once.
- **FR-013**: When a previously accepted session is no longer accepted by
  Eetmeter, the system MUST treat the account as needing re-authentication and
  MUST attempt to re-authenticate from the stored credentials before reporting a
  failure.
- **FR-014**: The system MUST keep each operator's account configuration and
  status isolated from any other operator's, even while only one operator
  exists.
- **FR-015**: The system MUST validate supplied configuration when it is loaded
  or changed, and MUST reject an invalid account with a clear, non-sensitive
  error (identifying the account and the problem) without emitting the secret and
  without disturbing already-valid accounts.
- **FR-016**: The system MUST persist each account's latest verification status
  (status, cause category, and timestamps) in its state store so it is restored
  on restart rather than reset to "never verified". Account configuration itself
  (labels, login identifiers, secrets, device identifiers) is NOT persisted: it
  is rebuilt from the start-up configuration sources on every start, which are
  authoritative. The out-of-band encryption key MUST be supplied again at every
  start so the service can encrypt the secrets it reads from the start-up source;
  if the key is absent or invalid the service refuses to start.
- **FR-017**: Each account MUST have a label that is unique within its operator's
  set, so accounts can be referred to unambiguously. The system MUST reject an
  add or rename that would reuse a label already present in that operator's set,
  with a clear, non-sensitive error, leaving the existing account untouched and
  creating no second account. The system MUST likewise reject an add or update
  that would give a second account an Eetmeter login identifier already used by
  another of that operator's accounts: the login identifier is unique within an
  operator's set.
- **FR-018**: The system MUST behave as a considerate client of Eetmeter during
  all of the above: only the requests needed to authenticate and retrieve the
  account are made, and repeated failures MUST NOT translate into sustained
  request volume against Eetmeter.

### Key Entities

- **Operator**: the party that owns and configures a set of Eetmeter accounts.
  Exactly one operator exists in the initial product, but each account and its
  status belong to a specific operator so that additional operators can be added
  later without restructuring.
- **Eetmeter Account**: a configured connection to a single Mijn Eetmeter
  account. Attributes: label (unique per operator), account login identifier
  (also unique per operator), secret (held only as in-memory ciphertext, never
  persisted, never returned), device identifier, the configuration source
  currently in effect, connection status, time of last successful
  authentication, time of last verification attempt, and last failure category
  (if any).
- **Verification Result**: the outcome of one attempt to authenticate an account
  with Eetmeter — status reached, cause category on failure, and timestamp. The
  most recent result is always available per account; earlier results MAY be
  retained as history.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An operator can take a fresh deployment from "no accounts" to "all
  supplied accounts connected" in under 5 minutes, without editing or rebuilding
  the software.
- **SC-002**: Under normal Eetmeter availability, 100% of configured accounts
  reach a definitive status (connected or a classified failure) within 30
  seconds of being configured or of service start-up.
- **SC-003**: Across every interface response, log, metric, and error message, a
  stored account secret appears zero times — verifiable by inspection and by
  automated scanning.
- **SC-004**: When an account's credentials are wrong, an operator can identify
  which account failed and that the cause is rejected credentials (not a
  transient error) with no outside help, in 100% of cases.
- **SC-005**: After an operator corrects the secret of a rejected account, the
  automatic re-verification triggered by that update returns the account to
  "connected" with no service restart.
- **SC-006**: A service restart re-establishes every account present in the
  start-up configuration, with its verification status restored from the state
  store and no manual re-entry of credentials, in 100% of cases, provided the
  out-of-band encryption key is supplied again. Accounts or edits introduced only
  through a live call since the last start are not expected to survive a restart.
- **SC-007**: The system never retries a failed verification on its own. When an
  operator triggers repeated re-checks of one account, the attempts reaching
  Eetmeter are paced to no more than 6 in any 10-minute window, rather than a
  continuous stream.
- **SC-008**: An operator can list all configured accounts and, for each, see its
  status and last-successful-authentication time in a single view.

## Assumptions

- The operator is already authenticated to the eetmeter-sync service through the
  service's standard access control; this feature does not define how an operator
  signs in.
- Authenticating with Eetmeter requires an account login identifier, a secret,
  and a device identifier. Where the operator does not provide a device
  identifier, the system generates and reuses a stable one so that Eetmeter sees
  a consistent device per account.
- "Fetching the account that is supplied" means authenticating to Eetmeter and
  then performing one lightweight authenticated read-back to confirm the session
  works and the account is reachable; the exact endpoint is fixed by the recorded
  fixtures (see plan.md and research.md Decision 2). Retrieving recipes or any
  other account content is a separate, later feature.
- The initial product serves a single operator whose two Eetmeter accounts will
  later be mirrored. Grouping accounts into a sync relationship, and anything
  about syncing, is out of scope for this feature.
- Persistent storage is available to the service for verification status only.
  Account configuration (including secrets) is not persisted in this feature — it
  is re-read from the start-up configuration source on every boot.
- Under normal conditions a single account verification completes within a few
  seconds.
- Both a declarative start-up configuration path and a live management path are
  provided. The declarative start-up configuration is the durable source of
  truth; live management changes apply to the running process only and do not
  survive a restart.

## Out of Scope

- Fetching, comparing, or syncing recipes (or any other Eetmeter content).
- Grouping or pairing accounts for synchronization, and any sync scheduling.
- Operator sign-up, operator identity management, or multi-operator onboarding.
- Any graphical or end-user interface beyond the service's programmatic
  interface.
- Proactive notifications or alerting when an account's status changes.
