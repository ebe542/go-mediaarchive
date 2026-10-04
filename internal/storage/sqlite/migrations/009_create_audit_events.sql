CREATE TABLE audit_events (
    id TEXT PRIMARY KEY NOT NULL,
    occurred_at TEXT NOT NULL,
    event_type TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'denied', 'failure')),
    actor_id TEXT,
    actor_username TEXT,
    actor_role TEXT,
    target_type TEXT NOT NULL,
    target_id TEXT,
    target_name TEXT,
    reason TEXT,
    range_start INTEGER,
    range_end INTEGER,
    range_total INTEGER,
    CHECK (
        (actor_id IS NULL AND actor_username IS NULL AND actor_role IS NULL)
        OR
        (actor_id IS NOT NULL AND actor_username IS NOT NULL AND actor_role IS NOT NULL)
    ),
    CHECK (
        (range_start IS NULL AND range_end IS NULL AND range_total IS NULL)
        OR
        (
            range_start >= 0
            AND range_end >= range_start
            AND range_total > range_end
        )
    )
) STRICT;

CREATE INDEX audit_events_occurred_at_id_index
    ON audit_events (occurred_at DESC, id DESC);

CREATE TRIGGER audit_events_reject_update
BEFORE UPDATE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events are immutable');
END;

CREATE TRIGGER audit_events_reject_delete
BEFORE DELETE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events are immutable');
END;
