# ADR-0008: Client-generated UUIDv7 IDs and an offline capture outbox

Status: Accepted · Date: 2026-11-24

## Context
Quick capture on a phone must work with bad signal. Full offline-first sync
(CRDTs, conflict resolution) is too costly for v1.

## Decision
- All IDs are UUIDv7 (time-ordered, good index locality). Clients may supply
  the `id` on create; the server treats a repeat create with the same id and
  owner as idempotent (200 if identical, 409 if different).
- The PWA queues task/note creates in an IndexedDB outbox when offline and
  replays them in order on reconnect/focus/start (Background Sync where
  available; Firefox and Safari lack it).
- Optimistic concurrency on updates via `version` / `ETag` + `If-Match`.
- Offline edits of server-side items are out of scope for v1.

## Consequences
- Simple, robust capture with no lost tasks; the path to fuller sync later
  is open (`updated_since` list filter + versions already exist).
