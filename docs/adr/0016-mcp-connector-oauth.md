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
- **Registration is closed unless the owner opens it.** Settings → Connected apps opens a
  10-minute window (3 uses, 2FA required). A client registered in it belongs to that account,
  and only that account can approve it. This stops a stranger registering an app and sending
  the owner its authorize link. Only the two Claude callback URLs are accepted, so a code can
  never reach another site. The name is untrusted text, shown escaped and labelled unverified.
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
- **2FA is required** to approve an app, because an app keeps working for 30 days. At most 10
  apps per account (the oldest is revoked). Changing or resetting the password, or turning 2FA
  off or resetting it, disconnects every app.
- **Optional address list.** `BLAIR_MCP_ALLOWED_CIDRS` limits `/mcp`, `/oauth/token`,
  `/oauth/register`, `/oauth/revoke` and the discovery documents to given addresses. The
  sign-in page stays open. There is no built-in list: Anthropic publishes its outbound ranges
  (`160.79.104.0/21` and `153.61.208.0/20` when last read, 2026-11-28; see its IP addresses page) and
  says only that they "will not change without notice". Leave this unset if you would rather not
  risk a silent break when they move; tokens still protect every endpoint. Refused requests log
  `connector address gate refused a request` with the address, which is how you would notice.
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
