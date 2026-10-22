CREATE UNIQUE INDEX IF NOT EXISTS lungor_outbox_idempotency_key
    ON lungor_outbox ((payload->>'idempotency_key'));

CREATE TABLE IF NOT EXISTS lungor_usage_counters (
    subject      text        NOT NULL,
    unit         text        NOT NULL,
    used         bigint      NOT NULL DEFAULT 0,
    period_end   timestamptz,
    synced_at    timestamptz,
    PRIMARY KEY (subject, unit)
);
