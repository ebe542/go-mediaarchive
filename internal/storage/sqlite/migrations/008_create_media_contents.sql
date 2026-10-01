CREATE TABLE media_contents (
    media_id TEXT PRIMARY KEY NOT NULL
        REFERENCES media_items(id)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT,

    storage_key TEXT NOT NULL UNIQUE
        CHECK (
            length(storage_key) BETWEEN 1 AND 255
            AND storage_key = trim(storage_key)
            AND substr(storage_key, 1, 1) != '/'
            AND instr(storage_key, '\') = 0
            AND instr(storage_key, ':') = 0
            AND instr('/' || storage_key || '/', '/../') = 0
            AND instr('/' || storage_key || '/', '/./') = 0
        ),

    stored_at TEXT NOT NULL
        CHECK (length(stored_at) > 0)
) STRICT;
