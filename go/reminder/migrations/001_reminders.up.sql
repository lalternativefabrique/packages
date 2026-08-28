CREATE TABLE IF NOT EXISTS reminders (
    id         TEXT PRIMARY KEY,
    user_id    TEXT,
    body       TEXT NOT NULL,
    due_at     TIMESTAMPTZ NOT NULL,
    status     TEXT NOT NULL,          -- 'pending' | 'fired' | 'cancelled' | 'done'
    created_at TIMESTAMPTZ NOT NULL,
    fired_at   TIMESTAMPTZ,
    channels   JSONB NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS reminders_due_idx
    ON reminders (due_at) WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS reminders_user_idx
    ON reminders (user_id, due_at);
