# ADR-0011: Simplifications made while building the MVP

Status: Accepted · Date: 2026-11-24 · Amends ADR-0003, ADR-0004, ADR-0005

## Context
The owner asked for a working, secure deployment quickly. Some tooling choices
from the first ADRs cost more than they returned at this size.

## Decision
- **No server code generation (amends ADR-0004).** Handlers are hand-written
  chi handlers. `api/openapi.yaml` stays the contract. Every HTTP test validates
  requests and responses against it with kin-openapi, and a test requires each
  operation to be documented. The TypeScript client is still generated
  (`openapi-typescript` + `openapi-fetch`).
- **No sqlc (amends ADR-0003).** SQL lives in the service layer and uses
  `database/sql`. Each query filters on `owner_id`, and an isolation test
  covers each resource.
- **No Tailwind (amends ADR-0005).** Svelte scoped CSS plus the token file
  `web/src/lib/styles/tokens.css`. Bits UI provides menus.
- **SvelteKit 3 subpath imports.** Imports use `#lib/...` with explicit `.ts`
  extensions. (`$lib` is deprecated.)
- **Deferred from v1:** the offline outbox and service worker (the app is
  installable but needs a network connection), reminders and in-app
  notifications, SSE live sync, multi-select bulk actions, and continuous
  profiling push (pprof on a localhost listener only). They stay in PLAN.md.
- **Recovery codes are SHA-256 hashed.** They carry about 60 bits of
  entropy, so argon2id would only slow down login without improving security.

## Consequences
- Contract drift is caught by tests rather than by the compiler. The checks
  are strict: unknown request fields are rejected, and every response is
  validated.
- Fewer tools in the build chain. Adding codegen back later remains possible.
