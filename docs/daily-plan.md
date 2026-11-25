# Daily plan and standups

Blair keeps an optional plan for each day: the tasks you picked to work on.
Turn on the UI in **Settings → Features → Daily plan**. The API works either way.

## In the app
- **Plan** page: today's picks, *carry over* yesterday's unfinished ones,
  *keeps slipping* (planned on several days, never finished), and every open
  task to pick from, filterable by project.
- **Copy for standup** copies `*Tasks*` and one bullet per task, linked to
  the first URL in its notes. Slack and docs get links; plain text gets markdown.
- Press `p` on a task, or use the right-click menu, to add or remove it.
- Today shows a *Planned* section at the top.

## From a script
Create a token with `tasks:read tasks:write` in Settings → API tokens.

```sh
# Record today's picks (ids from GET /tasks); tasks already planned are left alone.
curl -fsS -X POST "$BLAIR_URL/api/v1/plans/today/tasks" \
  -H "Authorization: Bearer $BLAIR_TOKEN" -H 'Content-Type: application/json' \
  -d '{"task_ids": ["0193...", "0193..."]}'

# Tasks planned on 3+ past days in the last 30 without being finished.
curl -fsS "$BLAIR_URL/api/v1/plans/slipping?min_days=3&days=30" -H "Authorization: Bearer $BLAIR_TOKEN"
```

`PUT /plans/{date}` replaces a day's plan, `GET /plans/{date}` reads it, and
`DELETE /plans/{date}/tasks/{id}` removes one task. Dates are `YYYY-MM-DD`
or `today` (your account's timezone). Full reference: `/api/docs` → Plans.
