# ADR-0007: Per-project kanban columns mapped to statuses

Status: Accepted · Date: 2026-11-24

## Context
Owner likes per-project boards (Vikunja) but wants custom columns that stay
aligned with task status (tududi). Global-status-only boards are too rigid;
free-form columns make "is this done?" ambiguous for Today/Logbook/export/migrations.

## Decision
- Fixed status enum: `todo`, `doing`, `waiting`, `done`, `cancelled`.
- Each project has its own ordered columns; each column maps to one status.
- Moving to a column sets status; setting status moves to the first matching
  column (rules in SPEC §3.3). Every project keeps ≥1 `todo` and ≥1 `done` column.
- Inbox tasks have status but no column.

## Consequences
- Completion semantics are global and unambiguous, while boards stay flexible.
- Rare mismatched state (status with no matching column) is allowed, visible
  via a badge, and covered by tests.
