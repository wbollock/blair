# ADR-0004: Contract-first API with OpenAPI and code generation

Status: Accepted · Date: 2026-11-24

## Context
API-first is a core requirement: migration scripts and agents, CLI, and future native
apps use the same API, and the docs must be trustworthy.

## Decision
- `api/openapi.yaml` (OpenAPI 3.0.3) is the contract and is edited **before**
  handlers. 3.0.3 rather than 3.1 for the best tool support in oapi-codegen.
- Go: `oapi-codegen` strict-server interfaces + models; handlers implement them.
- TypeScript: `openapi-typescript` types + `openapi-fetch` client for the web UI.
- `mise run gen` regenerates both; CI fails on uncommitted diffs.
- Tests validate every request/response against the spec with `kin-openapi`.
- Spec linted with `vacuum`; every operation needs summary, description,
  examples, error responses, and `x-scopes`.
- Docs served by the app at `/api/docs` (Scalar, assets embedded, no CDN).

## Consequences
- Docs can't drift from behavior; frontend type errors surface API changes.
- Slight friction: every API change touches YAML first. That friction is the point.
