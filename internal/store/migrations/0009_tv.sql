CREATE TABLE tv_devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    token_hash BLOB NOT NULL UNIQUE,
    paired_at TEXT NOT NULL
);
