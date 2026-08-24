CREATE TABLE IF NOT EXISTS legacy_conversion_reports (
    id TEXT PRIMARY KEY,
    source_snapshot_id TEXT NOT NULL UNIQUE,
    converter_version TEXT NOT NULL,
    backup_location TEXT NOT NULL,
    digest TEXT NOT NULL,
    created_at TEXT NOT NULL,
    payload TEXT NOT NULL
);
