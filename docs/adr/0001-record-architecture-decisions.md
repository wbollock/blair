# ADR-0001: Record architecture decisions

Status: Accepted · Date: 2026-11-24

## Context
Blair is built largely with AI coding agents across many sessions. Agents (and
future me) lose context between sessions and will happily "improve" a past
decision without knowing why it was made.

## Decision
Record each significant, hard-to-reverse decision as a short ADR in
`docs/adr/NNNN-title.md` (Context / Decision / Consequences). ADRs are
immutable once accepted; changing a decision means a new ADR that supersedes
the old one. `docs/SPEC.md` describes *what*; ADRs describe *why*.

## Consequences
- Agents must read relevant ADRs before changing architecture (see CLAUDE.md).
- Small cost per decision; large savings in re-litigation.
