-- Timestamp columns store RFC3339 UTC strings (e.g. "2024-01-02T15:04:05Z")
-- formatted and parsed explicitly in Go, rather than relying on the sqlite
-- driver's DECLTYPE-based auto-conversion, which is lost across aggregate
-- functions like MAX(). A plain TEXT column keeps behavior identical for
-- direct reads and aggregates alike.

CREATE TABLE IF NOT EXISTS families (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    -- A secret bearer token for the family's Today-tab kiosk dashboard
    -- (see internal/server/dashboard.go). NULL means the dashboard hasn't
    -- been set up. Its uniqueness is enforced by idx_families_dashboard_key
    -- in migrate(), not inline here — SQLite's ALTER TABLE ADD COLUMN can't
    -- add a UNIQUE column, so both the fresh-create and migrated-database
    -- paths need to go through the same index-based route anyway.
    dashboard_key TEXT
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('parent', 'child')),
    created_at TEXT NOT NULL,
    -- The login identity (Auth0 "sub") bound to this user row, if any. Not
    -- unique: one login can be bound to more than one row (e.g. a child
    -- who's a member of two families), but never to two rows in the *same*
    -- family. NULL until an invitation is accepted (or the founding
    -- parent's first login).
    auth_subject TEXT,
    email TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_users_family ON users(family_id);
-- idx_users_auth_subject is created in migrate(), not here: schema.sql runs
-- unconditionally via CREATE TABLE IF NOT EXISTS, even against an old table
-- shape that doesn't have the auth_subject column yet, and creating an
-- index on a not-yet-existing column would fail outright.

CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_cents INTEGER NOT NULL,
    -- Raw cron expression; only populated (and only meaningful) when
    -- repeat_mode = 'cron'.
    schedule TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    -- icon_type is 'emoji' or 'fontawesome' (empty string alongside an
    -- empty icon_value means no icon at all).
    icon_type TEXT NOT NULL DEFAULT '',
    icon_value TEXT NOT NULL DEFAULT '',
    -- 'once' (due once on start_date), 'weekly' (due on days_of_week every
    -- repeat_interval_weeks weeks, counted from start_date), or 'cron' (due
    -- whenever `schedule` matches).
    repeat_mode TEXT NOT NULL DEFAULT 'cron',
    -- Comma-separated 0=Sunday..6=Saturday, e.g. "1,3,5". Only meaningful
    -- for repeat_mode = 'weekly'.
    days_of_week TEXT NOT NULL DEFAULT '',
    repeat_interval_weeks INTEGER NOT NULL DEFAULT 1,
    -- YYYY-MM-DD. See the WeeklySchedule.anchor_date and OnceSchedule.date
    -- proto comments for what this means per repeat_mode.
    start_date TEXT NOT NULL DEFAULT '',
    -- RFC3339 UTC, or NULL for a task that hasn't been deleted. Deletion is
    -- soft so that the occurrences a task produced outlive it, and so its
    -- schedule stays available to reconstruct past occurrences that were
    -- never completed. A deleted task generates no occurrences from this
    -- date onward. See the Task.deleted_at proto comment.
    deleted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_tasks_family ON tasks(family_id);

CREATE TABLE IF NOT EXISTS task_assignments (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    child_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, child_id)
);
CREATE INDEX IF NOT EXISTS idx_task_assignments_child ON task_assignments(child_id);

-- One instance of a task, for one child, on one date. A row exists once
-- something has been recorded about that instance; instances nothing has
-- happened to yet are derived from the task's schedule at read time and
-- never stored. See the TaskOccurrence proto for the full model.
CREATE TABLE IF NOT EXISTS task_occurrences (
    id TEXT PRIMARY KEY,
    -- Deliberately NOT a foreign key. A task can be deleted, and the
    -- occurrences it produced must outlive it — a cascade here is exactly
    -- the bug this table replaces, where deleting a task erased the
    -- earnings that justified payouts already made. Kept as a plain
    -- historical reference so occurrences can still be grouped by task.
    task_id TEXT NOT NULL,
    child_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    due_date TEXT NOT NULL,
    -- How the task read when this row was written, copied rather than
    -- joined at read time so that renaming or repricing a task never
    -- rewrites what already happened.
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    icon_type TEXT NOT NULL DEFAULT '',
    icon_value TEXT NOT NULL DEFAULT '',
    amount_cents INTEGER NOT NULL,
    -- NULL means the occurrence is due but not completed. Every earnings
    -- query MUST filter on this being non-NULL, or amounts for chores
    -- nobody did inflate the balance.
    completed_at TEXT,
    UNIQUE (task_id, child_id, due_date)
);
CREATE INDEX IF NOT EXISTS idx_occurrences_child ON task_occurrences(child_id);
CREATE INDEX IF NOT EXISTS idx_occurrences_family ON task_occurrences(family_id);
-- Covering partial indexes for the balance aggregates in computeSummary.
-- The column order matters: each one carries amount_cents as its last
-- column so the SUM is answered from the index alone, without touching the
-- table. Measured on a 50k-row family, these take ListChildSummaries from
-- ~90ms to ~6ms; at 200k rows it's ~515ms to ~148ms. Two indexes rather
-- than one because the queries split into completed_at-bounded and
-- due_date-bounded shapes, and a single index makes the planner pick wrong
-- for whichever half it doesn't fit.
CREATE INDEX IF NOT EXISTS idx_occurrences_earned_by_completed
    ON task_occurrences(child_id, completed_at, amount_cents) WHERE completed_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_occurrences_earned_by_due
    ON task_occurrences(child_id, due_date, amount_cents) WHERE completed_at IS NOT NULL;

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
-- Covering, for the same reason as the task_occurrences pair above: the
-- paid-out half of every child's balance is answered from the index alone.
CREATE INDEX IF NOT EXISTS idx_payouts_child_amount ON payouts(child_id, amount_cents);

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

-- Small generic key/value store for app-wide config that isn't tied to any
-- family — currently just the generated VAPID keypair used to sign Web Push
-- messages.
CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- One row per browser/device that has enabled push notifications, tied to
-- whichever user was active there at the time. endpoint is unique so
-- re-subscribing (e.g. after the browser rotates it, or re-enabling after
-- disabling) replaces the old row instead of accumulating stale ones.
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_push_subscriptions_family ON push_subscriptions(family_id);
