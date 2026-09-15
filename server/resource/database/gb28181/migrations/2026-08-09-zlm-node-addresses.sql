-- 2026-08-09 ZLM 节点地址职责拆分。
-- receive_host: 写入 GB28181 SDP,设备向此地址发送 RTP。
-- playback_host: 返回给浏览器/客户端的播放访问地址,可填写公网 IP 或域名。
SET @sql = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE `meta_node` ADD COLUMN `receive_host` varchar(255) NOT NULL DEFAULT '''' AFTER `host`',
    'SELECT 1'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'meta_node'
    AND column_name = 'receive_host'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @sql = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE `meta_node` ADD COLUMN `playback_host` varchar(255) NOT NULL DEFAULT '''' AFTER `receive_host`',
    'SELECT 1'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'meta_node'
    AND column_name = 'playback_host'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
