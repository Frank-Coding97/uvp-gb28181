-- 2026-07-25: persist the user-selected GB28181 audio session mode.
-- PostgreSQL 12+; repeatable for both fresh and upgraded installations.

ALTER TABLE IF EXISTS gb_talk_session
    ADD COLUMN IF NOT EXISTS mode VARCHAR(16) NOT NULL DEFAULT 'talk';

DO $$
BEGIN
    IF to_regclass('gb_talk_session') IS NOT NULL THEN
        UPDATE gb_talk_session
        SET mode = 'talk'
        WHERE mode IS NULL OR mode NOT IN ('broadcast', 'talk');
    END IF;
END $$;
