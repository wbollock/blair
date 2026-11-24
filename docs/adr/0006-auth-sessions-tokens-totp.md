# ADR-0006: Server-side sessions, scoped API tokens, TOTP 2FA

Status: Accepted · Date: 2026-11-24

## Context
Internet-exposed, self-hosted. Needs user creation, 2FA, browser login, and
token access for scripts. JWTs were considered and rejected: they can't be
revoked without server state anyway, and leak easily into logs/storage.

## Decision
- Browser: opaque session tokens in a `__Host-` cookie (HttpOnly, Secure,
  SameSite=Lax), SHA-256 hashed in the DB, sliding + absolute expiry, revocable.
- CSRF: Go's `http.CrossOriginProtection` plus a required custom header on
  cookie-authenticated unsafe requests.
- Scripts/clients: personal access tokens with scopes, hashed, prefixed
  (`blair_pat_`) for secret scanning; never valid for admin endpoints.
- Passwords: argon2id. 2FA: TOTP with encrypted secret + hashed single-use
  recovery codes; replay-protected. Passkeys deferred to Phase 2.
- Registration closed by default; invites or CLI to create users.

## Consequences
- Every request does one indexed session/token lookup; fine at this scale.
- Native apps later use PATs or an OAuth-style device flow (future ADR).
