# ADR-0016: A remote MCP connector with its own OAuth server

Status: Accepted · Date: 2026-11-28

## Context
claude.ai custom connectors speak the remote MCP protocol and sign in with
OAuth. They cannot send a personal access token. The owner wants Claude to
read and update their tasks from claude.ai and the phone apps, without giving
it a way to destroy data or to reach the rest of the API.

## Decision
- **Blair is the authorization server** (`internal/oauth`). No third-party
  identity provider: the user signs in with their Blair password and 2FA on
  `/oauth/authorize`, then approves a consent screen. OAuth 2.1 profile:
  authorization code only, PKCE `S256` only, public clients only, exact
  redirect URI match, RFC 8414 / 9728 metadata, RFC 7591 registration, RFC 7009
  revocation, RFC 9207 `iss` in the redirect, RFC 8707 `resource`.
- **Registration is open but narrow.** Anyone can register a client, but only
  with the two Claude callback URLs. A malicious client therefore can never
  receive a code. Registrations are rate-limited, capped at 200, and pruned
  after a day if never used. The client name is untrusted text and is only
  shown escaped, labelled as unverified.
- **Short-lived, rotating, bound tokens.** Codes 60 s. Access tokens 1 h.
  Refresh tokens 30 d, rotated on every use. Reuse of a rotated refresh token,
  or of a code, revokes the whole grant. Tokens are opaque, stored as SHA-256
  hashes, and tagged to the `/mcp` resource. They carry only `tasks:read` and
  `tasks:write`, and the REST API refuses them outright (403), so they can
  never reach a DELETE route even if scopes were misconfigured. This builds on
  `tasks:delete` (ADR-0015).
- **The MCP server has no delete tool** (`internal/mcpsrv`, official Go SDK,
  stateless Streamable HTTP, JSON responses). It shares the service layer with
  the REST API, so ownership checks, column and status rules, and versioning
  stay in one place. `update_task` requires the task's `version`; without
  `If-Match` semantics an agent could silently overwrite a newer edit.
  `plan_today` only adds. Unknown projects and labels are errors, never
  created implicitly.
- **Off by default.** `BLAIR_MCP_ENABLED=false` mounts nothing (404). The
  Connected apps page and revocation still work when it is off.
- **A separate Origin check on `/mcp`** accepts no browser origin except
  claude.ai and claude.com (server-side callers send none).
- **Rate limits and audit.** Per grant: 120 requests and 30 writes a minute.
  Authentication failures are limited per address. Every write records the
  tool, grant and task id in the audit log, never titles or text. Reads are
  logged by tool name only.

## Consequences
- Anything a task contains is read by the model. A title such as "ignore
  previous instructions" is data, and the server instructions say so, but a
  model can still be fooled. The worst a fooled model can do is create, edit,
  complete, move or plan tasks, all of which can be undone. It cannot delete,
  read notes, export, or change the account.
- Anyone who knows the owner's password and 2FA can connect an app. The
  consent screen, the "connected" notification and the Connected apps page are
  the safety net.
- Refresh-token reuse revokes the connection even when it was a client retry
  after a lost response. The user reconnects. The alternative is a grace
  window, which weakens theft detection.
- A rotating refresh chain keeps used tokens until they expire (30 days), at
  most one row per rotation.
- Dependencies: `github.com/modelcontextprotocol/go-sdk` and its small
  transitive set (`jsonschema-go`, `segmentio/encoding`, `uritemplate`,
  `golang.org/x/oauth2`).
- Local testing with MCP Inspector needs `BLAIR_OAUTH_EXTRA_REDIRECT_URIS`
  for its callback, because production refuses any other redirect URI.
