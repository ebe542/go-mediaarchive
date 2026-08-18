CREATE TABLE password_credentials (
    user_id TEXT PRIMARY KEY NOT NULL
        REFERENCES users(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    password_hash TEXT NOT NULL
        CHECK (
            length(password_hash) > 0
            AND password_hash GLOB '$argon2id$*'
        ),

    created_at TEXT NOT NULL
        CHECK (length(created_at) > 0),

    updated_at TEXT NOT NULL
        CHECK (length(updated_at) > 0)
) STRICT;