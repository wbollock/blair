# Blair

A self-hosted task and notes app that never loses the task you just added.

![Today view with quick-add, the just-added tray and quick actions](docs/screenshots/quick-actions.png)

- **Capture fast, file fast.** Type `Fix gutter #homelab /doing !high fri 9am @house`
  and the project, column, priority, due date and label are set as you type.
  The new task stays pinned in a **Just added** tray with one-tap chips for
  project, column, due date, priority and labels. If the view you're in would
  hide it, the card tells you where it went.
- **Right-click anything.** Quick actions on every task: status, column, move
  to project, due date, priority, labels, duplicate, delete. On phones,
  long-press or tap `⋯`.
- **Kanban per project.** Custom columns, each mapped to a status, so dragging
  to *Done* completes the task.
- **Inbox triage**, notes, search, Logbook, trash with undo, keyboard shortcuts (`?`).
- **Secure accounts.** Closed registration with invites, argon2id passwords, TOTP 2FA
  with recovery codes, revocable sessions, scoped API tokens, and an audit log.
- **API-first.** An OpenAPI contract with interactive docs at `/api/docs`.
  Batch upsert and full export let an agent migrate your data from any other app.
- **OpenTelemetry** traces, metrics and logs over OTLP.
- **Installable PWA.** Paper design, light/dark/system themes, works on phones.
- **One Go binary** (CGo-free) with SQLite and nightly backups.

| Board | Task details | Phone |
|---|---|---|
| ![Board](docs/screenshots/board.png) | ![Drawer](docs/screenshots/drawer.png) | ![Mobile capture](docs/screenshots/mobile-capture.png) |

The screenshots come from the end-to-end test run (`web/e2e`) against a real server.

## Run it

```sh
docker build -t blair:latest .
docker run -d --name blair -p 127.0.0.1:8080:8080 -v blair-data:/data \
  -e BLAIR_BASE_URL=https://tasks.example.com blair:latest
docker logs blair 2>&1 | grep setup   # one-time admin setup link (valid 1 hour)
```

Put it behind a TLS reverse proxy (Caddy: `reverse_proxy 127.0.0.1:8080`).
Configuration is in [docs/SPEC.md §14](docs/SPEC.md). Common settings:
`BLAIR_REGISTRATION=closed|invite|open`, `BLAIR_REQUIRE_2FA`,
`BLAIR_TRUSTED_PROXIES`, `OTEL_EXPORTER_OTLP_ENDPOINT`.

Admin CLI (same binary): `blair user create|list|passwd|reset-2fa`, `blair backup`, `blair migrate`.

## Develop

```sh
mise install && mise run hooks
mise run dev                 # API on :8080
cd web && npm run dev        # UI with hot reload, proxies /api to :8080
mise run check               # lint, generated types up to date, Go + web tests
```

Docs: [SPEC](docs/SPEC.md) · [PLAN](docs/PLAN.md) · [ADRs](docs/adr/) · agent guide [CLAUDE.md](CLAUDE.md).
