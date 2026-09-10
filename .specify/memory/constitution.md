<!--
Sync Impact Report
- Version change: (template / unversioned) → 1.0.0
- Ratification: initial adoption 2026-09-10
- Principles defined:
    1. Simplicity & YAGNI (KISS)
    2. Don't Assume
    3. Pragmatic Testing
    4. Idiomatic Go
- Added sections: Context (preamble)
- Removed sections: none carried over (previous file was the unfilled template);
  optional "Additional Constraints" and "Development Workflow" sections were
  intentionally omitted — this constitution governs principles only.
- Templates reviewed:
    ✅ .specify/templates/plan-template.md — "Constitution Check" gate is generic,
       derives from this file; no edit needed.
    ✅ .specify/templates/spec-template.md — no constitution-specific content; no edit needed.
    ✅ .specify/templates/tasks-template.md — tests already marked OPTIONAL, consistent
       with Principle 3; no edit needed.
    ✅ .claude/skills/speckit-*/SKILL.md — all reference `.specify/memory/constitution.md`
       generically; no agent-specific or outdated references found.
- Deferred TODOs: none
-->

# eetmeter-sync Constitution

## Context

`eetmeter-sync` is a private, single-user project whose source is published openly.
It is a REST API written in Go, possibly gaining a small frontend later. "Open
source" means the code is public and readable — not that it is a supported product
with external users, stability guarantees, or a support channel.

The purpose of this constitution is to keep the project small. Every rule below
exists to resist scope creep, premature generalization, and ceremony that a
one-person project does not need.

## Core Principles

### I. Simplicity & YAGNI (KISS)

Build only what a current, stated requirement needs. When two approaches both
satisfy the requirement, choose the one with less code, fewer moving parts, and
fewer concepts to hold in your head.

- No abstraction, interface, layer, configuration knob, or extensibility hook is
  added before a concrete present need forces it. "We might need it later" is not
  a need.
- The Go standard library is the default. Every third-party dependency MUST be
  justified in the commit message that introduces it, naming what it replaces and
  why the stdlib is insufficient.
- Dead code, unused parameters, and commented-out blocks are deleted, not kept
  "just in case." Version control remembers.

**Rationale**: The project has one user and one maintainer. Complexity has no
payoff here and a permanent maintenance cost.

### II. Don't Assume

When a requirement, an external API's behavior, or a data shape is unclear, stop
and confirm before building on the guess.

- Any assumption that is made anyway MUST be recorded where it lives — in the
  feature spec, or as a comment at the point in the code that depends on it.
- External API responses are inspected against real data before code is written
  to parse them, not reverse-engineered from documentation alone.

**Rationale**: Wrong assumptions discovered late are the most expensive kind of
rework, and a solo project has no reviewer to catch them.

### III. Pragmatic Testing

Automated tests exist to catch regressions cheaply. They are written where that
payoff is real, and skipped where it is not.

- Non-trivial logic (parsing, transformation, sync/reconciliation rules, anything
  with branches or edge cases) MUST have automated tests.
- Every bug fix MUST add a test that fails before the fix and passes after.
- Trivial glue code — wiring, straight-through handlers, struct mapping with no
  logic — does not require tests.
- There is no test-first mandate and no coverage target. Test after, test during,
  whatever gets working software fastest without leaving core logic unguarded.

**Rationale**: Strict TDD and coverage gates are ceremony this project cannot
afford; untested parsing and sync logic is a risk it cannot afford either.

### IV. Idiomatic Go

Code follows standard Go conventions so that a reader — including future-you and
anyone who finds the public repo — can navigate it without surprises.

- Code MUST pass `gofmt` and `go vet` before it is committed.
- Follow standard Go project layout and naming idioms. Do not invent a bespoke
  structure.
- Favor clear, obvious code over clever code. Exported identifiers get a short doc
  comment stating what they do.

**Rationale**: Idiomatic code is cheaper to return to after months away, and the
repo is public.

## Governance

- This constitution supersedes ad-hoc practice. When a decision conflicts with a
  principle here, the principle wins or the constitution is amended first.
- Amendments are made in a documented commit that edits this file and bumps the
  version below according to semantic versioning:
  - MAJOR — a principle is removed or redefined in a backward-incompatible way.
  - MINOR — a new principle or section is added, or guidance is materially expanded.
  - PATCH — clarifications and wording fixes with no change in meaning.
- There is no approval process; the maintainer is the sole authority. The
  requirement is that changes be deliberate and written down, not that they be
  reviewed by someone else.
- Complexity that violates a principle (an added dependency, a new layer, a
  bespoke structure) is either justified in the commit that introduces it or it
  is not merged.

**Version**: 1.0.0 | **Ratified**: 2026-09-10 | **Last Amended**: 2026-09-10
