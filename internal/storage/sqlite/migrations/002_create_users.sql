CREATE TABLE users (
    id TEXT PRIMARY KEY NOT NULL
        CHECK (
            length(id) = 36
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 24, 1) = '-'
            AND substr(id, 1, 8) NOT GLOB '*[^0-9a-f]*'
            AND substr(id, 10, 4) NOT GLOB '*[^0-9a-f]*'
            AND substr(id, 15, 4) NOT GLOB '*[^0-9a-f]*'
            AND substr(id, 20, 4) NOT GLOB '*[^0-9a-f]*'
            AND substr(id, 25, 12) NOT GLOB '*[^0-9a-f]*'
        ),

    username TEXT COLLATE NOCASE NOT NULL UNIQUE
        CHECK (
            length(username) BETWEEN 3 AND 32
            AND username = lower(username)
            AND username = trim(username)
            AND substr(username, 1, 1) GLOB '[a-z0-9]'
            AND username NOT GLOB '*[^a-z0-9_-]*'
        ),

    display_name TEXT NOT NULL
        CHECK (
            length(trim(display_name)) BETWEEN 1 AND 100
            AND display_name = trim(display_name)
        ),

    role TEXT NOT NULL
        CHECK (role IN ('viewer', 'editor', 'admin')),

    active INTEGER NOT NULL DEFAULT 1
        CHECK (active IN (0, 1)),

    created_at TEXT NOT NULL
        CHECK (length(created_at) > 0),

    updated_at TEXT NOT NULL
        CHECK (length(updated_at) > 0)
) STRICT;