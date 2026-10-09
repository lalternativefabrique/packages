CREATE TABLE IF NOT EXISTS entitlements (
    subject      text PRIMARY KEY,
    plan         text NOT NULL,
    limits       jsonb NOT NULL DEFAULT '{}'::jsonb,
    refreshed_at timestamptz NOT NULL
);
