CREATE INDEX media_items_directory_index
    ON media_items (created_at DESC, id DESC);

CREATE INDEX media_items_type_directory_index
    ON media_items (media_type, created_at DESC, id DESC);
