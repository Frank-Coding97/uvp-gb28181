ALTER TABLE "gb_cascade_media_session" ADD COLUMN IF NOT EXISTS "session_kind" VARCHAR(16) NOT NULL DEFAULT 'video';
