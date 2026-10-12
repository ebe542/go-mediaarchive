CREATE TABLE content_audit_markers (
    session_hash BLOB NOT NULL,
    media_id TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'denied', 'failure')),
    reason TEXT NOT NULL DEFAULT '',
    recorded_at TEXT NOT NULL,
    PRIMARY KEY (session_hash, media_id, outcome, reason),
    FOREIGN KEY (session_hash) REFERENCES sessions (token_hash)
        ON UPDATE RESTRICT
        ON DELETE CASCADE,
    CHECK (length(session_hash) = 32),
    CHECK (length(media_id) = 36),
    CHECK (
        (outcome = 'success' AND reason = '') OR
        (outcome <> 'success' AND reason <> '')
    )
) STRICT, WITHOUT ROWID;
