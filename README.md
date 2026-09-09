# eetmeter-sync

Keep recipes mirrored between two [Mijn Eetmeter](https://mijn.voedingscentrum.nl/)
accounts, so a household doesn't have to enter every recipe twice.

> **Status:** early. The design is being built spec-first; there is no runnable
> service yet. The [project constitution](.specify/memory/constitution.md) is
> ratified and governs how the code is written.
>
> **This is an AI-generated project.** The specs, constitution, and code are
> produced with [Claude Code](https://claude.com/claude-code) under human review.

## What it does

Eetmeter (by the Dutch Voedingscentrum) lets you save recipes, but there is no
way to share them between accounts. `eetmeter-sync` runs as a small service that:

- performs a **daily automatic sync** and can be **triggered on demand** via an
  HTTP/JSON API;
- uses a **union mirror** model — a recipe in either account comes to exist in
  both, and new recipes propagate in both directions;
- **never merges or deletes on its own**: a recipe changed on both sides since
  the last sync is flagged for you to resolve, and deletions never propagate
  automatically.

Recipes are matched across accounts **by name**, with content-hash detection so a
rename updates in place instead of creating a duplicate.

## How it talks to Eetmeter

There is no official API. `eetmeter-sync` uses the private JSON API that backs
the official Mijn Eetmeter mobile app (`https://api3-mijn.voedingscentrum.nl`).
It is undocumented and offered under no stability guarantee, so the client is a
deliberate good citizen: per-account rate limiting, bounded backoff, and
contract tests pinned to recorded JSON fixtures that fail CI when the API drifts.

References for the API contract (read-only; not patterns to copy):

- [`SimonVreman/vreetmeter`](https://github.com/SimonVreman/vreetmeter) — a
  SwiftUI client exercising the same API.

## Development

Built with Go (see [`go.mod`](go.mod)) and the
[Spec Kit](https://github.com/github/spec-kit) workflow:

```
/speckit-specify → /speckit-plan → /speckit-tasks → /speckit-implement
```

Non-negotiables from the [constitution](.specify/memory/constitution.md):

- **Test-first** for all core logic and every Eetmeter API contract.
- **No secrets** in the repo, logs, API responses, or fixtures — checked before
  every commit. Credentials are encrypted at rest with an out-of-band key.
- **Conventional Commits** ([1.0.0](https://www.conventionalcommits.org/en/v1.0.0/))
  for every commit.
- `gofmt`, `go vet`, and `go test ./... -race` must pass.

## Deployment

Cloud-ready by design: the service ships as an OCI container image built from a
multi-stage `Dockerfile` (distroless, non-root). Image building is part of CI;
releases publish to a registry. The process is stateless, reads all
configuration from the environment, bakes in no secrets, and shuts down cleanly
on `SIGTERM`.

Working with an AI agent in this repo? See [`CLAUDE.md`](CLAUDE.md).

## License

MIT — see [`LICENSE`](LICENSE).
