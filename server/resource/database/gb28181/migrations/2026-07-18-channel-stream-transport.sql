-- 通道级流媒体传输模式
-- 2026-07-18: 为 gb_channel 添加 stream_transport 字段,支持通道级流传输模式控制

ALTER TABLE `gb_channel`
ADD COLUMN `stream_transport` VARCHAR(16) NOT NULL DEFAULT 'TCP-Passive'
COMMENT '流传输模式: UDP / TCP-Active / TCP-Passive' AFTER `stream_id`;

-- 创建索引以优化按传输模式查询
CREATE INDEX `idx_stream_transport` ON `gb_channel` (`stream_transport`);
