-- +goose Up
-- OAuth 2.1 authorization server for the MCP connector (ADR-0016).
-- Codes, access tokens and refresh tokens are stored as SHA-256 hashes.

-- Public clients registered with RFC 7591 (Dynamic Client Registration).
CREATE TABLE oauth_clients (
    id             TEXT PRIMARY KEY,
    owner_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE, -- the account that opened registration; only it can approve this client
    name           TEXT NOT NULL,
    redirect_uris  TEXT NOT NULL,      -- space separated, exact match
    created_at     TEXT NOT NULL,
    approved_at    TEXT                -- set the first time a code is exchanged; an approved client may reconnect later
);

-- Registration is closed unless the owner opens a short window in Settings → Connected apps.
-- This stops anyone else from registering a client and tricking the owner into approving it.
CREATE TABLE oauth_windows (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TEXT NOT NULL,
    remaining   INTEGER NOT NULL
);

-- One in-progress /oauth/authorize flow: login, then 2FA, then consent.
CREATE TABLE oauth_requests (
    id              TEXT PRIMARY KEY,
    client_id       TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    redirect_uri    TEXT NOT NULL,
    scope           TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT '',
    code_challenge  TEXT NOT NULL,
    resource        TEXT NOT NULL,
    stage           TEXT NOT NULL,     -- login | mfa | consent
    user_id         TEXT REFERENCES users(id) ON DELETE CASCADE,
    attempts        INTEGER NOT NULL DEFAULT 0,
    binding_hash    BLOB NOT NULL,     -- hash of the cookie that ties the flow to one browser
    csrf_hash       BLOB NOT NULL,
    created_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL
);

-- A user's consent to one client: what "Connected apps" lists and revokes.
CREATE TABLE oauth_grants (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_id      TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    scope          TEXT NOT NULL,
    resource       TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    last_used_at   TEXT,
    revoked_at     TEXT,
    revoked_reason TEXT
);
CREATE INDEX oauth_grants_user ON oauth_grants(user_id);

-- Authorization codes: single use, 60 seconds.
CREATE TABLE oauth_codes (
    code_hash       BLOB PRIMARY KEY,
    client_id       TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    redirect_uri    TEXT NOT NULL,
    scope           TEXT NOT NULL,
    resource        TEXT NOT NULL,
    code_challenge  TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    used_at         TEXT,
    grant_id        TEXT REFERENCES oauth_grants(id) ON DELETE CASCADE  -- set once exchanged, so a replay can revoke it
);

-- Access (1 hour) and refresh (30 days, rotating) tokens.
CREATE TABLE oauth_tokens (
    token_hash  BLOB PRIMARY KEY,
    grant_id    TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
    expires_at  TEXT NOT NULL,
    used_at     TEXT,                  -- refresh tokens: set on rotation; reuse revokes the grant
    created_at  TEXT NOT NULL
);
CREATE INDEX oauth_tokens_grant ON oauth_tokens(grant_id);

-- +goose Down
DROP TABLE oauth_tokens;
DROP TABLE oauth_codes;
DROP TABLE oauth_grants;
DROP TABLE oauth_requests;
DROP TABLE oauth_clients;
DROP TABLE oauth_windows;
