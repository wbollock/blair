# ADR-0013: Repeating tasks roll forward in place

Status: Accepted · Date: 2026-11-24

## Context
Chores, bills and standups repeat. Todoist moves a repeating task to its next
date; Things creates a fresh copy each time. The owner also wants future
analysis of what gets done and what keeps slipping.

## Decision
- **Rule format:** a subset of RFC 5545 RRULE stored on the task
  (`recurrence`): `FREQ` (DAILY, WEEKLY, MONTHLY, YEARLY), `INTERVAL`,
  `BYDAY` (weekly) and `BYMONTHDAY` (monthly, clamped to short months). No
  COUNT, UNTIL or BYSETPOS. Importers can map most tools' rules onto it.
- **Quick-add:** `every …` phrases only. Bare `daily` or `weekly` would eat
  titles like "Weekly report". The Go and TS implementations share
  `testdata/recur/next.json` and the quick-add vectors.
- **Completion rolls forward in place.** Marking the task done moves it to
  the next occurrence after its due date or today, whichever is later. That
  way a long-overdue daily task lands on tomorrow, not on a date still in the
  past. The status goes back to todo (and the project's To do column), the
  checklist resets, and a reminder shifts by the same number of days.
- **History:** each completion is a row in `task_completions` (task, due date,
  completed time), for the Logbook and the analysis views on the TODO list.
- **Cancelled ends the series.** Removing the rule makes done final.

## Consequences
- One task id for the whole series: links, notes and checklists stay put.
- The Logbook doesn't show repeat completions yet; `task_completions` has them.
- Undo in the UI restores the old due date but keeps the completion row.
