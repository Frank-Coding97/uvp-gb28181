-- 通道快照(播放触发)- down
-- 2026-07-20: 回滚 gb_channel 的 snapshot_url / snapshot_at 字段

ALTER TABLE `gb_channel`
    DROP COLUMN `snapshot_url`,
    DROP COLUMN `snapshot_at`;
