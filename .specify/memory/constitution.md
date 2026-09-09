<!--
Sync Impact Report
==================
Version change: (unversioned template) → 1.0.0
Bump rationale: Initial ratification. MAJOR to 1.0.0 establishes the first
governed baseline for the project. (Drafted in one sitting; the Eetmeter access
model was corrected from "HTML scraping" to "private JSON API" before ratification
after reviewing the reference implementations listed below — no version bump since
nothing was committed at the earlier wording.)

Modified principles: none (initial definition).
Added sections:
  - Core Principles I–VI:
    I.   Library-First Core, API-Only Interface
    II.  Test-First for Core Logic and External Contracts (NON-NEGOTIABLE)
    III. Non-Destructive, Idempotent Sync
    IV.  Deterministic Recipe Identity
    V.   Credential and Secret Protection
    VI.  Good Citizen to the Eetmeter API
  - Security & Deployment Constraints (includes a mandatory pre-commit
    sensitive-data check and cloud-ready OCI container delivery with image
    building in CI)
  - Development Workflow & Quality Gates (includes Conventional Commits 1.0.0 as
    the commit-message standard)
  - Reference Material
  - Governance (includes a Delegated agent authority clause: an AI agent may
    create branches/commits/PRs and administer the GitHub repository)
Removed sections: none.

Templates & docs reviewed for consistency:
  - .specify/templates/plan-template.md ✅ "Constitution Check" gate is generic and
    derives from this file; no edit required.
  - .specify/templates/spec-template.md ✅ no constitution-driven mandatory
    sections to add or remove.
  - .specify/templates/tasks-template.md ⚠ PENDING: template marks tests OPTIONAL
    by default. Principle II makes tests mandatory (and test-first) for domain
    packages and the Eetmeter contract. The /speckit-tasks generator MUST emit
    failing-test tasks for those areas regardless of the template default. Update
    the template's "Tests" note when convenient.
  - .specify/templates/checklist-template.md ✅ no changes required.
  - .claude/skills/speckit-*/SKILL.md ✅ no outdated agent-specific references.
  - README / quickstart ✅ none present yet.

Deferred TODOs: none. RATIFICATION_DATE set to the first adoption date
(2026-09-09).
-->

# eetmeter-sync Constitution

## Purpose

eetmeter-sync keeps recipe collections mirrored between two Voedingscentrum
"Mijn Eetmeter" accounts so that a household does not have to enter every recipe
twice. It runs as a long-lived service with a daily automatic sync and an
on-demand trigger. It talks to Eetmeter through the private JSON API that backs
the official Mijn Eetmeter mobile app (`https://api3-mijn.voedingscentrum.nl`);
that API is undocumented, unversioned in practice, and offered under no
stability guarantee. The data it touches is personal and effectively
irreplaceable, the accounts are not the operator's own, and the service is
reachable from the public internet. Every principle below exists to keep that
combination safe.

## Core Principles

### I. Library-First Core, API-Only Interface

All sync logic — fetching, snapshotting, diffing, identity resolution,
reconciliation planning, and applying changes — MUST live in importable Go
packages under `internal/` or `pkg/` that run and are testable without any
network server. The HTTP/JSON API is the single external interface contract. The
internal scheduler, any future web UI, and any future CLI MUST be clients of that
same API surface (or of the same underlying packages) and MUST NOT have a
privileged path that bypasses its checks, auth, or logging. A package MUST have a
single stated purpose in its doc comment; grouping-only packages are prohibited.

Rationale: A public service that can rewrite other people's data must have one
audited way in. Keeping the engine independent of the transport makes it
testable, keeps the daily job and a manual trigger on identical code, and lets a
partner-facing UI be built later with no engine changes.

### II. Test-First for Core Logic and External Contracts (NON-NEGOTIABLE)

For every package under `internal/` or `pkg/`, and for every external contract
(the shape of Eetmeter API requests and responses), tests MUST be written first
and MUST fail before implementation. Red-Green-Refactor is enforced in review: a
change that adds or alters core logic without a preceding failing test is
rejected. Eetmeter interactions MUST be tested against recorded JSON fixtures so
that a change in the API breaks CI rather than silently breaking a sync. Response
decoding MUST tolerate unknown fields and MUST fail loudly on a missing or
type-mismatched field it depends on. Exploratory spikes are permitted only on
branches that are never merged.

Rationale: Sync defects corrupt a food diary silently and are noticed late, now
across more than one person's account. Tests written first pin intended behavior
before implementation bias sets in, and fixture tests turn an unofficial,
unstable dependency into something that fails visibly.

### III. Non-Destructive, Idempotent Sync

The sync model is a union mirror: a recipe present in either account MUST come to
exist in both; new recipes propagate in both directions. The following are
absolute:

