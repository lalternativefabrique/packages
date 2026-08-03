CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    url           TEXT NOT NULL,
    event_types   TEXT[] NOT NULL DEFAULT '{}',
    description   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,                    -- 'active' | 'disabled' | 'deleted'
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL,
    last_event_id TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS webhook_endpoints_tenant_idx
    ON webhook_endpoints (tenant_id, created_at DESC);
