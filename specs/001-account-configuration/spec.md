# Feature Specification: Account Configuration

**Feature Branch**: `001-account-configuration`

**Created**: 2026-09-09

**Status**: Draft

**Input**: User description: "eetmeter-sync: let's first make the spec about the base; Fetch the accounts that are supplied, make sure there is a way to configure these accounts"

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
   which source supplied it.

---

### User Story 3 - Fix an account after its password changes (Priority: P2)

An operator changed the password on one of the Eetmeter accounts directly in
Eetmeter. eetmeter-sync now fails to authenticate that account. The operator
updates the stored secret, and the system recovers on its own.

**Why this priority**: Credentials will drift over the life of the service.
Recovering without a restart or a support request is important, but it builds on
the P1 ability to configure and verify.

**Independent Test**: Put an account into "credentials rejected" state, update its
secret to the correct value, and confirm the account returns to "connected"
within one verification cycle with no restart.

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
   the account's configuration and stored secret are deleted and it no longer
   appears anywhere in the system's account views.
3. **Given** any set of configured accounts, **When** the operator requests an
   on-demand re-check of one account or of all accounts, **Then** the system
   re-verifies the requested account(s) and updates their status and timestamps.

---

### Edge Cases

- **Eetmeter unreachable or slow during verification**: the account is recorded
  as "temporarily unavailable" (a transient failure), not "credentials
  rejected", and is retried later with increasing back-off.
- **Same Eetmeter account configured twice** (same login identifier under two
  labels): the system flags the duplicate rather than silently maintaining two
  copies.
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
  configurations while the system is running, without requiring a restart.
- **FR-004**: When the same account is supplied by more than one configuration
  source, the system MUST resolve the conflict by a defined precedence —
  runtime change over deployment environment over configuration file over
  built-in default — and MUST be able to report which source supplied the value
  currently in effect.
- **FR-005**: The system MUST store every account secret encrypted at rest. The
  encryption key MUST come from outside the stored data (deployment environment
  or a key service) and MUST NOT be written alongside the encrypted data or into
  the system's own persistent storage.
- **FR-006**: The system MUST NOT reveal a stored secret in any interface
  response, log entry, error message, metric, or diagnostic output. Where a
  secret must be referred to, only a non-reversible indicator is exposed (for
  example: secret set / not set, and when it was last updated).
- **FR-007**: For each configured account, the system MUST verify the supplied
  credentials by authenticating with Eetmeter and retrieving the account, and
  MUST record the result (success, or failure with a cause category) together
  with the time the check was made.
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
- **FR-012**: The system MUST limit how often it attempts authentication for a
  given account and MUST apply increasing back-off after repeated failures,
  rather than retrying immediately or continuously.
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
- **FR-016**: The system MUST persist account configuration and the latest
  verification status across restarts, so that credentials do not need to be
  re-entered (given the out-of-band encryption key is provided again).
- **FR-017**: Each account MUST have a label that is unique within its operator's
  set, so accounts can be referred to unambiguously; the system MUST reject or
  flag a second account that reuses a label, and MUST flag when two labels point
  at the same Eetmeter login identifier.
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
  account. Attributes: label (unique per operator), account login identifier,
  secret (stored encrypted, never returned), device identifier, the
  configuration source currently in effect, connection status, time of last
  successful authentication, time of last verification attempt, and last failure
  category (if any).
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
  account returns to "connected" within one verification cycle and with no
  service restart.
- **SC-006**: Account configuration and status survive a service restart with no
  re-entry of credentials in 100% of cases, provided the out-of-band encryption
  key is supplied again.
- **SC-007**: When one account fails authentication repeatedly, the system makes
  no more than a small, bounded, backed-off number of attempts against Eetmeter
  in the following 10 minutes (target: 6 or fewer), rather than a continuous
  stream.
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
  confirming the account is reachable; retrieving recipes or any other account
  content is a separate, later feature.
- The initial product serves a single operator whose two Eetmeter accounts will
  later be mirrored. Grouping accounts into a sync relationship, and anything
  about syncing, is out of scope for this feature.
- Persistent storage is available to the service for account configuration and
  status.
- Under normal conditions a single account verification completes within a few
  seconds.
- Both a declarative start-up configuration path and a live management path are
  provided, consistent with the project's configuration-precedence rule.

## Out of Scope

- Fetching, comparing, or syncing recipes (or any other Eetmeter content).
- Grouping or pairing accounts for synchronization, and any sync scheduling.
- Operator sign-up, operator identity management, or multi-operator onboarding.
- Any graphical or end-user interface beyond the service's programmatic
  interface.
- Proactive notifications or alerting when an account's status changes.
