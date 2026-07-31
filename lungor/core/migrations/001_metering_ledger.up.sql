-- Usage metering: an append-only ledger of consumption, and the catalogue of
-- units that can be consumed.
--
-- The ledger is the source of truth. Nothing stores a running total: a balance
-- or a period's consumption is a SUM over the rows, which is what makes
-- (app_id, idempotency_key) enough to make a debit safe to retry. A cached
-- counter would need its own reconciliation.
--
-- tenant_id / app_id / customer_id are plain UUIDs, not foreign keys. A
-- single-tenant application derives customer_id deterministically from its own
-- user id (see metering/domain.StaticResolver) and needs no customer table; a
-- multi-tenant one points them at its own. Neither is imposed here.
--
-- delta is signed: negative consumes, positive credits back. Refunds and
-- top-ups are the same operation with the opposite sign.

CREATE TABLE IF NOT EXISTS usage_units (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    app_id      UUID NOT NULL,
    code        TEXT NOT NULL,
    name        TEXT NOT NULL,
    unit_amount BIGINT NOT NULL DEFAULT 0,
    currency    TEXT NOT NULL DEFAULT 'EUR',
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (app_id, code)
);

CREATE TABLE IF NOT EXISTS usage_ledger (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    app_id          UUID NOT NULL,
    customer_id     UUID NOT NULL,
    subscription_id UUID,
    unit            TEXT NOT NULL,
    delta           BIGINT NOT NULL,
    idempotency_key TEXT NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (app_id, idempotency_key)
);

-- Ordered to serve the only read on the hot path: what this customer has
-- consumed of this unit, inside this window.
CREATE INDEX IF NOT EXISTS idx_usage_ledger_balance
    ON usage_ledger(app_id, customer_id, unit, occurred_at);
