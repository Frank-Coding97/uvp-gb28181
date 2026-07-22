-- 通道级按需直播开关
-- 2026-07-22: 新注册通道默认开启,无人观看时由 ZLM Hook 自动关闭

ALTER TABLE `gb_channel`
    ADD COLUMN `on_demand_live` TINYINT(1) NOT NULL DEFAULT '1'
        COMMENT '按需直播 1=无人观看自动关闭'
        AFTER `stream_id`;

CREATE INDEX `idx_stream_id` ON `gb_channel` (`stream_id`);