- Running a sync twice with no intervening changes MUST produce zero writes on
  the second run.
- A recipe changed on **both** sides since the last successful sync MUST be
  flagged and left untouched. The tool MUST NOT merge or pick a winner in the
  MVP.
- Deletions MUST NOT propagate automatically. Removing a recipe from the other
  account requires an explicit, separate opt-in action and MUST NEVER happen in
  the unattended run.
- The unattended daily run MAY create and modify recipes; it MUST NOT delete.
- Every sync MUST be able to compute and expose its full intended change set
  (a plan) without applying it.
- A sync MUST be safe to interrupt and resume without duplicating work.

Rationale: The operator's worst outcomes are recipes that get mangled or
silently diverge. Refusing to guess, refusing to delete unattended, and always
being previewable make those outcomes structurally hard to reach.

### IV. Deterministic Recipe Identity

A recipe's identity across the two accounts is its **name**. Identity and change
detection MUST be pure functions over (a) the current recipe list in each account
and (b) a persisted last-sync snapshot per account (recipe name → content hash),
stored in the service's state store. A recipe whose content hash matches a
prior recipe under a different name MUST be treated as a rename and updated in
place, not duplicated. Absent a matching snapshot entry, a recipe is treated as
new, never as an edit. These rules MUST have exhaustive unit tests covering
create, edit, rename, delete-on-one-side, and both-sides-changed.

Rationale: "Recipes don't match up" is the headline risk. Making identity a
tested pure function over an explicit snapshot — rather than a live guess —
means the matching behavior is inspectable and stable across runs.

### V. Credential and Secret Protection

Eetmeter account credentials and session tokens MUST be encrypted at rest. The
encryption key MUST be supplied out of band (environment variable or a KMS) and
MUST NOT be stored alongside the ciphertext or committed to the repository.
Credentials and tokens MUST NEVER appear in logs, error messages, API responses,
metrics, or fixtures. Recorded fixtures MUST be scrubbed of real tokens and
personal data before they are committed. Even while the service is single-tenant,
stored data MUST be structured per user (no global singletons for account
configuration or snapshots) so that isolation is already real when a second
household is added.

Rationale: A public server holding several people's third-party logins is a
standing target. Encryption with an out-of-band key limits the blast radius of a
host compromise, and per-user structure keeps "open it up later" a configuration
change rather than a rewrite.

### VI. Good Citizen to the Eetmeter API

Eetmeter is a service the project does not own and integrates with through a
reverse-engineered private API. The client MUST: serialize or rate-limit requests
per account with a configurable minimum interval; retry only with bounded
exponential backoff plus jitter and a capped attempt count; bound aggregate
outbound concurrency across all accounts; and send only the app-compatibility
headers the API requires to function (e.g. `version`, `platform`) plus a
`User-Agent` that identifies this tool — it MUST NOT further disguise its traffic,
volume, or purpose. On `401`/`403` the client MUST discard the token and
re-authenticate rather than hammering the endpoint.

Rationale: All traffic reaches Eetmeter from one server on behalf of multiple
accounts; without deliberate restraint that pattern looks like abuse and gets the
users blocked. Sending the minimum headers needed to work, and no more, keeps the
integration honest about what it is.

## Security & Deployment Constraints

- **Transport**: TLS is required for every non-loopback connection. Plain HTTP is
  permitted only for a loopback bind during local development.
- **Authentication**: Every API endpoint except a health check MUST require
  authentication. There MUST be no "trusted because of network location" bypass.
  Authentication attempts MUST be rate-limited. No default or hard-coded
  credentials may ship.
- **Secrets**: All secrets (DB credentials, the credential-encryption key,
  session signing keys) come from the environment or a secrets manager, never
  from files in the working tree. `.gitignore` MUST exclude any local secret or
  state file.
- **No sensitive data in history**: Before every commit, the working set MUST be
  checked for credentials, tokens, encryption keys, `.env` contents, and real
  personal data (including in fixtures and logs). A hit blocks the commit. CI
  MUST run a secret scanner and fail on a finding. A secret that reaches history
  is treated as compromised: rotate it, do not merely revert.
- **Cloud-ready delivery**: The service ships as an OCI container image built
  from a multi-stage `Dockerfile` (minimal or distroless base, runs as a non-root
  user). Building the image is part of CI; releases publish it to a registry. The
  process is stateless — all state lives in the external store — reads its entire
  configuration from the environment, bakes no secrets or config into the image,
  and shuts down gracefully on `SIGTERM`.
