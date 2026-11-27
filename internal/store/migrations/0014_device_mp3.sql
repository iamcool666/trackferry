-- Lossless songs reach this speaker as MP3, made by ffmpeg, for models that cannot play FLAC.
ALTER TABLE devices ADD COLUMN mp3 INTEGER NOT NULL DEFAULT 0;
