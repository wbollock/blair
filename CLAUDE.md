# CLAUDE.md

Guidance for AI coding agents (Claude Code and others) working in this repo.
`AGENTS.md` is a symlink to this file.

## What this is

Blair is a self-hosted task and notes app (TickTick/Todoist/Vikunja style). It
is web-first and API-first: a Go single binary with SQLite serves a documented
JSON API, plus an embedded SvelteKit PWA. Its core promise: **a task you just
added is never out of reach.** You can see it, click it, and file it
immediately (SPEC §5).

## Read before working

1. `docs/SPEC.md`: the source of truth for behavior. Find the section that
   covers your change.
2. `docs/PLAN.md`: the current phase and its checkboxes. Work on the current
   phase unless told otherwise.
3. `docs/adr/`: why things are the way they are. Do not reverse an ADR
   silently. Propose a superseding ADR instead.

## Workflow (spec → contract → test → code)

1. If behavior changes, update `docs/SPEC.md` first.
2. If the API changes, edit `api/openapi.yaml` (with examples, error responses
   and `x-scopes`), then run `mise run gen`.
3. Write failing tests: a Go service/handler test against a real temp SQLite
   DB, Vitest for frontend logic, Playwright for user journeys.
4. Implement until the tests pass, then run `mise run check`.
5. Commit in small Conventional Commits (`feat(tasks): …`, `fix(web): …`,
   `docs(spec): …`, `test(import): …`). The hook in `.githooks/` enforces the
   format. Run `mise run hooks` once per clone.
6. Tick the box in `docs/PLAN.md` when a checklist item is fully done.

## Commands

Toolchain is pinned in `mise.toml`. Run everything through `mise`. The system
Go on this box is too old (1.19), so do not use it.

```sh
mise install        # install pinned Go + Node
mise run hooks      # enable the commit-msg hook
```

Tasks added in Phase 1: `mise run dev | gen | lint | test | e2e | build | check`.
Update this section when they exist.

## Invariants agents must not break

- **Ownership**: every user-owned query filters on `owner_id`. The isolation
  test harness must cover every new resource.
- **Column ↔ status rules** (SPEC §3.3 / ADR-0007) live only in the `service`
  layer. Handlers and the frontend never reimplement them.
- **Quick-add grammar**: `testdata/quickadd/cases.json` is the contract for both
  the Go and TS parsers. Add a case before changing behavior. Never fork the
  grammar between implementations.
- **Never lose a capture**: any create path must surface the new task in the
  just-added tray, including offline. Do not "simplify" this away.
- **No server-rendered HTML from user content.** Markdown is rendered
  client-side and sanitized with DOMPurify. Keep the CSP strict, with no inline
  scripts and no CDNs.
- **CGo-free** (`modernc.org/sqlite`). The SQLite DSN uses `_pragma=name(value)`.
- **Contract tests**: handler responses are validated against the OpenAPI spec.
  Never weaken the validator to make a test pass.
- **Imports preserve history** (`created_at`, `completed_at`) and are
  idempotent on `(source, source_id)`.
- Secrets are never logged. Tokens and codes are stored hashed. The TOTP
  secret is stored encrypted.

## Layout (target; created in Phase 1)

```
cmd/blair/            main + CLI subcommands
internal/config/      env config
internal/httpapi/     handlers (oapi-codegen strict server), middleware
internal/service/     business rules (tasks, projects, auth, quickadd, import)
internal/store/       sqlc queries + generated code, migrations
internal/web/         go:embed of the SvelteKit build
api/openapi.yaml      the API contract
web/                  SvelteKit app (src/lib/api = generated client)
testdata/             shared fixtures and test vectors
docs/                 SPEC, PLAN, ADRs
```
