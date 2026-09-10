-- 作业单表单历史值（MySQL 5.7+）。
-- 让「项目名称 / 站区 / 作业负责人 / 作业人员」四个字段支持「录入后寄存，
-- 后续下拉选用」：同字段同值用唯一键去重，提交作业单时 upsert。
-- 列表接口按 last_used_at desc 限定条数，避免历史过长时下拉被噪声淹没。

CREATE TABLE IF NOT EXISTS `gb_work_order_form_history` (
  `id` BIGINT NOT NULL AUTO_INCREMENT,
  `field_key` VARCHAR(64) NOT NULL,
  `value` VARCHAR(256) NOT NULL,
  `use_count` INT NOT NULL DEFAULT 1,
  `last_used_at` DATETIME NOT NULL,
  `created_at` DATETIME NOT NULL,
  `updated_at` DATETIME NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_gb_work_order_form_history_field_value` (`field_key`, `value`),
  KEY `idx_gb_work_order_form_history_field_used` (`field_key`, `last_used_at`),
  KEY `idx_gb_work_order_form_history_field_count` (`field_key`, `use_count`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
