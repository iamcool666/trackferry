-- What each signed-in browser is called, so the family can tell whose phone asked for a song.
ALTER TABLE sessions ADD COLUMN name TEXT NOT NULL DEFAULT '';
