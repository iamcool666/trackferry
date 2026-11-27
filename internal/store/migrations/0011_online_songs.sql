-- Online songs shown by a search, so a queue that names one by its key can still play it later.
CREATE TABLE online_songs (
    key     TEXT PRIMARY KEY,
    song    TEXT NOT NULL,
    seen_ms INTEGER NOT NULL
);
CREATE INDEX online_songs_seen ON online_songs(seen_ms);
