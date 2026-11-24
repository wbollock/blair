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
   and `x-scopes`), then run `mise run gen`. Handlers are hand-written
   (ADR-0011); the HTTP tests validate them against the contract.
3. Write failing tests: a Go service/handler test against a real temp SQLite
   DB, Vitest for frontend logic, Playwright for user journeys.
4. Implement until the tests pass, then run `mise run check`.
5. Commit in small Conventional Commits (`feat(tasks): …`, `fix(web): …`,
   `docs(spec): …`, `test(batch): …`). The hook in `.githooks/` enforces the
   format. Run `mise run hooks` once per clone.
6. Tick the box in `docs/PLAN.md` when a checklist item is fully done.

## Commands

Toolchain is pinned in `mise.toml`. Run everything through `mise`. The system
Go on this box is too old (1.19), so do not use it.

```sh
mise install        # install pinned Go + Node
mise run hooks      # enable the commit-msg hook
```

```sh
mise run dev        # API on :8080 (data in ./data)
cd web && npm run dev   # UI with hot reload; proxies /api to :8080
mise run gen        # regenerate web/src/lib/api/schema.d.ts after editing api/openapi.yaml
mise run test       # go test -race ./... and vitest
mise run lint       # go vet, gofmt, svelte-check
mise run check      # lint + generated types up to date + tests (run before committing)
mise run build      # web build embedded into bin/blair
```

End-to-end tests (`web/e2e`) need a fresh server and run in the official
Playwright image, because Chromium can't run on this Debian 11 host:

```sh
docker run --rm --network host -e HOME=/tmp -e SETUP_URL=<setup link from server log> \
  --user $(id -u):$(id -g) -v /tmp:/tmp -v $PWD:/repo -w /repo/web \
  mcr.microsoft.com/playwright:v1.65.1-noble npx playwright test
```

Set `SCREENSHOT_DIR=/repo/docs/screenshots` to refresh the README screenshots.

Deployed at https://blair.wboll.dev from `~/docker/blair` (image `blair:latest`,
built from this repo; Caddy site in `~/docker/caddy/sites.caddy`).

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
- **Web imports** use `#lib/...` with explicit `.ts` extensions (SvelteKit 3).
  Never name a CSS class `label`, `input`, `btn` or `chip` inside a component
  unless you mean the global style from `app.css`.
- **Contract tests**: handler responses are validated against the OpenAPI spec.
  Never weaken the validator to make a test pass.
- **Batch upserts preserve history** (`created_at`, `completed_at`) and are
  idempotent on `(external_source, external_id)`. There are no built-in
  importers: migrations go through the public API.
- **Telemetry carries no content or PII**: no titles, bodies, emails or
  tokens in span attributes or log fields. New spans and metrics are listed
  in `docs/OBSERVABILITY.md`.
- Secrets are never logged. Tokens and codes are stored hashed. The TOTP
  secret is stored encrypted.

## Layout

```
cmd/blair/            main + CLI subcommands
internal/config/      env config
internal/httpapi/     chi handlers, middleware, SPA + docs serving, contract tests
internal/auth/        argon2id, TOTP, tokens, secret box
internal/quickadd/    quick-add parser (mirrors web/src/lib/quickadd.ts)
internal/fracindex/   ordering keys
internal/service/     business rules (tasks, projects, auth, quickadd, batch)
internal/store/       SQLite open/migrate/backup; migrations/*.sql (goose)
internal/telemetry/   OpenTelemetry setup (traces, metrics, logs, profiles)
internal/web/         go:embed of the SvelteKit build
api/openapi.yaml      the API contract
web/                  SvelteKit app (src/lib/api/schema.d.ts = generated types; e2e/ = Playwright)
testdata/             shared fixtures and test vectors
docs/                 SPEC, PLAN, ADRs
```
