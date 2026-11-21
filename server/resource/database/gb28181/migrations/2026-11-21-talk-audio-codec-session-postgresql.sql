ALTER TABLE "gb_talk_session" ADD COLUMN IF NOT EXISTS "audio_codec" VARCHAR(16) NOT NULL DEFAULT 'pcma';
