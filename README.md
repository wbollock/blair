# Blair

A self-hosted task and notes app that never loses the task you just added.

- **Capture fast, file fast.** Quick-add syntax
  (`Fix gutter #homelab /doing !high fri at 9am`), a "just added" tray of
  tappable cards, and Inbox triage mode.
- **Kanban per project**, with custom columns mapped to statuses. Priority,
  due dates, labels, checklists, and markdown.
- **Notes**, in-app reminders, and notifications.
- **Secure accounts** with TOTP 2FA, sessions, and scoped API tokens.
- **API-first**: an OpenAPI contract with interactive docs at `/api/docs`.
  Import from Vikunja, tududi, Todoist, TickTick, Obsidian Tasks, and CSV.
- **PWA** for iOS, macOS Safari, Android Firefox and Chrome, with offline
  capture.
- **One Go binary** with SQLite and built-in backups.

Status: specification phase. See [docs/SPEC.md](docs/SPEC.md),
[docs/PLAN.md](docs/PLAN.md), and [docs/adr/](docs/adr/).

## Development

```sh
mise install && mise run hooks
```

Contributor and agent workflow: [CLAUDE.md](CLAUDE.md).
