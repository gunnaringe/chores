-- Timestamp columns store RFC3339 UTC strings (e.g. "2024-01-02T15:04:05Z")
-- formatted and parsed explicitly in Go, rather than relying on the sqlite
-- driver's DECLTYPE-based auto-conversion, which is lost across aggregate
-- functions like MAX(). A plain TEXT column keeps behavior identical for
-- direct reads and aggregates alike.

CREATE TABLE IF NOT EXISTS families (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('parent', 'child')),
    created_at TEXT NOT NULL,
    -- The login identity (Auth0 "sub") bound to this user, if any. Only
    -- ever set for parents; children don't log in themselves. NULL until an
    -- invitation is accepted (or the founding parent's first login).
    auth_subject TEXT UNIQUE,
    email TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_users_family ON users(family_id);

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_cents INTEGER NOT NULL,
    schedule TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_family ON tasks(family_id);

CREATE TABLE IF NOT EXISTS task_completions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    child_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    due_date TEXT NOT NULL,
    amount_cents INTEGER NOT NULL,
    completed_at TEXT NOT NULL,
    UNIQUE (task_id, child_id, due_date)
);
CREATE INDEX IF NOT EXISTS idx_completions_child ON task_completions(child_id);
CREATE INDEX IF NOT EXISTS idx_completions_family ON task_completions(family_id);

CREATE TABLE IF NOT EXISTS payouts (
    id TEXT PRIMARY KEY,
    child_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    amount_cents INTEGER NOT NULL,
    full_payout INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_payouts_child ON payouts(child_id);

CREATE TABLE IF NOT EXISTS invitations (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    accepted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_invitations_family ON invitations(family_id);
