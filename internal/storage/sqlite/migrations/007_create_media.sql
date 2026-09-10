CREATE TABLE media_items (
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

    title TEXT NOT NULL
        CHECK (
            length(title) BETWEEN 1 AND 200
            AND title = trim(title)
        ),

    original_filename TEXT NOT NULL
        CHECK (
            length(original_filename) BETWEEN 1 AND 255
            AND original_filename = trim(original_filename)
            AND original_filename NOT IN ('.', '..')
            AND instr(original_filename, '/') = 0
            AND instr(original_filename, '\') = 0
            AND instr(original_filename, ':') = 0
        ),

    media_type TEXT NOT NULL
        CHECK (media_type IN ('book', 'document', 'video')),

    mime_type TEXT NOT NULL
        CHECK (
            length(mime_type) BETWEEN 3 AND 127
            AND mime_type = trim(mime_type)
            AND mime_type = lower(mime_type)
            AND instr(mime_type, '/') > 1
            AND instr(mime_type, '/') < length(mime_type)
            AND length(mime_type) - length(replace(mime_type, '/', '')) = 1
        ),

    size INTEGER NOT NULL
        CHECK (size > 0),

    checksum BLOB NOT NULL
        CHECK (
            typeof(checksum) = 'blob'
            AND length(checksum) = 32
        ),

    owner_id TEXT NOT NULL
        REFERENCES users(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    created_at TEXT NOT NULL
        CHECK (length(created_at) > 0),

    updated_at TEXT NOT NULL
        CHECK (
            length(updated_at) > 0
            AND updated_at >= created_at
        )
) STRICT;

CREATE INDEX media_items_owner_id_index
    ON media_items (owner_id);

CREATE TABLE media_authors (
    media_id TEXT NOT NULL
        REFERENCES media_items(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    position INTEGER NOT NULL
        CHECK (position >= 0),

    name TEXT NOT NULL
        CHECK (
            length(name) BETWEEN 1 AND 100
            AND name = trim(name)
        ),

    PRIMARY KEY (media_id, position),
    UNIQUE (media_id, name)
) STRICT;

CREATE TABLE media_grants (
    media_id TEXT NOT NULL
        REFERENCES media_items(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    user_id TEXT NOT NULL
        REFERENCES users(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    permissions INTEGER NOT NULL
        CHECK (permissions BETWEEN 1 AND 63),

    PRIMARY KEY (media_id, user_id)
) STRICT;

CREATE INDEX media_grants_user_id_index
    ON media_grants (user_id);
