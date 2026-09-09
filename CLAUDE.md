# CLAUDE.md

Guidance for AI coding agents working in this repository.

## What this is

`eetmeter-sync` mirrors recipes between two Mijn Eetmeter accounts so a household
doesn't enter each recipe twice. It runs as a long-lived, public-facing service
with a daily automatic sync and an on-demand HTTP/JSON trigger. Go, cloud-ready,
container-delivered.

## The constitution is authoritative

[`.specify/memory/constitution.md`](.specify/memory/constitution.md) governs how
this code is written. Read it before making design decisions. If a task conflicts
with it, stop and surface the conflict — do not quietly deviate. Amending the
constitution is itself a PR (see its Governance section); versioning and the Sync
Impact Report must be updated in the same change.

## Workflow

Spec-first, using [Spec Kit](https://github.com/github/spec-kit):

```
/speckit-specify → /speckit-plan → /speckit-tasks → /speckit-implement
```

The plan's **Constitution Check** gate is evaluated against the constitution
before Phase 0 and re-checked after design. `/speckit-tasks` must emit
failing-test tasks for every domain package and Eetmeter contract, regardless of
the task template's optional-tests default.

## Hard rules (from the constitution)

- **Test-first, non-negotiable.** For anything under `internal/` or `pkg/`, and
  for every Eetmeter API contract, write a failing test before the
  implementation. Eetmeter interactions are tested against recorded JSON
  fixtures.
- **Never commit sensitive data.** Before every commit, scan the working set for
  credentials, tokens, encryption keys, `.env` contents, and real personal data
  (fixtures and logs included). A hit blocks the commit. Fixtures must be
  scrubbed. A secret that reaches history is compromised — rotate it.
- **Sync safety.** Union mirror. Second run with no changes = zero writes. A
  recipe changed on both sides is flagged, never merged. Deletions never
  propagate automatically and never in the unattended run. Every sync can produce
  a plan without applying it.
- **Recipe identity is the name**, with content-hash rename detection, computed
  as pure functions over a persisted per-account last-sync snapshot.
- **Credentials** are encrypted at rest with an out-of-band key; never in logs,
  API responses, metrics, or fixtures.
- **Good API citizen.** Per-account rate limiting, bounded backoff + jitter,
  capped concurrency, only the headers the API needs, re-auth on 401/403.
- **Interface.** The HTTP/JSON API is the only way in. Scheduler, future UI,
  future CLI are all clients — no privileged bypass path.
- **Tooling.** `gofmt` and `go vet` clean; `go test ./... -race` passes; `main`
  always builds and is green.
- **Cloud-ready.** Multi-stage `Dockerfile` (distroless, non-root). Stateless
  process, config from env, no baked secrets, graceful `SIGTERM`. Image build is
  part of CI.

## Commits & PRs

- **Conventional Commits 1.0.0**: `<type>[scope]: <description>`. Types:
  `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `ci`, `build`, `perf`.
  Breaking: `type!:` or a `BREAKING CHANGE:` footer.
- Each commit is one coherent change that builds and passes tests on its own.
- Commit message trailer:
  ```
  Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
  ```
- PR description footer:
  ```
  🤖 Generated with [Claude Code](https://claude.com/claude-code)
  ```
- An agent may create branches, commits, PRs, and administer the GitHub
  repository on the maintainer's account (constitution → Governance → Delegated
  agent authority). This does not lift any rule above.

## Eetmeter API references (read-only; not patterns to copy)

- [`SimonVreman/vreetmeter`](https://github.com/SimonVreman/vreetmeter) — SwiftUI
  client for the same private API (`Vreetmeter/Api/`). Base URL
  `https://api3-mijn.voedingscentrum.nl/api/`. Auth: `POST /account/credentials`
  → token sent as `authorization: Basic <token>`, with `version` / `platform`
  headers. "Recipes" are the API's *combined products* (`combinedproduct`).
- `../eetmeter-share` — an earlier unfinished Go attempt (local to the
  maintainer's workspace). Look at `internal/voedingscentrum/` only for how the
  API is called; its architecture carries no authority.

When a reference disagrees with reality, the recorded fixtures win and the
discrepancy is noted in the spec or plan.
