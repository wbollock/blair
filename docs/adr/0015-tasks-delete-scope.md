# ADR-0015: Deleting needs its own scope, `tasks:delete`

Status: Accepted · Date: 2026-11-28

> The request for this change called it "ADR-0010". That number is already
> taken by the OpenTelemetry ADR, and ADRs are not renumbered, so this is the
> next free one.

## Context
`tasks:write` covered creating, editing and deleting. A token that only needs
to file and update tasks (an agent, a script, the MCP connector in ADR-0016)
could therefore also delete projects, columns, tasks, checklist items and
labels. A leaked or misbehaving write token should not be able to destroy data.

## Decision
- New PAT scope `tasks:delete`. The routes that delete task data now need it
  instead of `tasks:write`: `DELETE /projects/{id}`, `/columns/{id}`,
  `/tasks/{id}`, `/checklist/{id}`, `/labels/{id}` and
  `/plans/{date}/tasks/{id}`. `PUT /plans/{date}` replaces a whole plan and can drop
  entries, so it needs `tasks:write` and `tasks:delete`.
- `POST /batch` accepts `delete` ops. Those ops (except for notes, which keep
  `notes:write`) also need `tasks:delete`, or the new scope would be a no-op.
- Migration `00005` adds `tasks:delete` to every existing token that has
  `tasks:write`, so nothing that worked yesterday breaks. New tokens get only
  what they ask for; the token form lists the new scope unticked by default.
- The OAuth grants for MCP clients (ADR-0016) can only be `tasks:read` and
  `tasks:write`. `tasks:delete` is never grantable over OAuth.

## Consequences
- Existing tokens keep their power until the owner rotates them. Narrowing them
  is a manual step: create a new token without `tasks:delete`.
- Notes are still deleted with `notes:write` (they go to the trash and can be
  restored for 30 days). A `notes:delete` scope is a possible follow-up.
- A test walks every `DELETE` path in the OpenAPI contract and checks that a
  token without `tasks:delete` gets 403 on all of them.
