-- 固件仓库菜单注册
-- 添加固件仓库独立菜单项（与"设备列表"平级）
-- Migration: 2026-10-02-firmware-repository-menu

-- sys_menu: 固件仓库菜单项
INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`) VALUES
(140600, 0, '/gb28181/firmware-repo/index', 'firmware-repository-list', '', 'gb28181/firmware-repo/index', '固件仓库', 0, 0, 0, 0, 0, '', 0, '', 'lucide:HardDrive', 21, 2, 0, '', NOW(), NOW(), NULL, 1);

-- sys_menu: 固件仓库权限按钮（已在 Task 5 提到但未实际插入）
INSERT INTO `sys_menu` (`id`, `parent_id`, `path`, `name`, `redirect`, `component`, `title`, `is_full`, `hide`, `disable`, `keep_alive`, `affix`, `link`, `iframe`, `svg_icon`, `icon`, `sort`, `type`, `is_link`, `permission`, `created_at`, `updated_at`, `deleted_at`, `created_by`) VALUES
(140601, 140600, '', 'FirmwareRepositoryUpload', NULL, '', '上传固件', 0, 0, 0, 0, 0, '', 0, '', '', 1, 3, 0, 'gb28181:firmware:upload', NOW(), NOW(), NULL, 1),
(140602, 140600, '', 'FirmwareRepositoryDelete', NULL, '', '删除固件', 0, 0, 0, 0, 0, '', 0, '', '', 2, 3, 0, 'gb28181:firmware:delete', NOW(), NOW(), NULL, 1),
(140603, 140600, '', 'FirmwareRepositoryDownload', NULL, '', '下载固件', 0, 0, 0, 0, 0, '', 0, '', '', 3, 3, 0, 'gb28181:firmware:download', NOW(), NOW(), NULL, 1);

-- sys_api: 固件仓库 API（6 个接口）
INSERT INTO `sys_api` (`id`, `title`, `path`, `method`, `group`, `created_at`, `updated_at`, `deleted_at`, `created_by`) VALUES
(621, '上传固件', '/api/gb28181/device-mgmt/firmware-repository', 'POST', '固件仓库', NOW(), NOW(), NULL, 1),
(622, '固件列表', '/api/gb28181/device-mgmt/firmware-repository', 'GET', '固件仓库', NOW(), NOW(), NULL, 1),
(623, '固件详情', '/api/gb28181/device-mgmt/firmware-repository/:id', 'GET', '固件仓库', NOW(), NOW(), NULL, 1),
(624, '删除固件', '/api/gb28181/device-mgmt/firmware-repository/:id', 'DELETE', '固件仓库', NOW(), NOW(), NULL, 1),
(625, '生成下载链接', '/api/gb28181/device-mgmt/firmware-repository/:id/download-link', 'POST', '固件仓库', NOW(), NOW(), NULL, 1),
(626, '下载固件（token）', '/api/gb28181/device-mgmt/firmware-repository/download/:token', 'GET', '固件仓库', NOW(), NOW(), NULL, 1);

-- sys_menu_api: 菜单与 API 关联
INSERT INTO `sys_menu_api` (`menu_id`, `api_id`) VALUES
(140600, 621),  -- 固件仓库菜单 -> 上传固件
(140600, 622),  -- 固件仓库菜单 -> 固件列表
(140600, 623),  -- 固件仓库菜单 -> 固件详情
(140600, 624),  -- 固件仓库菜单 -> 删除固件
(140600, 625),  -- 固件仓库菜单 -> 生成下载链接
(140600, 626);  -- 固件仓库菜单 -> 下载固件（token）

-- sys_casbin_rule: 管理员角色授权（role_id = 1）
INSERT INTO `sys_casbin_rule` (`ptype`, `v0`, `v1`, `v2`, `v3`, `v4`, `v5`) VALUES
('p', '1', '/api/gb28181/device-mgmt/firmware-repository', 'POST', '', '', ''),
('p', '1', '/api/gb28181/device-mgmt/firmware-repository', 'GET', '', '', ''),
('p', '1', '/api/gb28181/device-mgmt/firmware-repository/:id', 'GET', '', '', ''),
('p', '1', '/api/gb28181/device-mgmt/firmware-repository/:id', 'DELETE', '', '', ''),
('p', '1', '/api/gb28181/device-mgmt/firmware-repository/:id/download-link', 'POST', '', '', ''),
('p', '1', '/api/gb28181/device-mgmt/firmware-repository/download/:token', 'GET', '', '', '');
