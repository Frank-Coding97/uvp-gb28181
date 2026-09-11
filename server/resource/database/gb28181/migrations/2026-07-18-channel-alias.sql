-- 通道用户别名
-- 2026-07-18: 为 gb_channel 添加 alias 字段,支持用户自定义别名
-- name 保留 Catalog 上报值; alias 由用户编辑,前端优先展示 alias,为空则回退 name。

ALTER TABLE `gb_channel`
ADD COLUMN `alias` VARCHAR(255) NOT NULL DEFAULT ''
COMMENT '用户自定义别名(用户可编辑,不被 Catalog 上报覆盖)' AFTER `name`;
