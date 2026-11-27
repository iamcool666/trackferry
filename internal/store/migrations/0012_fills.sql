-- Lyrics of online songs, and what was found online for library songs that lack their own.
-- An empty lyrics with a time is a miss: it is not asked again until a while has passed.
CREATE TABLE online_lyrics (
    key        TEXT PRIMARY KEY,
    lyrics     TEXT NOT NULL,
    fetched_ms INTEGER NOT NULL
);
CREATE INDEX online_lyrics_fetched ON online_lyrics(fetched_ms);

CREATE TABLE track_fills (
    track_id        INTEGER PRIMARY KEY,
    cover_tried_ms  INTEGER NOT NULL DEFAULT 0,
    lyrics          TEXT NOT NULL DEFAULT '',
    lyrics_from     TEXT NOT NULL DEFAULT '',
    lyrics_tried_ms INTEGER NOT NULL DEFAULT 0
);
