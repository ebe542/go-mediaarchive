CREATE TABLE password_enrollments (
    user_id TEXT PRIMARY KEY NOT NULL
        REFERENCES users(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    token_hash BLOB NOT NULL UNIQUE
        CHECK (
            typeof(token_hash) = 'blob'
            AND length(token_hash) = 32
        ),

    created_at TEXT NOT NULL
        CHECK (length(created_at) > 0),

    expires_at TEXT NOT NULL
        CHECK (
            length(expires_at) > 0
            AND expires_at > created_at
        )
) STRICT;

CREATE INDEX password_enrollments_expires_at_index
    ON password_enrollments (expires_at);
