# Observability for the MCP connector

Covers what ADR-0016 adds. Rule for all of it: **no tokens, codes, passwords, 2FA codes, emails,
task titles or descriptions** in any log field, metric attribute or span. A test
(`TestMCPAndOAuthLogsCarryNoSecretsOrContent`) fails if one appears.

## Metrics (OTLP, and `/metrics` when Prometheus is on)

| Metric | Type | Attributes |
| --- | --- | --- |
| `blair.oauth.events` | counter | `event`: `client_registered`, `registration_denied`, `registration_opened`, `authorize`, `authorize_denied`, `login_failure`, `mfa_failure`, `owner_mismatch`, `token_issued`, `token_refreshed`, `refresh_reuse`, `code_reuse`, `revoke` |
| `blair.mcp.requests` | counter | `outcome`: `ok`, `unauthorized`, `insufficient_scope`, `origin_denied`, `rate_limited`, `address_denied` |
| `blair.mcp.tool.calls` | counter | `tool` (one of the 12 names), `outcome`: `ok`, `error`, `rate_limited` |
| `blair.mcp.tool.duration` | histogram, ms | `tool` |

Useful alerts: any `refresh_reuse` or `code_reuse` (a token was replayed, the grant is already
revoked); a burst of `login_failure`, `mfa_failure` or `unauthorized`; any `owner_mismatch`;
`registration_denied` when you did not just try to add a connector.

## Logs (JSON on stdout, trace-correlated)

| Message | Level | Fields |
| --- | --- | --- |
| `http request` | info | method, route, status, duration (existing). No query strings or bodies |
| `oauth event` | info | `event`, `client_id`, `grant_id`, `user_id` |
| `oauth security event` | **warn** | the same plus `ip`. For login, 2FA and registration failures, owner mismatch, code reuse and refresh-token reuse |
| `mcp tool` | info | `tool`, `write`, `ok`, `duration_ms`, `user_id`, `grant_id` |
| `mcp request refused` | **warn** | `outcome`, `ip` |
| `connector address gate refused a request` | **warn** | `ip`, `route` |

## Audit log (per user, Settings → Security, kept 1 year)

`oauth.registration_opened`, `oauth.client_registered`, `oauth.authorize`, `oauth.authorize_denied`,
`oauth.login_failure`, `oauth.mfa_failure`, `oauth.owner_mismatch`, `oauth.token_issued`,
`oauth.token_refreshed`, `oauth.refresh_reuse`, `oauth.code_reuse`, `oauth.revoke`, and `mcp.write`
(tool, grant, task id). An app registering under your account, an app being connected, and a refresh-token reuse each also
create an in-app notification. Reads through `/mcp` are counted and logged but not stored per call.
