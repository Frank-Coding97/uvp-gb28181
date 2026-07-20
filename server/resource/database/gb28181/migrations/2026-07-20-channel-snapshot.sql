-- 通道快照(播放触发)
-- 2026-07-20: 为 gb_channel 添加 snapshot_url / snapshot_at 字段
-- 每次播放通道成功后,后台 fire-and-forget 抓一张 ZLMediaKit JPEG 快照落盘,
-- 前端通道列表 / ControlConsole 卡片展示缩略图。
-- see: wiki/projects/uvp/specs/channel-snapshot-on-play.md

ALTER TABLE `gb_channel`
    ADD COLUMN `snapshot_url` VARCHAR(500) NOT NULL DEFAULT ''
        COMMENT '通道最新快照 URL(相对路径,如 /uploads/gb-channel-snapshot/2026-07/xxx_yyy.jpg)'
        AFTER `capabilities`,
    ADD COLUMN `snapshot_at` DATETIME NULL
        COMMENT '通道最新快照抓拍时间'
        AFTER `snapshot_url`;
