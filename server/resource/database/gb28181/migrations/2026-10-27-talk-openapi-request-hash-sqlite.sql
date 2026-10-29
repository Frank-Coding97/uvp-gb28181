-- models.GbTalkSession 早就按这列读写，而 baseline 只覆盖「建新库」；
-- 存量库升级后少这一列，写对讲会话直接失败，前端只看到「语音对讲操作失败」。
-- 2026-10-27 在 62 上级实测复现（1.1.13 存量库 40 列）。
ALTER TABLE "gb_talk_session" ADD COLUMN "openapi_request_hash" TEXT NOT NULL DEFAULT '';
