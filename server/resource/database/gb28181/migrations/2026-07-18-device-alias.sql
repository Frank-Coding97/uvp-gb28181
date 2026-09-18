-- 设备用户别名
-- 2026-07-18: 为 gb_device 添加 alias 字段,支持用户自定义别名
-- 冲突背景:name 字段由设备通过 DeviceInfo 应答上报,前端用户编辑名称会被下一次上报覆盖
-- 解决方案:name 保留给设备自上报,alias 由用户编辑,前端展示优先取 alias,为空则 fallback 到 name

ALTER TABLE `gb_device`
ADD COLUMN `alias` VARCHAR(255) NOT NULL DEFAULT ''
COMMENT '用户自定义别名(用户可编辑,不被设备上报覆盖)' AFTER `name`;
