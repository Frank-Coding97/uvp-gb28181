-- 回退「运行日志」菜单图标为空（原始态）。
-- 该菜单在补图标前的 icon 就是空串，故 down 恢复为 ''。
-- 幂等：SQL Server。定位同 up，用权限码而非 path。
UPDATE sys_menu SET icon=N'', updated_at=CURRENT_TIMESTAMP
WHERE permission='gb28181:log:view' AND deleted_at IS NULL AND icon=N'lucide:Terminal';
