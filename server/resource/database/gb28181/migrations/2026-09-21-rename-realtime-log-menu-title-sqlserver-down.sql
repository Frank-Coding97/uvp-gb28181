-- 回退「运行日志」为「实时日志控制台」。
-- 幂等：SQL Server。定位同 up，用权限码而非 path。
UPDATE sys_menu
SET title=N'实时日志控制台', updated_at=CURRENT_TIMESTAMP
WHERE permission='gb28181:log:view' AND deleted_at IS NULL AND title=N'运行日志';
