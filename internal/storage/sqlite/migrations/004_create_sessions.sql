CREATE TABLE sessions (
    token_hash BLOB PRIMARY KEY NOT NULL
        CHECK (
            typeof(token_hash) = 'blob'
            AND length(token_hash) = 32
        ),

    user_id TEXT NOT NULL
        REFERENCES users(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    created_at TEXT NOT NULL
        CHECK (length(created_at) > 0),

    last_seen_at TEXT NOT NULL
        CHECK (
            length(last_seen_at) > 0
            AND last_seen_at >= created_at
        ),

    expires_at TEXT NOT NULL
        CHECK (
            length(expires_at) > 0
            AND expires_at > created_at
            AND last_seen_at <= expires_at
        ),

    revoked_at TEXT
        CHECK (
            revoked_at IS NULL
            OR length(revoked_at) > 0
        )
) STRICT;

CREATE INDEX sessions_user_id_index
    ON sessions (user_id);