-- Preserve complete GB/T 28181-2022 ParentID values without splitting or truncation.
ALTER TABLE `gb_channel` MODIFY COLUMN `parent_id` VARCHAR(512) NOT NULL DEFAULT '' COMMENT '父节点编码(目录树,2022 多父值按 / 原样保存)';
