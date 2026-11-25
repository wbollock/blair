# ADR-0012: Lean reminders, sync and offline capture

Status: Accepted · Date: 2026-11-24 · Amends ADR-0008, ADR-0009

## Context
The owner wants features they use every day, not machinery. The first
designs for reminders (§8), live sync (§10.7) and the offline queue (§12.3)
were bigger than single-user daily use needs.

## Decision
- **One reminder per task.** `tasks.remind_at` replaces a reminders table and
  relative presets. Changing it clears `reminded_at`, which re-arms it. A 30 s
  ticker fires due reminders in one transaction (exactly once). Completed and
  trashed tasks never fire.
- **No overdue notifications.** The Today view already shows overdue tasks.
- **Security notifications** reuse the audit log: a sign-in from a new
  browser (by User-Agent), a failed 2FA check after a correct password,
  password and 2FA changes, new recovery codes and new API tokens.
- **No SSE (amends ADR-0009).** The app polls `/notifications` every 30 s while
  visible, and reloads the current view when it returns after 15 s away.
- **Offline outbox in localStorage (amends ADR-0008).** Only task creates
  queue. Edits to queued tasks merge into the queued patch. Replay is
  idempotent on the client id. The outbox is tied to its owner and survives
  an expired session, and sign-out warns before dropping unsynced tasks.
  Offline launch uses the last good session copy (profile, projects,
  labels) so quick-add tokens still resolve. There is no read cache of tasks.
- **Service worker** caches the shell and build output, network-first for
  pages, and updates by replacing the cache on activation (no update toast).

## Consequences
- Far less code and no long-lived connections through the proxy.
- Changes from another device show up on focus, not instantly.
- A second reminder per task needs a new task or a later edit.
