# Implementation Plan

Build order for [SPEC.md](SPEC.md). Each phase ends with a **deployable,
tested app**: a walking skeleton first, then vertical slices. Tick the boxes as
work lands. One checkbox is roughly one to three Conventional Commits.

Rules for every phase:

- Write or extend the OpenAPI spec and tests **before** the implementation.
- Keep `mise run check` green on every commit (lint + gen-verify + test).
- When the phase is done: update SPEC.md for anything that changed, add
  ADRs for new decisions, and demo the exit criteria in a real browser at
  375 px and 1440 px widths.

---

## Phase 1 — Walking skeleton

- [ ] Go module `github.com/wbollock/blair`, `cmd/blair` with `serve`/`version`, config from env (§14.1), slog
- [ ] SQLite open (WAL test), goose migrations embedded, `migrate` subcommand, pre-migration backup
- [ ] `api/openapi.yaml` skeleton (info, servers, security schemes, problem+json, pagination components, `/healthz`)
- [ ] oapi-codegen + sqlc wired via `mise run gen`; CI verifies no diff
- [ ] `/healthz`, `/readyz`, `/metrics`; `/api/docs` (Scalar embedded) + `/api/openapi.{yaml,json}`
- [ ] SvelteKit app in `web/` (Svelte 5, TS strict, Tailwind v4 tokens, light/dark), built into `internal/web/dist`, served with SPA fallback and security headers/CSP
- [ ] Generated TS client; Vitest + Playwright set up with one smoke test
- [ ] `mise` tasks: `dev`, `gen`, `lint`, `test`, `e2e`, `build`, `check`
- [ ] Dockerfile (distroless nonroot), `.github/workflows/ci.yml`, golangci-lint config, gitleaks

**Exit:** `docker run` serves an empty app shell and the API docs page. CI is green.

## Phase 2 — Accounts and security (§9)

- [ ] Users table, argon2id hasher (param upgrade), breached-password list
- [ ] Setup token flow + `blair user create|passwd|list|reset-2fa`
- [ ] Sessions (`__Host-` cookie, hashing, sliding/absolute expiry, rotation), login/logout
- [ ] CrossOriginProtection + custom header middleware, rate limiter, trusted proxies
- [ ] TOTP enroll/confirm/disable, recovery codes, MFA login step, replay protection
- [ ] API tokens with scopes; scope enforcement middleware driven by `x-scopes` in the spec
- [ ] Invites + registration modes; admin user management
- [ ] Audit log + Settings → Security UI (sessions, tokens, 2FA, audit)
- [ ] Tests: per-user isolation harness (reusable for every later resource), auth e2e with TOTP

**Exit:** create the admin, enable 2FA, log in with TOTP and a recovery code, create a PAT and curl `/me`.

## Phase 3 — Core tasks (§3, §4, §6)

- [ ] Projects + columns with the status invariants (exhaustive service tests for §3.3)
- [ ] Tasks CRUD, soft delete/trash/restore, version/ETag/If-Match, client-id idempotency
- [ ] Labels, checklist items, fractional indexing (shared TS/Go vectors)
- [ ] FTS5 search
- [ ] UI shell: sidebar / bottom tab bar, Inbox, Today, Upcoming, Project list view, Label, Logbook, Trash, Search
- [ ] Task detail drawer/route: title (inline md), description (GFM editor + preview), all fields
- [ ] markdown-it + DOMPurify renderer with XSS test corpus
- [ ] Undo toasts; keyboard shortcuts framework + `?` cheat sheet

**Exit:** manage tasks end-to-end on desktop and phone.

## Phase 4 — Capture and triage (§5): the core feature

- [ ] Go quick-add parser passing `testdata/quickadd/cases.json` + fuzz test; `quick_add` on `POST /tasks`; `POST /quick-add/parse`
- [ ] TS parser passing the same vectors + fuzz (fast-check); in-input token chips with autocomplete
- [ ] Context-aware defaults per view
- [ ] Just-added tray with chip pickers, optimistic updates, and the "not in this view" notice
- [ ] Mobile capture sheet (Task | Note toggle, sticky chips, stays open)
- [ ] Triage mode with suggestions, keyboard and swipe
- [ ] Recently added view
- [ ] e2e for all §5.5 acceptance criteria (desktop + mobile projects)

**Exit:** §5.5 passes. Capture 10 tasks on a phone and file them all without hunting.

## Phase 5 — Kanban (§7)

- [ ] Board view, column CRUD UI (name + status, WIP limit, reorder, delete-with-move)
- [ ] Drag and drop desktop + touch, keyboard DnD, "Move to…" menu
- [ ] Mobile snap columns + pills; Done column trimming
- [ ] Optimistic moves with rollback; e2e for drag → status change

**Exit:** run a real project on the board from a phone.

## Phase 6 — Notes, reminders, notifications (§3.4, §8)

- [ ] Notes CRUD, list/cards, pin, convert-to-task
- [ ] Reminders (absolute + relative), scheduler with exactly-once firing tests (fake clock)
- [ ] Overdue + security + import notifications
- [ ] SSE hub (`/events`, tickets, Last-Event-ID replay, heartbeat) + polling fallback
- [ ] Bell, panel, toasts; live cross-tab sync of task changes

**Exit:** a reminder set on a phone pops up as a toast on the desktop tab.

## Phase 7 — PWA and offline (§12)

- [ ] Manifest, icons (maskable + monochrome), apple meta, shortcuts, share target
- [ ] Service worker precache + update toast
- [ ] IndexedDB outbox with replay triggers and error states; read cache; logout wipe
- [ ] "Offline — view only" mode
- [ ] Lighthouse CI budgets (§12.4)
- [ ] Manual install checklist run on iOS Safari, macOS Safari, Android Firefox, Android Chrome (`docs/INSTALL.md`)

**Exit:** airplane-mode capture syncs when back online, on iPhone and on Android Firefox.

## Phase 8 — Import and export (§11)

- [ ] `blair.export.v1` JSON Schema, export JSON/CSV, import `blair` with a round-trip test
- [ ] Import job framework: dry run, idempotent `(source, source_id)`, report, transactions, SSE progress
- [ ] Vikunja importer (fixture from a real export, completion dates preserved)
- [ ] Obsidian Tasks, generic CSV with mapping UI
- [ ] Todoist, TickTick, tududi importers
- [ ] `POST /tasks/batch`; "Migrating from X" docs with a stdlib Python script
- [ ] Settings → Import/Export UI with preview

**Exit:** import the live Vikunja data (open + completed, with dates) into Blair.

## Phase 9 — Hardening and deploy

- [ ] Security review against OWASP ASVS L2 checklist (`docs/SECURITY-REVIEW.md`); fix findings
- [ ] `/security-review` + govulncheck + npm audit clean
- [ ] Built-in backup schedule verified with a restore drill
- [ ] Accessibility pass (axe zero, keyboard-only walkthrough, screen reader spot check)
- [ ] `~/docker/blair` compose + Caddy `blair.wboll.dev`; metrics token
- [ ] Retire tududi (and later Vikunja) once Blair has been in daily use for 2 weeks

**Exit:** Blair is the daily driver.

## Later (Phase 10+)

Recurring tasks (RRULE) · passkeys · shared projects · read-only ICS feed ·
saved filters · start/scheduled dates · areas/groups · native SwiftUI client ·
OAuth device flow for native apps · attachments.
