# ADR-0009: In-app notifications over SSE, no push

Status: Accepted · Date: 2026-11-24

## Context
Owner wants reminders and notifications inside the app only, not Web Push
or email. Multiple devices/tabs should stay in sync.

## Decision
- A scheduler goroutine fires reminders/overdue notices into a
  `notifications` table transactionally (exactly once).
- A per-user SSE hub streams notifications and entity change events at
  `/api/v1/events`, with `Last-Event-ID` replay; polling fallback.
- No service-worker push, no VAPID keys, no OS notifications in v1.

## Consequences
- Reminders are only seen when the app is open; acceptable per owner.
- SSE requires `WriteTimeout: 0` on the server for that route and no proxy
  buffering (Caddy handles SSE correctly by default).
