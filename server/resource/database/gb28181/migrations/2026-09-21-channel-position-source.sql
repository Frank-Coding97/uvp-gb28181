-- 通道坐标来源标记 gb_channel.position_source / position_updated_at（MySQL 5.7+）.
--
-- 背景：`gb_channel.longitude/latitude` 这对列此前**只有一条写入通路** —— 设备 Catalog 应答
-- Item 层的 `<Longitude>`/`<Latitude>`（A.2.1.9，两版都是 minOccurs=0 的可选元素）。
-- 实际结果是：设备不报就永远是 0，界面上只能显示"无坐标"；而设备真报了一次之后，
-- 下一轮不带坐标的目录刷新又会把它清零（upsert 无条件写）。
--
-- 本轮把这对列扩成三通路的**共用落点**（实时位置 / 目录声明 / 人工录入），
-- 因此必须能回答"这个坐标是谁写的" —— 这就是 position_source 的职责。
--
-- 取值（models.ChannelPositionSource*，与 Go 常量逐字一致）：
--   'catalog' 设备在 Catalog 里声明的安装位置
--   'mobile'  设备 MobilePosition 上报的实时位置（§9.5.1 / A.2.5.6）
--   'manual'  人工在通道编辑里录入（协议上不存在，是平台侧能力）
--   ''        从未有过坐标（与 longitude=0 && latitude=0 互为充要）
--
-- ⛔ 优先关系写在**写入侧**，不是这一列：
--     实时位置（mobile）恒覆盖；人工值（manual）不被目录刷新覆盖；
--     目录（catalog）只在当前不是 manual/mobile 时才写。见 catalog/upsert.go 的覆盖守卫。
--
-- 幂等：每列先查 information_schema 再决定是否执行。

SET @ch_position_source_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'position_source'
);
SET @ch_position_source_sql := IF(
  @ch_position_source_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `position_source` varchar(16) NOT NULL DEFAULT '''' COMMENT ''通道坐标来源 catalog/mobile/manual,空=无坐标'' AFTER `latitude`',
  'SELECT 1'
);
PREPARE ch_position_source_stmt FROM @ch_position_source_sql;
EXECUTE ch_position_source_stmt;
DEALLOCATE PREPARE ch_position_source_stmt;

SET @ch_position_updated_exists := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'gb_channel' AND COLUMN_NAME = 'position_updated_at'
);
SET @ch_position_updated_sql := IF(
  @ch_position_updated_exists = 0,
  'ALTER TABLE `gb_channel` ADD COLUMN `position_updated_at` datetime(3) DEFAULT NULL COMMENT ''通道坐标最近更新时间'' AFTER `position_source`',
  'SELECT 1'
);
PREPARE ch_position_updated_stmt FROM @ch_position_updated_sql;
EXECUTE ch_position_updated_stmt;
DEALLOCATE PREPARE ch_position_updated_stmt;

-- 存量数据回填：把已有非零坐标标成 catalog（此前只有目录这一条通路）。
-- ⛔ 只在 position_source 为空时回填，重跑不覆盖 mobile/manual。
UPDATE `gb_channel`
SET `position_source` = 'catalog'
WHERE `position_source` = '' AND (`longitude` <> 0 OR `latitude` <> 0);
