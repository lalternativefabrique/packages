CREATE TABLE IF NOT EXISTS connect_grant_pending (
    state       TEXT PRIMARY KEY,
    verifier    TEXT NOT NULL,
    owner       TEXT NOT NULL,
    product     TEXT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS connect_grant_pending_expiry_idx ON connect_grant_pending (expires_at);

CREATE TABLE IF NOT EXISTS connect_grant (
    owner          TEXT NOT NULL,
    product        TEXT NOT NULL,
    status         TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
    refresh_token  BYTEA,
    scopes         TEXT[] NOT NULL DEFAULT '{}',
    resources      TEXT[] NOT NULL DEFAULT '{}',
    connected_at   TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (owner, product)
);
