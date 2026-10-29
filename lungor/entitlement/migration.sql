CREATE TABLE IF NOT EXISTS entitlements (
    subject      text PRIMARY KEY,
    plan         text NOT NULL,
    limits       jsonb NOT NULL DEFAULT '{}'::jsonb,
    refreshed_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS entitlement_inbox (
    source_event_id text PRIMARY KEY,
    delivery_id     text NOT NULL DEFAULT '',
    type            text NOT NULL,
    subject         text NOT NULL DEFAULT '',
    payload         jsonb NOT NULL,
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
    attempts        integer NOT NULL DEFAULT 0,
    last_error      text,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    received_at     timestamptz NOT NULL DEFAULT now(),
    processed_at    timestamptz
);

CREATE INDEX IF NOT EXISTS entitlement_inbox_due ON entitlement_inbox (next_attempt_at) WHERE status = 'pending';