- **Logging & observability**: Logs are structured (`log/slog`), default to
  warning level, and pass through a redaction step that strips known secret
  fields. The service MUST expose the outcome, timestamp, and change summary of
  the most recent sync per account, and the unattended run MUST surface failures
  loudly (non-zero process signal / visible error state), never swallow them.
- **State**: Persistent state (per-user snapshots, encrypted credentials, run
  history) lives in a defined store outside the repository. Its location and
  schema are documented.
- **Language & tooling**: Go at the version pinned in `go.mod` (currently 1.26).
  `gofmt` and `go vet` MUST pass. CI MUST run `go test ./...` with the race
  detector enabled.
- **Dependencies**: The standard library is the default. Each third-party module
  MUST be justified in the PR that adds it (capability gained, lighter
  alternative rejected and why). No dependency is added purely for stylistic
  convenience over the standard library.
- **Configuration precedence**: explicit request/flag > environment variable >
  config file > built-in default. Every configuration key is documented.

## Development Workflow & Quality Gates

- **Spec-driven flow**: Features move through `/speckit-specify` →
  `/speckit-plan` → `/speckit-tasks` → `/speckit-implement`. The plan's
  Constitution Check gate MUST be evaluated against this document before Phase 0
  and re-checked after design.
- **Task generation**: Because Principle II is non-negotiable, generated task
  lists MUST include failing-test tasks for every domain package and every
  Eetmeter contract touched, even where the task template marks tests optional.
- **Commits**: Commit messages MUST follow Conventional Commits 1.0.0
  (`<type>[optional scope]: <description>`, with `feat`/`fix`/`docs`/`chore`/
  `refactor`/`test`/`ci`/`build`/`perf` as the common types, `!` or a
  `BREAKING CHANGE:` footer for breaking changes). Each commit SHOULD be a single
  coherent change that builds and passes tests on its own.
- **Review**: Every change is reviewed against these principles. The reviewer
  MUST explicitly confirm: tests precede logic (II); the sync stays idempotent,
  non-merging, and non-deleting-unattended (III); identity logic remains pure and
  fully tested (IV); no secret can reach a log, response, fixture, or the repo
  (V); outbound Eetmeter traffic stays rate-limited and backed off (VI).
- **Complexity**: Any deviation from a principle MUST be recorded in the plan's
  Complexity Tracking table with the concrete need and the rejected simpler
  alternative. Unjustified complexity blocks merge.
- **Green main**: `main` MUST always build and pass the full test suite with the
  race detector. Work that cannot ship safely stays behind a flag or on a branch.

## Reference Material

The Eetmeter API has no official documentation. The following are the working
references for its request/response contract. They are read-only inputs — their
architecture and code are NOT patterns to copy.

- **`github.com/SimonVreman/vreetmeter`** (MIT) — a SwiftUI client that exercises
  the same API. Best available reference for endpoints, auth flow, required
  headers, and response shapes (see `Vreetmeter/Api/`). "Recipes" correspond to
  the API's *combined products* (`combinedproduct`).
- **`../eetmeter-share`** — an earlier, unfinished Go attempt in this workspace.
  Consult `internal/voedingscentrum/` only to see how the API is addressed from
  Go. Its structure, dependencies, and logic carry no authority here.

When implementation reveals that a reference is wrong or stale, the fixtures
recorded under Principle II — not these references — are the source of truth, and
the discrepancy MUST be noted in the relevant spec or plan.

## Governance

This constitution supersedes other process conventions in the repository. Where a
practice and this document conflict, this document governs until it is amended.

**Amendment procedure**: Propose the change in a pull request that edits this
file, states the rationale, and lists the impact on dependent templates and docs.
Merge requires approval from a project maintainer. On merge, the version and
dates below MUST be updated and the Sync Impact Report at the top of this file
MUST be refreshed.

**Delegated agent authority**: An AI coding agent working in this repository is
authorized to create branches, make commits, push, open pull requests, and
administer the project's repository on the maintainer's GitHub account (including
repository creation and visibility). This authority does not lift any principle
above: commits still follow Conventional Commits, changes still pass the quality
gates, and secrets still never enter the repository or its history.

**Constitution versioning**: Semantic versioning of this document.
MAJOR — a principle is removed or redefined in a backward-incompatible way, or
governance rules change materially. MINOR — a new principle or section is added,
or existing guidance is materially expanded. PATCH — clarifications, wording, and
typo fixes that do not change what is required.

**Compliance review**: Every pull request description MUST assert compliance with
this constitution or link to the Complexity Tracking justification for a
deviation. A maintainer re-reads this document in full at least once per MINOR
release of the tool to confirm the principles still match how the service
actually behaves — in particular Principles III–VI, whose failure modes are
silent.

**Version**: 1.0.0 | **Ratified**: 2026-09-09 | **Last Amended**: 2026-09-09
