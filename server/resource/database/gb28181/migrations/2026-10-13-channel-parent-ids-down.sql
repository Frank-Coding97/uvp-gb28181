-- The runner refuses this downgrade while any ParentID exceeds 20 characters.
ALTER TABLE `gb_channel` MODIFY COLUMN `parent_id` VARCHAR(20) NOT NULL DEFAULT '' COMMENT '父节点编码(目录树)';
