# ADR-0002: Go single-binary backend with chi

Status: Accepted · Date: 2026-11-24

## Context
Self-hosted on a small home box alongside other containers. Owner prefers Go.
Wants low ops burden: one thing to run, back up, and upgrade.

## Decision
- Backend in Go (version pinned via `mise.toml` / `go.mod` toolchain).
- `net/http` + `go-chi/chi` router; stdlib-first otherwise.
- The SvelteKit build is embedded with `go:embed`, so the whole app is one
  static binary (CGo-free) and one distroless container.
- The same binary provides the CLI (`serve`, `migrate`, `user`, `backup`, ...).
- Layering: handlers → service → store. Business rules live only in `service`.

## Consequences
- Trivial deploys and rollbacks (swap image tag; DB migrations are forward-only
  and run on start, with an automatic pre-migration backup).
- Frontend and backend are released together; no version skew between UI and API
  for the first-party client. Third-party clients rely on `/api/v1` stability.
