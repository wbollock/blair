# ADR-0014: An optional daily plan, kept as history

Status: Accepted · Date: 2026-11-24

## Context
The owner runs a standup script each morning: pick today's tasks with fzf,
paste them into Slack, and log them to a notes file. They want this inside
Blair, including seeing tasks that get picked day after day but never
finish, without making it mandatory or tying Blair to one workflow.

## Decision
- **A plan is its own thing, not a due date.** `plan_items (owner, date,
  task, position)` records what was picked for a day. A due date says when a
  task must happen; a plan says what you chose to do. This is the "plan your
  day" model from Todoist, Sunsama and tududi.
- **History is the point.** Rows stay after the day passes. `GET
  /plans/slipping` counts past days a still-open task was planned without
  being completed that day. Completions of repeating tasks
  (`task_completions`) count as done, so a daily chore never slips.
- **API first, UI optional.** `GET/PUT /plans/{date}`, `POST
  /plans/{date}/tasks`, `DELETE /plans/{date}/tasks/{id}`; `today` resolves
  in the account's timezone. The UI (Plan page, Today section, `p`
  shortcut, menu item) appears only when Settings → Daily plan is on. The
  API always works, so scripts don't depend on the toggle.
- **Standup output stays generic.** The Plan page copies `*Tasks*` plus a
  bullet per task, linked to the first URL in its notes, as plain text and
  HTML. Personal formats (meetings, notes files) stay in scripts.

## Consequences
- Any client can record a plan with one call and read slipping tasks back.
- Plans and completions are in the JSON export.
- The analysis views on the TODO list can build on the same history.
