-- SQL Server SQL 转换文件
-- 由 MySQL SQL 转换而来

SET NOCOUNT ON;

IF OBJECT_ID('gb_zlm_managed_resource','U') IS NOT NULL DROP TABLE gb_zlm_managed_resource;
CREATE TABLE gb_zlm_managed_resource (
  id BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
  node_id BIGINT NOT NULL,
  resource_type NVARCHAR(32) NOT NULL,
  resource_key NVARCHAR(255) NOT NULL,
  app NVARCHAR(64) NOT NULL DEFAULT '',
  stream NVARCHAR(255) NOT NULL DEFAULT '',
  identity_fingerprint CHAR(64) NOT NULL,
  summary NVARCHAR(512) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME2(3) NOT NULL,
  last_observed_at DATETIME2(3) NULL,
  tombstoned_at DATETIME2(3) NULL,
  updated_at DATETIME2(3) NOT NULL
);
CREATE UNIQUE INDEX uk_gb_zlm_managed_resource_identity ON gb_zlm_managed_resource(node_id,resource_type,resource_key);
CREATE INDEX idx_gb_zlm_managed_resource_observed ON gb_zlm_managed_resource(node_id,last_observed_at);
CREATE INDEX idx_gb_zlm_managed_resource_tombstone ON gb_zlm_managed_resource(node_id,tombstoned_at);

IF OBJECT_ID('gb_channel','U') IS NOT NULL AND COL_LENGTH('gb_channel','recording_mode') IS NULL ALTER TABLE gb_channel ADD recording_mode NVARCHAR(16) NOT NULL DEFAULT 'off';
IF OBJECT_ID('gb_recording_plan_gap','U') IS NOT NULL DROP TABLE gb_recording_plan_gap;
CREATE TABLE gb_recording_plan_gap (id BIGINT IDENTITY(1,1) PRIMARY KEY, plan_id BIGINT NULL, channel_id BIGINT NOT NULL, started_at DATETIME2(3) NOT NULL, ended_at DATETIME2(3) NULL, duration_ms BIGINT NOT NULL DEFAULT 0, reason_code NVARCHAR(64) NOT NULL, reason_message NVARCHAR(500) NOT NULL DEFAULT '', recovered BIT NOT NULL DEFAULT 0, execution_id BIGINT NULL, created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL);
IF OBJECT_ID('gb_recording_plan_execution','U') IS NOT NULL DROP TABLE gb_recording_plan_execution;
CREATE TABLE gb_recording_plan_execution (id BIGINT IDENTITY(1,1) PRIMARY KEY, plan_id BIGINT NULL, channel_id BIGINT NOT NULL, device_id NVARCHAR(20) NOT NULL DEFAULT '', action NVARCHAR(32) NOT NULL, trigger_source NVARCHAR(32) NOT NULL, stage NVARCHAR(32) NOT NULL DEFAULT '', attempt INT NOT NULL DEFAULT 1, result NVARCHAR(24) NOT NULL, reason_code NVARCHAR(64) NOT NULL DEFAULT '', reason_message NVARCHAR(500) NOT NULL DEFAULT '', stream_id NVARCHAR(64) NOT NULL DEFAULT '', node_id NVARCHAR(64) NOT NULL DEFAULT '', recording_session_id BIGINT NULL, generation BIGINT NOT NULL DEFAULT 0, started_at DATETIME2(3) NOT NULL, ended_at DATETIME2(3) NULL, duration_ms BIGINT NOT NULL DEFAULT 0, created_at DATETIME2(3) NOT NULL);
IF OBJECT_ID('gb_recording_plan_channel_state','U') IS NOT NULL DROP TABLE gb_recording_plan_channel_state;
CREATE TABLE gb_recording_plan_channel_state (channel_id BIGINT PRIMARY KEY, plan_id BIGINT NULL, plan_version BIGINT NOT NULL DEFAULT 0, desired_state NVARCHAR(24) NOT NULL, actual_state NVARCHAR(32) NOT NULL, reason_code NVARCHAR(64) NOT NULL DEFAULT '', reason_message NVARCHAR(500) NOT NULL DEFAULT '', next_transition_at DATETIME2(3), next_retry_at DATETIME2(3), reconcile_at DATETIME2(3) NOT NULL, attempt_count INT NOT NULL DEFAULT 0, generation BIGINT NOT NULL DEFAULT 0, stream_id NVARCHAR(64) NOT NULL DEFAULT '', recording_session_id BIGINT, node_id NVARCHAR(64) NOT NULL DEFAULT '', last_media_at DATETIME2(3), last_success_at DATETIME2(3), lease_owner NVARCHAR(128) NOT NULL DEFAULT '', lease_until DATETIME2(3), state_version BIGINT NOT NULL DEFAULT 0, created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL);
CREATE INDEX idx_recording_plan_state_reconcile ON gb_recording_plan_channel_state(reconcile_at,channel_id);
IF OBJECT_ID('gb_recording_plan_binding','U') IS NOT NULL DROP TABLE gb_recording_plan_binding;
CREATE TABLE gb_recording_plan_binding (id BIGINT IDENTITY(1,1) PRIMARY KEY, plan_id BIGINT NOT NULL, channel_id BIGINT NOT NULL, owner_dept_id BIGINT NOT NULL, assigned_by BIGINT NOT NULL DEFAULT 0, assigned_at DATETIME2(3) NOT NULL, created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL, CONSTRAINT uk_recording_plan_binding_channel UNIQUE(channel_id));
IF OBJECT_ID('gb_recording_plan_period','U') IS NOT NULL DROP TABLE gb_recording_plan_period;
CREATE TABLE gb_recording_plan_period (id BIGINT IDENTITY(1,1) PRIMARY KEY, plan_id BIGINT NOT NULL, weekday SMALLINT NOT NULL, start_slot SMALLINT NOT NULL, end_slot SMALLINT NOT NULL, created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL);
IF OBJECT_ID('gb_recording_plan','U') IS NOT NULL DROP TABLE gb_recording_plan;
CREATE TABLE gb_recording_plan (id BIGINT IDENTITY(1,1) PRIMARY KEY, name NVARCHAR(128) NOT NULL, description NVARCHAR(500) NOT NULL DEFAULT '', status SMALLINT NOT NULL DEFAULT 1, version BIGINT NOT NULL DEFAULT 1, owner_dept_id BIGINT NOT NULL, created_by BIGINT NOT NULL DEFAULT 0, updated_by BIGINT NOT NULL DEFAULT 0, created_at DATETIME2(3) NOT NULL, updated_at DATETIME2(3) NOT NULL, deleted_at DATETIME2(3));

IF OBJECT_ID('gb_channel_favorite_item','U') IS NOT NULL DROP TABLE [gb_channel_favorite_item];
CREATE TABLE [gb_channel_favorite_item] ([id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY, [group_id] BIGINT NOT NULL, [device_code] NVARCHAR(64) NOT NULL, [channel_code] NVARCHAR(64) NOT NULL, [device_name] NVARCHAR(255) NOT NULL DEFAULT '', [channel_name] NVARCHAR(255) NOT NULL DEFAULT '', [created_at] DATETIME2 NOT NULL, [updated_at] DATETIME2 NOT NULL);
CREATE UNIQUE INDEX [uk_gb_channel_favorite_item_code] ON [gb_channel_favorite_item] ([group_id],[device_code],[channel_code]);
CREATE INDEX [idx_gb_channel_favorite_item_group] ON [gb_channel_favorite_item] ([group_id]);
IF OBJECT_ID('gb_channel_favorite_group','U') IS NOT NULL DROP TABLE [gb_channel_favorite_group];
CREATE TABLE [gb_channel_favorite_group] ([id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY, [owner_user_id] BIGINT NOT NULL, [name] NVARCHAR(64) NOT NULL, [created_at] DATETIME2 NOT NULL, [updated_at] DATETIME2 NOT NULL);
CREATE UNIQUE INDEX [uk_gb_channel_favorite_group_owner_name] ON [gb_channel_favorite_group] ([owner_user_id],[name]);
CREATE INDEX [idx_gb_channel_favorite_group_owner] ON [gb_channel_favorite_group] ([owner_user_id]);
IF OBJECT_ID('gb_custom_group_device', 'U') IS NOT NULL DROP TABLE [gb_custom_group_device];
CREATE TABLE [gb_custom_group_device] (
    [id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
    [group_id] BIGINT NOT NULL, [device_id] BIGINT NOT NULL,
    [created_by] BIGINT NOT NULL, [created_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [uk_custom_group_device] UNIQUE ([group_id], [device_id])
);
CREATE INDEX [idx_custom_group_device_group] ON [gb_custom_group_device] ([group_id]);
CREATE INDEX [idx_custom_group_device_device] ON [gb_custom_group_device] ([device_id]);

IF OBJECT_ID('gb_custom_group', 'U') IS NOT NULL DROP TABLE [gb_custom_group];
CREATE TABLE [gb_custom_group] (
    [id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,
    [owner_dept_id] BIGINT NOT NULL, [parent_id] BIGINT NOT NULL DEFAULT 0,
    [path] NVARCHAR(1024) NOT NULL, [depth] SMALLINT NOT NULL DEFAULT 0,
    [name] NVARCHAR(64) NOT NULL, [created_by] BIGINT NOT NULL,
    [created_at] DATETIME2(3) NOT NULL, [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [uk_custom_group_sibling_name] UNIQUE ([owner_dept_id], [parent_id], [name])
);
CREATE INDEX [idx_custom_group_parent] ON [gb_custom_group] ([parent_id]);
CREATE INDEX [idx_custom_group_dept_path] ON [gb_custom_group] ([owner_dept_id], [path]);

-- Table structure for demo_students
IF OBJECT_ID('demo_students', 'U') IS NOT NULL DROP TABLE [demo_students];
CREATE TABLE [demo_students] (
    [student_id] BIGINT IDENTITY(1,1) NOT NULL,
    [student_name] NVARCHAR(50) NOT NULL,
    [age] INT NOT NULL DEFAULT 18,
    [gender] NVARCHAR(50) NOT NULL DEFAULT '',
    [class_name] NVARCHAR(20) NOT NULL,
    [admission_date] DATETIME NOT NULL,
    [email] NVARCHAR(100),
    [phone] NVARCHAR(20),
    [address] NVARCHAR(MAX),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT DEFAULT 0,
    PRIMARY KEY ([student_id])
);


-- Records of demo_students
-- Table structure for demo_teacher
IF OBJECT_ID('demo_teacher', 'U') IS NOT NULL DROP TABLE [demo_teacher];
CREATE TABLE [demo_teacher] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [name] NVARCHAR(50) NOT NULL,
    [employee_id] NVARCHAR(20),
    [gender] TINYINT DEFAULT 0,
    [phone] NVARCHAR(20),
    [email] NVARCHAR(100),
    [subject] NVARCHAR(50),
    [title] NVARCHAR(50),
    [status] TINYINT DEFAULT 1,
    [hire_date] DATE,
    [birth_date] DATE,
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT DEFAULT 0,
    PRIMARY KEY ([id])
);


-- Records of demo_teacher
-- Table structure for example
IF OBJECT_ID('example', 'U') IS NOT NULL DROP TABLE [example];
CREATE TABLE [example] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [name] NVARCHAR(255) NOT NULL,
    [description] NVARCHAR(255),
    [created_at] DATETIME NOT NULL,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] INT,
    PRIMARY KEY ([id])
);


-- Records of example
SET IDENTITY_INSERT [example] ON;
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1, '项目管理系统', '用于管理项目进度和任务分配的系统', '2024-01-15 09:30:00', '2024-01-20 14:25:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (2, '客户关系管理', '帮助企业维护客户关系的软件平台', '2024-01-16 10:15:00', '2024-01-22 11:40:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (3, '财务分析工具', '提供财务报表和数据分析功能', '2024-01-17 14:20:00', '2024-01-25 16:30:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (4, '库存管理系统', '实时跟踪和管理库存水平', '2024-01-18 08:45:00', '2024-01-26 09:15:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (5, '人力资源平台', '员工信息管理和招聘流程优化', '2024-01-19 11:30:00', '2024-01-27 13:20:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (6, '在线学习系统', '提供课程管理和在线学习功能', '2024-01-20 15:10:00', '2024-01-28 17:05:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (7, '营销自动化', '自动化营销活动和客户跟进', '2024-01-21 09:00:00', '2024-01-29 10:45:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (8, '数据可视化', '将数据转化为直观的图表和报告', '2024-01-22 13:25:00', '2024-01-30 15:30:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (9, '移动应用开发', '跨平台移动应用开发框架', '2024-01-23 16:40:00', '2024-01-31 18:20:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (10, '云存储服务', '安全可靠的云端文件存储解决方案', '2024-01-24 10:50:00', '2024-02-01 12:35:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (11, '智能客服系统', '基于AI的智能客户服务助手', '2024-01-25 14:15:00', '2024-02-02 16:10:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (12, '供应链管理', '优化供应链流程和物流管理', '2024-01-26 08:30:00', '2024-02-03 10:25:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (13, '质量控制系统', '产品质量检测和流程监控', '2024-01-27 11:45:00', '2024-02-04 13:40:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (14, '企业门户网站', '企业信息发布和员工协作平台', '2024-01-28 15:20:00', '2024-02-05 17:15:00', NULL, 1);
INSERT INTO [example] ([id], [name], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (15, '数据分析平台', '大数据处理和分析工具集', '2024-01-29 09:35:00', '2024-02-06 11:30:00', NULL, 1);
-- Table structure for sys_affix
SET IDENTITY_INSERT [example] OFF;
IF OBJECT_ID('sys_affix', 'U') IS NOT NULL DROP TABLE [sys_affix];
CREATE TABLE [sys_affix] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [name] NVARCHAR(255),
    [path] NVARCHAR(255),
    [url] NVARCHAR(255),
    [file_md5] NVARCHAR(32) DEFAULT '',
    [size] INT,
    [ftype] NVARCHAR(100),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] INT,
    [suffix] NVARCHAR(100),
    [thumbnail_path] NVARCHAR(255),
    [thumbnail_name] NVARCHAR(255),
    [thumbnail_url] NVARCHAR(255),
    PRIMARY KEY ([id])
);


-- Records of sys_affix
-- Table structure for sys_affix_chunk
IF OBJECT_ID('sys_affix_chunk', 'U') IS NOT NULL DROP TABLE [sys_affix_chunk];
CREATE TABLE [sys_affix_chunk] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [upload_id] NVARCHAR(64) NOT NULL,
    [file_md5] NVARCHAR(32) NOT NULL,
    [file_name] NVARCHAR(255),
    [file_size] BIGINT,
    [chunk_size] INT,
    [total_chunks] INT,
    [chunk_index] INT NOT NULL,
    [chunk_path] NVARCHAR(255),
    [status] TINYINT DEFAULT 0,
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] INT,
    PRIMARY KEY ([id])
);


-- Records of sys_affix_chunk
-- Table structure for sys_api
IF OBJECT_ID('sys_api', 'U') IS NOT NULL DROP TABLE [sys_api];
CREATE TABLE [sys_api] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [title] NVARCHAR(255),
    [path] NVARCHAR(255),
    [method] NVARCHAR(32),
    [api_group] NVARCHAR(255),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT,
    PRIMARY KEY ([id])
);


-- Records of sys_api
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1, '用户登录', '/api/login', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (2, '刷新Token', '/api/refreshToken', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (3, '生成验证码ID', '/api/captcha/id', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (4, '获取验证码图片', '/api/captcha/image', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (5, '用户登出', '/api/users/logout', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (6, '获取当前用户信息', '/api/users/profile', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (7, '根据ID获取用户信息', '/api/users/:id', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (8, '用户列表', '/api/users/list', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (9, '新增用户', '/api/users/add', 'POST', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (10, '更新用户信息', '/api/users/edit', 'PUT', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (11, '删除用户', '/api/users/delete', 'DELETE', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (12, '获取用户权限菜单', '/api/sysMenu/getRouters', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (13, '获取完整菜单列表', '/api/sysMenu/getMenuList', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (14, '根据ID获取菜单信息', '/api/sysMenu/:id', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (15, '新增菜单', '/api/sysMenu/add', 'POST', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (16, '更新菜单', '/api/sysMenu/edit', 'PUT', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (17, '删除菜单', '/api/sysMenu/delete', 'DELETE', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (18, '获取部门列表', '/api/sysDepartment/getDivision', 'GET', '部门管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (19, '获取所有角色数据', '/api/sysRole/getRoles', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (20, '根据角色ID获取角色菜单权限', '/api/sysRole/getUserPermission/:roleId', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (21, '添加角色的菜单权限', '/api/sysRole/addRoleMenu', 'POST', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (22, '角色分页列表', '/api/sysRole/list', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (23, '根据ID获取角色信息', '/api/sysRole/:id', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (24, '新增角色', '/api/sysRole/add', 'POST', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (25, '更新角色', '/api/sysRole/edit', 'PUT', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (26, '删除角色', '/api/sysRole/delete', 'DELETE', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (27, '获取所有字典数据', '/api/sysDict/getAllDicts', 'GET', '字典管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (28, '根据字典编码获取字典', '/api/sysDict/getByCode/:code', 'GET', '字典管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (29, 'API列表', '/api/sysApi/list', 'GET', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (30, '根据ID获取API信息', '/api/sysApi/:id', 'GET', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (31, '新增API', '/api/sysApi/add', 'POST', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (32, '更新API', '/api/sysApi/edit', 'PUT', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (33, '删除API', '/api/sysApi/delete', 'DELETE', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (35, '根据菜单ID获取API的ID集合', '/api/sysMenu/apis/:id', 'GET', '菜单管理', '2025-09-04 17:25:14', '2025-09-04 17:25:14', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (36, '设置菜单API权限', '/api/sysMenu/setApis', 'POST', '菜单管理', '2025-09-04 17:26:04', '2025-09-04 17:26:04', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (37, '根据ID获取部门信息', '/api/sysDepartment/:id', 'GET', '部门管理', '2025-09-12 14:46:42', '2025-09-12 14:46:42', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (38, '新增部门', '/api/sysDepartment/add', 'POST', '部门管理', '2025-09-12 14:47:27', '2025-09-12 14:47:27', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (39, '更新部门', '/api/sysDepartment/edit', 'PUT', '部门管理', '2025-09-12 14:48:15', '2025-09-12 14:48:27', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (40, '删除部门', '/api/sysDepartment/delete', 'DELETE', '部门管理', '2025-09-12 14:49:15', '2025-09-12 14:49:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (41, '字典分页列表', '/api/sysDict/list', 'GET', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (42, '根据ID获取字典信息', '/api/sysDict/:id', 'GET', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (43, '新增字典', '/api/sysDict/add', 'POST', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (44, '更新字典', '/api/sysDict/edit', 'PUT', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (45, '删除字典', '/api/sysDict/delete', 'DELETE', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (46, '字典项列表', '/api/sysDictItem/list', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (47, '根据ID获取字典项信息', '/api/sysDictItem/:id', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (48, '根据字典ID获取字典项列表', '/api/sysDictItem/getByDictId/:dictId', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (49, '根据字典编码获取字典项列表', '/api/sysDictItem/getByDictCode/:dictCode', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (50, '新增字典项', '/api/sysDictItem/add', 'POST', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (51, '更新字典项', '/api/sysDictItem/edit', 'PUT', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (52, '删除字典项', '/api/sysDictItem/delete', 'DELETE', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (53, '修改用户密码、手机号及邮箱', '/api/users/updateAccount', 'PUT', '用户管理', '2025-09-18 18:11:01', '2025-09-18 18:11:01', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (54, '头像上传', '/api/users/uploadAvatar', 'POST', '用户管理', '2025-09-24 17:01:05', '2025-09-24 17:01:05', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (55, '上传文件', '/api/sysAffix/upload', 'POST', '文件管理', '2025-09-25 15:51:04', '2025-09-25 15:51:04', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (56, '删除文件', '/api/sysAffix/delete', 'DELETE', '文件管理', '2025-09-25 15:51:38', '2025-09-25 15:51:38', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (57, '修改文件名', '/api/sysAffix/updateName', 'PUT', '文件管理', '2025-09-25 15:52:31', '2025-09-25 15:52:31', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (58, '文件列表', '/api/sysAffix/list', 'GET', '文件管理', '2025-09-25 15:54:03', '2025-09-25 15:54:03', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (59, '获取文件详情', '/api/sysAffix/:id', 'GET', '文件管理', '2025-09-25 15:54:55', '2025-09-25 15:54:55', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (60, '下载文件', '/api/sysAffix/download/:id', 'GET', '文件管理', '2025-09-25 15:56:15', '2025-09-25 15:58:06', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (61, '设置数据权限', '/api/sysRole/dataScope', 'PUT', '角色管理', '2025-09-26 17:04:15', '2025-09-26 17:04:15', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (62, '读取系统配置', '/api/config/get', 'GET', '系统配置', '2025-10-09 16:21:29', '2025-10-09 16:21:29', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (63, '修改系统配置', '/api/config/update', 'PUT', '系统配置', '2025-10-09 16:21:59', '2025-10-09 16:22:09', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (64, '查看内存缓存', '/api/config/viewCache', 'GET', '系统配置', '2025-10-10 17:41:33', '2025-10-10 17:41:33', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (65, '列表查询', '/api/plugins/example/list', 'GET', '插件示例', '2025-10-14 10:54:47', '2025-10-14 10:54:47', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (66, '新增', '/api/plugins/example/add', 'POST', '插件示例', '2025-10-14 10:56:43', '2025-10-14 10:56:43', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (67, '修改', '/api/plugins/example/edit', 'PUT', '插件示例', '2025-10-14 10:57:10', '2025-10-14 10:57:17', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (68, '删除', '/api/plugins/example/delete', 'DELETE', '插件示例', '2025-10-14 10:58:03', '2025-10-14 10:58:03', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (69, '查询单条数据', '/api/plugins/example/:id', 'GET', '插件示例', '2025-10-14 10:59:33', '2025-10-14 10:59:33', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (70, '日志列表', '/api/sysOperationLog/list', 'GET', '日志管理', '2025-10-20 10:10:58', '2025-10-20 10:10:58', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (72, '日志删除', '/api/sysOperationLog/delete', 'DELETE', '日志管理', '2025-10-20 10:13:19', '2025-10-20 10:13:19', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (73, '日志导出', '/api/sysOperationLog/export', 'GET', '日志管理', '2025-10-20 10:14:11', '2025-10-20 10:14:11', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (74, '导出菜单', '/api/sysMenu/export', 'GET', '菜单管理', '2025-10-20 17:17:07', '2025-10-20 17:17:07', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (75, '导入菜单', '/api/sysMenu/import', 'POST', '菜单管理', '2025-10-21 11:30:34', '2025-10-24 08:59:44', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (89, '修改用户基本信息', '/api/users/updateBasicInfo', 'PUT', '用户管理', '2025-10-31 09:05:00', '2025-10-31 09:05:00', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (105, '生成代码文件', '/api/codegen/generate', 'POST', '代码生成', '2025-11-07 15:32:53', '2025-11-07 15:32:53', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (106, '获取表的字段信息', '/api/codegen/columns', 'GET', '代码生成', '2025-11-07 15:33:52', '2025-11-07 15:33:52', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (187, '获取数据库列表', '/api/codegen/databases', 'GET', '代码生成', '2025-11-17 15:12:26', '2025-11-17 15:12:26', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (188, '获取指定数据库中的表集合', '/api/codegen/tables', 'GET', '代码生成', '2025-11-17 15:13:38', '2025-11-17 15:13:38', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (189, '代码预览', '/api/codegen/preview', 'GET', '代码生成', '2025-11-17 15:14:25', '2025-11-17 15:14:25', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (190, '代码生成配置列表', '/api/sysGen/list', 'GET', '代码生成', '2025-11-17 15:15:20', '2025-11-17 15:15:20', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (191, ' 批量创建代码生成配置', '/api/sysGen/batchInsert', 'POST', '代码生成', '2025-11-17 15:22:46', '2025-11-17 15:22:46', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (192, '获取代码生成配置详情', '/api/sysGen/:id', 'GET', '代码生成', '2025-11-17 15:23:29', '2025-11-17 15:23:29', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (193, '更新代码生成配置和字段信息', '/api/sysGen/update', 'PUT', '代码生成', '2025-11-17 15:24:41', '2025-11-17 15:24:41', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (194, '删除代码生成配置和字段信息', '/api/sysGen/:id', 'DELETE', '代码生成', '2025-11-17 15:26:44', '2025-11-17 15:26:44', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (195, '刷新代码生成配置的字段信息', '/api/sysGen/refreshFields', 'PUT', '代码生成', '2025-11-17 15:27:33', '2025-11-17 15:27:33', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (196, '生成菜单', '/api/codegen/insertmenuandapi', 'POST', '代码生成', '2025-11-26 15:12:56', '2025-11-26 15:12:56', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (197, '批量删除', '/api/sysMenu/batchDelete', 'DELETE', '菜单管理', '2025-12-05 17:48:52', '2025-12-05 17:48:52', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (198, '获取插件列表', '/api/pluginsmanager/exports', 'GET', '插件管理', '2025-12-08 16:38:26', '2025-12-08 16:38:26', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (199, '导出插件', '/api/pluginsmanager/export', 'POST', '插件管理', '2025-12-08 16:39:19', '2025-12-08 16:44:36', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (200, '导入插件', '/api/pluginsmanager/import', 'POST', '插件管理', '2025-12-08 16:47:11', '2025-12-08 16:47:11', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (201, '卸载插件', '/api/pluginsmanager/uninstall', 'DELETE', '插件管理', '2025-12-08 16:48:07', '2025-12-08 16:48:07', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (203, '定时任务列表', '/api/sysJobs/list', 'GET', '任务调度', '2026-02-11 11:56:54', '2026-02-11 11:56:54', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (204, '定时任务获取所有执行器列表', '/api/sysJobs/executors', 'GET', '任务调度', '2026-02-12 17:57:47', '2026-02-12 17:57:47', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (205, '定时任务新增', '/api/sysJobs/add', 'POST', '任务调度', '2026-02-11 11:57:33', '2026-02-11 11:57:33', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (206, '定时任务编辑', '/api/sysJobs/edit', 'PUT', '任务调度', '2026-02-11 11:57:58', '2026-02-11 11:57:58', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (207, '定时任务获取数据', '/api/sysJobs/:id', 'GET', '任务调度', '2026-02-11 11:59:54', '2026-02-11 11:59:54', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (208, '定时任务设置任务状态', '/api/sysJobs/setStatus', 'PUT', '任务调度', '2026-02-12 17:56:33', '2026-02-12 17:56:33', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (209, '定时任务删除', '/api/sysJobs/delete', 'DELETE', '任务调度', '2026-02-11 11:59:05', '2026-02-11 11:59:05', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (210, '定时任务立即执行任务', '/api/sysJobs/executeNow', 'POST', '任务调度', '2026-02-12 17:57:07', '2026-02-12 17:57:07', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (211, '定时任务日志', '/api/sysJobResults/list', 'GET', '任务调度', '2026-02-11 12:00:54', '2026-02-11 12:00:54', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (212, '定时任务日志删除', '/api/sysJobResults/delete', 'DELETE', '任务调度', '2026-02-11 12:01:22', '2026-02-11 12:01:22', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (213, '分片上传初始化', '/api/sysAffix/chunk/init', 'POST', '文件管理', '2026-04-09 15:12:48', '2026-04-09 15:12:48', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (214, '分片上传上传分片', '/api/sysAffix/chunk/upload', 'POST', '文件管理', '2026-04-09 15:37:44', '2026-04-09 15:37:44', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (215, '分片上传合并分片', '/api/sysAffix/chunk/merge', 'POST', '文件管理', '2026-04-09 15:39:26', '2026-04-09 15:39:26', NULL, 1);
INSERT INTO [sys_api] ([id], [title], [path], [method], [api_group], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (216, '分片上传取消上传', '/api/sysAffix/chunk/cancel', 'DELETE', '文件管理', '2026-04-09 15:43:38', '2026-04-09 15:43:38', NULL, 1);
-- Table structure for sys_casbin_rule
SET IDENTITY_INSERT [sys_api] OFF;
IF OBJECT_ID('sys_casbin_rule', 'U') IS NOT NULL DROP TABLE [sys_casbin_rule];
CREATE TABLE [sys_casbin_rule] (
    [id] INT IDENTITY(1,1) NOT NULL,
    [ptype] NVARCHAR(100),
    [v0] NVARCHAR(100),
    [v1] NVARCHAR(100),
    [v2] NVARCHAR(100),
    [v3] NVARCHAR(100),
    [v4] NVARCHAR(100),
    [v5] NVARCHAR(100),
    PRIMARY KEY ([id])
);


-- Records of sys_casbin_rule
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (6266, 'g', 'user_1', 'role_1', '*', '', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4189, 'g', 'user_4', 'role_2', '*', '', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7386, 'p', 'role_1', '/api/codegen/generate', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7425, 'p', 'role_1', '/api/codegen/insertmenuandapi', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7443, 'p', 'role_1', '/api/codegen/preview', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7412, 'p', 'role_1', '/api/codegen/tables', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7384, 'p', 'role_1', '/api/config/get', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7452, 'p', 'role_1', '/api/config/update', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7442, 'p', 'role_1', '/api/config/viewCache', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7469, 'p', 'role_1', '/api/plugins/example/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7463, 'p', 'role_1', '/api/plugins/example/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7464, 'p', 'role_1', '/api/plugins/example/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7403, 'p', 'role_1', '/api/plugins/example/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7421, 'p', 'role_1', '/api/plugins/example/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7431, 'p', 'role_1', '/api/pluginsmanager/export', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7426, 'p', 'role_1', '/api/pluginsmanager/exports', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7459, 'p', 'role_1', '/api/pluginsmanager/import', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7454, 'p', 'role_1', '/api/pluginsmanager/uninstall', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7420, 'p', 'role_1', '/api/sysAffix/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7410, 'p', 'role_1', '/api/sysAffix/download/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7419, 'p', 'role_1', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7402, 'p', 'role_1', '/api/sysAffix/updateName', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7462, 'p', 'role_1', '/api/sysAffix/upload', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7423, 'p', 'role_1', '/api/sysApi/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7461, 'p', 'role_1', '/api/sysApi/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7456, 'p', 'role_1', '/api/sysApi/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7392, 'p', 'role_1', '/api/sysApi/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7467, 'p', 'role_1', '/api/sysApi/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7432, 'p', 'role_1', '/api/sysDepartment/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7417, 'p', 'role_1', '/api/sysDepartment/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7446, 'p', 'role_1', '/api/sysDepartment/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7393, 'p', 'role_1', '/api/sysDepartment/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7429, 'p', 'role_1', '/api/sysDepartment/getDivision', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7447, 'p', 'role_1', '/api/sysDict/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7468, 'p', 'role_1', '/api/sysDict/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7418, 'p', 'role_1', '/api/sysDict/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7455, 'p', 'role_1', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7449, 'p', 'role_1', '/api/sysDict/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7457, 'p', 'role_1', '/api/sysDictItem/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7409, 'p', 'role_1', '/api/sysDictItem/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7401, 'p', 'role_1', '/api/sysDictItem/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7433, 'p', 'role_1', '/api/sysDictItem/getByDictId/:dictId', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7414, 'p', 'role_1', '/api/sysGen/:id', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7413, 'p', 'role_1', '/api/sysGen/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7453, 'p', 'role_1', '/api/sysGen/batchInsert', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7411, 'p', 'role_1', '/api/sysGen/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7424, 'p', 'role_1', '/api/sysGen/refreshFields', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7382, 'p', 'role_1', '/api/sysGen/update', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7441, 'p', 'role_1', '/api/sysMenu/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7391, 'p', 'role_1', '/api/sysMenu/apis/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7390, 'p', 'role_1', '/api/sysMenu/batchDelete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7451, 'p', 'role_1', '/api/sysMenu/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7395, 'p', 'role_1', '/api/sysMenu/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7435, 'p', 'role_1', '/api/sysMenu/export', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7445, 'p', 'role_1', '/api/sysMenu/getMenuList', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7399, 'p', 'role_1', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7458, 'p', 'role_1', '/api/sysMenu/import', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7396, 'p', 'role_1', '/api/sysMenu/setApis', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7428, 'p', 'role_1', '/api/sysOperationLog/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7397, 'p', 'role_1', '/api/sysOperationLog/export', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7383, 'p', 'role_1', '/api/sysOperationLog/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7450, 'p', 'role_1', '/api/sysRole/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7408, 'p', 'role_1', '/api/sysRole/addRoleMenu', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7434, 'p', 'role_1', '/api/sysRole/dataScope', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7389, 'p', 'role_1', '/api/sysRole/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7400, 'p', 'role_1', '/api/sysRole/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7415, 'p', 'role_1', '/api/sysRole/getRoles', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7416, 'p', 'role_1', '/api/sysRole/getUserPermission/:roleId', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7444, 'p', 'role_1', '/api/users/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7387, 'p', 'role_1', '/api/users/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7388, 'p', 'role_1', '/api/users/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7460, 'p', 'role_1', '/api/users/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7466, 'p', 'role_1', '/api/users/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7440, 'p', 'role_1', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7427, 'p', 'role_1', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7394, 'p', 'role_1', '/api/users/updateAccount', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7465, 'p', 'role_1', '/api/users/updateBasicInfo', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7448, 'p', 'role_1', '/api/users/uploadAvatar', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4148, 'p', 'role_10', '/api/config/get', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4155, 'p', 'role_10', '/api/config/update', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4162, 'p', 'role_10', '/api/config/viewCache', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4168, 'p', 'role_10', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4125, 'p', 'role_10', '/api/sysApi/*', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4175, 'p', 'role_10', '/api/sysApi/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4179, 'p', 'role_10', '/api/sysApi/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4153, 'p', 'role_10', '/api/sysApi/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4158, 'p', 'role_10', '/api/sysApi/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4176, 'p', 'role_10', '/api/sysDepartment/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4133, 'p', 'role_10', '/api/sysDepartment/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4167, 'p', 'role_10', '/api/sysDepartment/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4122, 'p', 'role_10', '/api/sysDepartment/getDivision', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4136, 'p', 'role_10', '/api/sysDict/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4144, 'p', 'role_10', '/api/sysDict/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4137, 'p', 'role_10', '/api/sysDict/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4184, 'p', 'role_10', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4139, 'p', 'role_10', '/api/sysDict/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4146, 'p', 'role_10', '/api/sysDictItem/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4154, 'p', 'role_10', '/api/sysDictItem/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4147, 'p', 'role_10', '/api/sysDictItem/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4141, 'p', 'role_10', '/api/sysDictItem/getByDictId/*', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4174, 'p', 'role_10', '/api/sysMenu/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4145, 'p', 'role_10', '/api/sysMenu/apis/*', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4124, 'p', 'role_10', '/api/sysMenu/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4123, 'p', 'role_10', '/api/sysMenu/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4172, 'p', 'role_10', '/api/sysMenu/export', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4177, 'p', 'role_10', '/api/sysMenu/getMenuList', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4150, 'p', 'role_10', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4126, 'p', 'role_10', '/api/sysMenu/import', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4161, 'p', 'role_10', '/api/sysMenu/setApis', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4134, 'p', 'role_10', '/api/sysOperationLog/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4156, 'p', 'role_10', '/api/sysOperationLog/export', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4131, 'p', 'role_10', '/api/sysOperationLog/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4128, 'p', 'role_10', '/api/sysRole/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4135, 'p', 'role_10', '/api/sysRole/addRoleMenu', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4151, 'p', 'role_10', '/api/sysRole/dataScope', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4140, 'p', 'role_10', '/api/sysRole/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4166, 'p', 'role_10', '/api/sysRole/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4130, 'p', 'role_10', '/api/sysRole/getRoles', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4132, 'p', 'role_10', '/api/sysRole/getUserPermission/*', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4127, 'p', 'role_10', '/api/users/*', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4159, 'p', 'role_10', '/api/users/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4160, 'p', 'role_10', '/api/users/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4178, 'p', 'role_10', '/api/users/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4143, 'p', 'role_10', '/api/users/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4170, 'p', 'role_10', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4183, 'p', 'role_10', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4138, 'p', 'role_10', '/api/users/updateAccount', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (4171, 'p', 'role_10', '/api/users/uploadAvatar', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7481, 'p', 'role_2', '/api/codegen/generate', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7540, 'p', 'role_2', '/api/codegen/insertmenuandapi', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7539, 'p', 'role_2', '/api/codegen/preview', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7497, 'p', 'role_2', '/api/codegen/tables', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7550, 'p', 'role_2', '/api/config/get', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7553, 'p', 'role_2', '/api/config/update', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7472, 'p', 'role_2', '/api/config/viewCache', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7532, 'p', 'role_2', '/api/plugins/example/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7473, 'p', 'role_2', '/api/plugins/example/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7512, 'p', 'role_2', '/api/plugins/example/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7549, 'p', 'role_2', '/api/plugins/example/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7541, 'p', 'role_2', '/api/plugins/example/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7524, 'p', 'role_2', '/api/pluginsmanager/export', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7523, 'p', 'role_2', '/api/pluginsmanager/exports', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7486, 'p', 'role_2', '/api/pluginsmanager/import', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7525, 'p', 'role_2', '/api/pluginsmanager/uninstall', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7480, 'p', 'role_2', '/api/sysAffix/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7506, 'p', 'role_2', '/api/sysAffix/download/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7552, 'p', 'role_2', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7521, 'p', 'role_2', '/api/sysAffix/updateName', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7556, 'p', 'role_2', '/api/sysAffix/upload', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7529, 'p', 'role_2', '/api/sysApi/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7518, 'p', 'role_2', '/api/sysApi/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7479, 'p', 'role_2', '/api/sysApi/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7484, 'p', 'role_2', '/api/sysApi/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7476, 'p', 'role_2', '/api/sysApi/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7500, 'p', 'role_2', '/api/sysDepartment/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7530, 'p', 'role_2', '/api/sysDepartment/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7519, 'p', 'role_2', '/api/sysDepartment/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7560, 'p', 'role_2', '/api/sysDepartment/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7510, 'p', 'role_2', '/api/sysDepartment/getDivision', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7547, 'p', 'role_2', '/api/sysDict/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7520, 'p', 'role_2', '/api/sysDict/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7531, 'p', 'role_2', '/api/sysDict/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7534, 'p', 'role_2', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7475, 'p', 'role_2', '/api/sysDict/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7502, 'p', 'role_2', '/api/sysDictItem/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7548, 'p', 'role_2', '/api/sysDictItem/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7495, 'p', 'role_2', '/api/sysDictItem/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7501, 'p', 'role_2', '/api/sysDictItem/getByDictId/:dictId', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7522, 'p', 'role_2', '/api/sysGen/:id', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7498, 'p', 'role_2', '/api/sysGen/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7538, 'p', 'role_2', '/api/sysGen/batchInsert', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7490, 'p', 'role_2', '/api/sysGen/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7517, 'p', 'role_2', '/api/sysGen/refreshFields', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7558, 'p', 'role_2', '/api/sysGen/update', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7505, 'p', 'role_2', '/api/sysMenu/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7491, 'p', 'role_2', '/api/sysMenu/apis/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7535, 'p', 'role_2', '/api/sysMenu/batchDelete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7478, 'p', 'role_2', '/api/sysMenu/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7483, 'p', 'role_2', '/api/sysMenu/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7485, 'p', 'role_2', '/api/sysMenu/export', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7559, 'p', 'role_2', '/api/sysMenu/getMenuList', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7527, 'p', 'role_2', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7507, 'p', 'role_2', '/api/sysMenu/import', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7546, 'p', 'role_2', '/api/sysMenu/setApis', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7533, 'p', 'role_2', '/api/sysOperationLog/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7513, 'p', 'role_2', '/api/sysOperationLog/export', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7544, 'p', 'role_2', '/api/sysOperationLog/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7511, 'p', 'role_2', '/api/sysRole/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7487, 'p', 'role_2', '/api/sysRole/addRoleMenu', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7471, 'p', 'role_2', '/api/sysRole/dataScope', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7528, 'p', 'role_2', '/api/sysRole/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7482, 'p', 'role_2', '/api/sysRole/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7551, 'p', 'role_2', '/api/sysRole/getRoles', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7477, 'p', 'role_2', '/api/sysRole/getUserPermission/:roleId', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7488, 'p', 'role_2', '/api/users/:id', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7493, 'p', 'role_2', '/api/users/add', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7494, 'p', 'role_2', '/api/users/delete', 'DELETE', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7545, 'p', 'role_2', '/api/users/edit', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7543, 'p', 'role_2', '/api/users/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7526, 'p', 'role_2', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7499, 'p', 'role_2', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7489, 'p', 'role_2', '/api/users/updateAccount', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7509, 'p', 'role_2', '/api/users/updateBasicInfo', 'PUT', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (7542, 'p', 'role_2', '/api/users/uploadAvatar', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (2966, 'p', 'role_4', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (2971, 'p', 'role_4', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (2970, 'p', 'role_4', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (2969, 'p', 'role_4', '/api/users/*', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (2967, 'p', 'role_4', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (2968, 'p', 'role_4', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO [sys_casbin_rule] ([id], [ptype], [v0], [v1], [v2], [v3], [v4], [v5]) VALUES (2965, 'p', 'role_4', '/api/users/uploadAvatar', 'POST', '*', '', '');
-- Table structure for sys_department
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- 登录审计事件与操作菜单 seed。
IF OBJECT_ID(N'sys_login_logs', N'U') IS NULL CREATE TABLE [sys_login_logs] ([id] BIGINT IDENTITY(1,1) NOT NULL PRIMARY KEY,[user_id] BIGINT NULL,[username] NVARCHAR(100) NOT NULL,[result] NVARCHAR(16) NOT NULL,[failure_reason] NVARCHAR(48) NULL,[ip] NVARCHAR(50) NOT NULL DEFAULT N'',[location] NVARCHAR(100) NOT NULL DEFAULT N'未知',[user_agent] NVARCHAR(500) NOT NULL DEFAULT N'',[browser] NVARCHAR(100) NOT NULL DEFAULT N'未知',[os] NVARCHAR(100) NOT NULL DEFAULT N'未知',[created_at] DATETIME2 NOT NULL,[updated_at] DATETIME2 NULL,[deleted_at] DATETIME2 NULL);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name=N'idx_login_logs_created_at' AND object_id=OBJECT_ID(N'sys_login_logs')) CREATE INDEX idx_login_logs_created_at ON sys_login_logs(created_at);
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id],[title],[path],[method],[api_group],[created_at],[updated_at],[deleted_at],[created_by]) VALUES (341,N'登录日志列表',N'/api/sysLoginLog/list',N'GET',N'日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(342,N'登录日志详情',N'/api/sysLoginLog/:id',N'GET',N'日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(343,N'删除登录日志',N'/api/sysLoginLog/delete',N'DELETE',N'日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(344,N'清空登录日志',N'/api/sysLoginLog/clear',N'POST',N'日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(345,N'解锁登录账号',N'/api/sysLoginLog/unlock',N'POST',N'日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[hide],[disable],[sort],[type],[permission],[icon],[created_at],[updated_at],[deleted_at],[created_by]) VALUES (140384,10,N'/system/login-log',N'SystemLoginLog',N'system/login-log/index',N'登录日志',0,0,1,2,N'system:login-log:list',N'lucide:FileClock',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[hide],[disable],[sort],[type],[permission],[icon],[created_at],[updated_at],[deleted_at],[created_by]) VALUES (140385,140384,N'',N'SystemLoginLogDelete',N'',N'删除登录日志',1,0,1,3,N'system:login-log:delete',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(140386,140384,N'',N'SystemLoginLogClear',N'',N'清空登录日志',1,0,2,3,N'system:login-log:clear',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(140387,140384,N'',N'SystemLoginLogUnlock',N'',N'解锁登录账号',1,0,3,3,N'system:login-log:unlock',N'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_menu] OFF;
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140384),(1,140385),(1,140386),(1,140387); INSERT INTO [sys_menu_api] ([menu_id],[api_id]) VALUES (140384,341),(140384,342),(140385,343),(140386,344),(140387,345);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id],[ptype],[v0],[v1],[v2],[v3],[v4],[v5]) VALUES (7808,N'p',N'role_1',N'/api/sysLoginLog/list',N'GET',N'*',N'',N''),(7809,N'p',N'role_1',N'/api/sysLoginLog/:id',N'GET',N'*',N'',N''),(7810,N'p',N'role_1',N'/api/sysLoginLog/delete',N'DELETE',N'*',N'',N''),(7811,N'p',N'role_1',N'/api/sysLoginLog/clear',N'POST',N'*',N'',N''),(7812,N'p',N'role_1',N'/api/sysLoginLog/unlock',N'POST',N'*',N'',N'');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- Playback schemes reuse the multi-screen page and expose one hidden permission.
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id],[title],[path],[method],[api_group],[created_at],[updated_at],[deleted_at],[created_by]) VALUES
(228,N'查询播放方案','/api/gb28181/playback-schemes','GET',N'GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(229,N'查看播放方案','/api/gb28181/playback-schemes/:id','GET',N'GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(230,N'创建播放方案','/api/gb28181/playback-schemes','POST',N'GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(231,N'重命名播放方案','/api/gb28181/playback-schemes/:id','PATCH',N'GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(232,N'覆盖播放方案','/api/gb28181/playback-schemes/:id/layout','PUT',N'GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(233,N'删除播放方案','/api/gb28181/playback-schemes/:id','DELETE',N'GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[hide],[type],[permission],[created_at],[updated_at],[created_by]) VALUES
(140362,140355,'','','',N'管理播放方案',1,3,'gb28181:playback-scheme:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
SET IDENTITY_INSERT [sys_menu] OFF;
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140362);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) VALUES
(140362,228),(140362,229),(140362,230),(140362,231),(140362,232),(140362,233);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id],[ptype],[v0],[v1],[v2],[v3],[v4],[v5]) VALUES
(7572,'p','role_1','/api/gb28181/playback-schemes','GET','*','',''),
(7573,'p','role_1','/api/gb28181/playback-schemes/:id','GET','*','',''),
(7574,'p','role_1','/api/gb28181/playback-schemes','POST','*','',''),
(7575,'p','role_1','/api/gb28181/playback-schemes/:id','PATCH','*','',''),
(7576,'p','role_1','/api/gb28181/playback-schemes/:id/layout','PUT','*','',''),
(7577,'p','role_1','/api/gb28181/playback-schemes/:id','DELETE','*','','');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- GB28181 cascade API/menu/Casbin seed for fresh SQL Server installs.
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(234,N'查看级联平台列表',N'/api/gb28181/cascade/platforms',N'GET',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(235,N'创建级联平台',N'/api/gb28181/cascade/platforms',N'POST',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(236,N'查看级联平台',N'/api/gb28181/cascade/platforms/:id',N'GET',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(237,N'修改级联平台',N'/api/gb28181/cascade/platforms/:id',N'PUT',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(238,N'删除级联平台',N'/api/gb28181/cascade/platforms/:id',N'DELETE',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(239,N'启停级联平台',N'/api/gb28181/cascade/platforms/:id/enabled',N'PUT',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(240,N'启用级联平台',N'/api/gb28181/cascade/platforms/:id/enable',N'POST',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(241,N'停用级联平台',N'/api/gb28181/cascade/platforms/:id/disable',N'POST',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(242,N'重连级联平台',N'/api/gb28181/cascade/platforms/:id/reconnect',N'POST',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(243,N'查看级联共享',N'/api/gb28181/cascade/platforms/:id/shares',N'GET',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(244,N'更新级联共享',N'/api/gb28181/cascade/platforms/:id/shares',N'PUT',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(245,N'共享级联通道',N'/api/gb28181/cascade/platforms/:id/channels/share',N'POST',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(246,N'取消级联通道共享',N'/api/gb28181/cascade/platforms/:id/channels/unshare',N'POST',N'GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) VALUES
(140370,0,N'/gb28181/cascade',N'gb28181-cascade',N'gb28181/cascade/index',N'国标级联',0,0,13,2,N'',N'lucide:GitBranch',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by) VALUES
(140363,140370,N'',N'',N'',N'查看国标级联',1,3,N'gb28181:cascade:view',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140364,140370,N'',N'',N'',N'管理国标级联',1,3,N'gb28181:cascade:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140365,140370,N'',N'',N'',N'启停国标级联',1,3,N'gb28181:cascade:enable',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140366,140370,N'',N'',N'',N'共享国标级联',1,3,N'gb28181:cascade:share',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140367,140370,N'',N'',N'',N'重连国标级联',1,3,N'gb28181:cascade:reconnect',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
SET IDENTITY_INSERT [sys_menu] OFF;
INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,140370),(1,140363),(1,140364),(1,140365),(1,140366),(1,140367);
INSERT INTO sys_menu_api (menu_id,api_id) VALUES (140363,234),(140363,236),(140363,243),(140364,235),(140364,237),(140364,238),(140365,239),(140365,240),(140365,241),(140366,244),(140366,245),(140366,246),(140367,242);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5) VALUES
(7578,N'p',N'role_1',N'/api/gb28181/cascade/platforms',N'GET',N'*',N'',N''),(7579,N'p',N'role_1',N'/api/gb28181/cascade/platforms',N'POST',N'*',N'',N''),(7580,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id',N'GET',N'*',N'',N''),(7581,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id',N'PUT',N'*',N'',N''),(7582,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id',N'DELETE',N'*',N'',N''),(7583,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/enabled',N'PUT',N'*',N'',N''),(7584,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/enable',N'POST',N'*',N'',N''),(7585,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/disable',N'POST',N'*',N'',N''),(7586,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/reconnect',N'POST',N'*',N'',N''),(7587,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/shares',N'GET',N'*',N'',N''),(7588,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/shares',N'PUT',N'*',N'',N''),(7589,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/channels/share',N'POST',N'*',N'',N''),(7590,N'p',N'role_1',N'/api/gb28181/cascade/platforms/:id/channels/unshare',N'POST',N'*',N'',N'');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- ZLM media-node registry and durable endpoint-recovery gate.
IF OBJECT_ID(N'meta_node', N'U') IS NOT NULL DROP TABLE [meta_node];
CREATE TABLE [meta_node] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [revision] BIGINT NOT NULL CONSTRAINT [df_meta_node_revision] DEFAULT 1,
    [name] NVARCHAR(64) NOT NULL CONSTRAINT [df_meta_node_name] DEFAULT N'',
    [host] NVARCHAR(64) NOT NULL CONSTRAINT [df_meta_node_host] DEFAULT N'',
    [receive_host] NVARCHAR(255) NOT NULL CONSTRAINT [df_meta_node_receive_host] DEFAULT N'',
    [playback_host] NVARCHAR(255) NOT NULL CONSTRAINT [df_meta_node_playback_host] DEFAULT N'',
    [api_port] INT NOT NULL CONSTRAINT [df_meta_node_api_port] DEFAULT 18080,
    [api_secret] NVARCHAR(128) NOT NULL CONSTRAINT [df_meta_node_api_secret] DEFAULT N'',
    [media_server_uuid] NVARCHAR(64) NOT NULL CONSTRAINT [df_meta_node_media_server_uuid] DEFAULT N'',
    [weight] INT NOT NULL CONSTRAINT [df_meta_node_weight] DEFAULT 50,
    [tags_json] NVARCHAR(MAX),
    [state] NVARCHAR(16) NOT NULL CONSTRAINT [df_meta_node_state] DEFAULT N'active',
    [recovery_required] BIT NOT NULL CONSTRAINT [df_meta_node_recovery_required] DEFAULT 0,
    [recovery_reason] NVARCHAR(255) NOT NULL CONSTRAINT [df_meta_node_recovery_reason] DEFAULT N'',
    [recovery_fingerprint] CHAR(64) NOT NULL CONSTRAINT [df_meta_node_recovery_fingerprint] DEFAULT N'',
    [rtp_port_start] INT NOT NULL CONSTRAINT [df_meta_node_rtp_port_start] DEFAULT 30000,
    [rtp_port_end] INT NOT NULL CONSTRAINT [df_meta_node_rtp_port_end] DEFAULT 35000,
    [created_at] DATETIME2 NULL,
    [updated_at] DATETIME2 NULL,
    CONSTRAINT [pk_meta_node] PRIMARY KEY ([id]),
    CONSTRAINT [uk_media_server_uuid] UNIQUE ([media_server_uuid])
);
CREATE INDEX [idx_state] ON [meta_node] ([state]);
CREATE INDEX [idx_recovery_required] ON [meta_node] ([recovery_required]);

-- GB28181 device registry and dual-version profile archive.
IF OBJECT_ID(N'gb_device', N'U') IS NOT NULL DROP TABLE [gb_device];
CREATE TABLE [gb_device] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [device_id] NVARCHAR(20) NOT NULL CONSTRAINT [df_gb_device_device_id] DEFAULT N'',
    [name] NVARCHAR(255) NOT NULL CONSTRAINT [df_gb_device_name] DEFAULT N'',
    [password] NVARCHAR(255) NOT NULL CONSTRAINT [df_gb_device_password] DEFAULT N'',
    [transport] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_device_transport] DEFAULT N'',
    [manufacturer] NVARCHAR(255) NOT NULL CONSTRAINT [df_gb_device_manufacturer] DEFAULT N'',
    [model] NVARCHAR(255) NOT NULL CONSTRAINT [df_gb_device_model] DEFAULT N'',
    [firmware] NVARCHAR(255) NOT NULL CONSTRAINT [df_gb_device_firmware] DEFAULT N'',
    [ip] NVARCHAR(64) NOT NULL CONSTRAINT [df_gb_device_ip] DEFAULT N'',
    [port] INT NULL CONSTRAINT [df_gb_device_port] DEFAULT 0,
    [register_time] DATETIME2(3) NULL,
    [register_expire_at] DATETIME2(3) NULL,
    [keepalive_time] DATETIME2(3) NULL,
    [keepalive_interval] INT NULL CONSTRAINT [df_gb_device_keepalive_interval] DEFAULT 60,
    [expires] INT NULL CONSTRAINT [df_gb_device_expires] DEFAULT 0,
    [status] TINYINT NULL CONSTRAINT [df_gb_device_status] DEFAULT 0,
    [offline_at] DATETIME2(3) NULL,
    [created_at] DATETIME2(3) NULL,
    [updated_at] DATETIME2(3) NULL,
    [deleted_at] DATETIME2(3) NULL,
    [created_by] BIGINT NULL CONSTRAINT [df_gb_device_created_by] DEFAULT 0,
    [owner_dept_id] BIGINT NOT NULL CONSTRAINT [df_gb_device_owner_dept_id] DEFAULT 0,
    [reported_version] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_device_reported_version] DEFAULT N'',
    [reported_version_at] DATETIME2(3) NULL,
    [protocol_override] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_device_protocol_override] DEFAULT N'auto',
    [effective_version] NVARCHAR(8) NOT NULL CONSTRAINT [df_gb_device_effective_version] DEFAULT N'2016',
    [effective_version_source] NVARCHAR(16) NOT NULL CONSTRAINT [df_gb_device_effective_source] DEFAULT N'default',
    [effective_version_at] DATETIME2(3) NULL,
    [zlm_node_id] BIGINT NOT NULL CONSTRAINT [df_gb_device_zlm_node] DEFAULT 0,
    CONSTRAINT [pk_gb_device] PRIMARY KEY ([id]),
    CONSTRAINT [uk_gb_device_id] UNIQUE ([device_id])
);
CREATE INDEX [idx_gb_device_deleted_at] ON [gb_device] ([deleted_at]);
CREATE INDEX [idx_gb_device_owner_dept_deleted] ON [gb_device] ([owner_dept_id], [deleted_at]);
CREATE INDEX [idx_gb_device_status_keepalive] ON [gb_device] ([status], [keepalive_time]);
CREATE INDEX [idx_gb_device_zlm_node] ON [gb_device] ([zlm_node_id]);

-- GB28181 PTZ / home-position tables (2026-07-24).
IF OBJECT_ID(N'gb_ptz_home_position', N'U') IS NOT NULL DROP TABLE [gb_ptz_home_position];
IF OBJECT_ID(N'gb_ptz_operation_attempt', N'U') IS NOT NULL DROP TABLE [gb_ptz_operation_attempt];
IF OBJECT_ID(N'gb_ptz_cruise_track', N'U') IS NOT NULL DROP TABLE [gb_ptz_cruise_track];
IF OBJECT_ID(N'gb_ptz_preset', N'U') IS NOT NULL DROP TABLE [gb_ptz_preset];
IF OBJECT_ID(N'gb_ptz_state', N'U') IS NOT NULL DROP TABLE [gb_ptz_state];
IF OBJECT_ID(N'gb_ptz_operation', N'U') IS NOT NULL DROP TABLE [gb_ptz_operation];

CREATE TABLE [gb_ptz_operation] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [operation_id] NVARCHAR(64) NOT NULL,
    [idempotency_key] NVARCHAR(128) NOT NULL,
    [device_id] BIGINT NOT NULL,
    [device_code] NVARCHAR(20) NOT NULL,
    [channel_id] BIGINT NOT NULL,
    [channel_code] NVARCHAR(20) NOT NULL,
    [cmd_type] NVARCHAR(64) NOT NULL,
    [action] NVARCHAR(64),
    [payload_json] NVARCHAR(MAX),
    [sn] INT NOT NULL,
    [call_id] NVARCHAR(255),
    [cseq] NVARCHAR(64),
    [sip_status] INT NOT NULL CONSTRAINT [df_ptz_operation_sip_status] DEFAULT 0,
    [device_result] NVARCHAR(32),
    [device_error] NVARCHAR(MAX),
    [status] NVARCHAR(16) NOT NULL,
    [attempt] INT NOT NULL CONSTRAINT [df_ptz_operation_attempt] DEFAULT 1,
    [response_required] BIT NOT NULL CONSTRAINT [df_ptz_operation_response_required] DEFAULT 0,
    [max_attempts] INT NOT NULL CONSTRAINT [df_ptz_operation_max_attempts] DEFAULT 1,
    [error_code] NVARCHAR(64),
    [error_message] NVARCHAR(MAX),
    [actor_id] BIGINT NOT NULL CONSTRAINT [df_ptz_operation_actor_id] DEFAULT 0,
    [actor_dept_id] BIGINT NOT NULL CONSTRAINT [df_ptz_operation_actor_dept_id] DEFAULT 0,
    [created_at] DATETIME2(3) NOT NULL,
    [sent_at] DATETIME2(3),
    [completed_at] DATETIME2(3),
    [queue_deadline_at] DATETIME2(3),
    [dispatch_started_at] DATETIME2(3),
    [transport_deadline_at] DATETIME2(3),
    [deadline_at] DATETIME2(3),
    [next_attempt_at] DATETIME2(3),
    [response_call_id] NVARCHAR(255),
    [response_cseq] NVARCHAR(64),
    [response_at] DATETIME2(3),
    [response_has_data] BIT,
    [trigger_operation_id] NVARCHAR(64),
    [reconcile_operation_id] NVARCHAR(64),
    CONSTRAINT [pk_ptz_operation] PRIMARY KEY ([id]),
    CONSTRAINT [uk_ptz_operation_id] UNIQUE ([operation_id]),
    CONSTRAINT [uk_ptz_operation_idempotency] UNIQUE ([channel_id], [idempotency_key])
);

CREATE TABLE [gb_ptz_state] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [device_id] BIGINT NOT NULL,
    [device_code] NVARCHAR(20) NOT NULL,
    [channel_id] BIGINT NOT NULL,
    [channel_code] NVARCHAR(20) NOT NULL,
    [pan] DECIMAL(18,6),
    [tilt] DECIMAL(18,6),
    [zoom] DECIMAL(18,6),
    [focus] DECIMAL(18,6),
    [iris] DECIMAL(18,6),
    [device_time] DATETIME2(3),
    [received_at] DATETIME2(3) NOT NULL,
    [source_sn] INT NOT NULL CONSTRAINT [df_ptz_state_source_sn] DEFAULT 0,
    [freshness] NVARCHAR(16) NOT NULL CONSTRAINT [df_ptz_state_freshness] DEFAULT 'unknown',
    [dedupe_key] NVARCHAR(128),
    [raw_summary] NVARCHAR(MAX),
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_ptz_state] PRIMARY KEY ([id]),
    CONSTRAINT [uk_ptz_state_channel] UNIQUE ([channel_id])
);

CREATE TABLE [gb_ptz_preset] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [device_id] BIGINT NOT NULL,
    [channel_id] BIGINT NOT NULL,
    [preset_id] INT NOT NULL,
    [name] NVARCHAR(255),
    [status] NVARCHAR(16) NOT NULL CONSTRAINT [df_ptz_preset_status] DEFAULT 'unknown',
    [last_operation_id] NVARCHAR(64),
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_ptz_preset] PRIMARY KEY ([id]),
    CONSTRAINT [uk_ptz_preset_channel_number] UNIQUE ([channel_id], [preset_id])
);

CREATE TABLE [gb_ptz_cruise_track] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [device_id] BIGINT NOT NULL,
    [channel_id] BIGINT NOT NULL,
    [track_id] INT NOT NULL,
    [name] NVARCHAR(255),
    [enabled] BIT,
    [detail_json] NVARCHAR(MAX),
    [last_operation_id] NVARCHAR(64),
    [raw_summary] NVARCHAR(MAX),
    [device_time] DATETIME2(3),
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_ptz_cruise_track] PRIMARY KEY ([id]),
    CONSTRAINT [uk_ptz_cruise_channel_track] UNIQUE ([channel_id], [track_id])
);

CREATE TABLE [gb_ptz_operation_attempt] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [operation_id] BIGINT NOT NULL,
    [attempt_no] INT NOT NULL,
    [sn] INT NOT NULL,
    [status] NVARCHAR(16) NOT NULL,
    [call_id] NVARCHAR(255),
    [cseq] NVARCHAR(64),
    [sip_status] INT NOT NULL CONSTRAINT [df_ptz_attempt_sip_status] DEFAULT 0,
    [started_at] DATETIME2(3) NOT NULL,
    [lease_until] DATETIME2(3) NOT NULL,
    [sent_at] DATETIME2(3),
    [completed_at] DATETIME2(3),
    [error_code] NVARCHAR(64),
    [error_message] NVARCHAR(MAX),
    [created_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_ptz_operation_attempt] PRIMARY KEY ([id]),
    CONSTRAINT [uk_ptz_operation_attempt] UNIQUE ([operation_id], [attempt_no])
);

CREATE TABLE [gb_ptz_home_position] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [device_id] BIGINT NOT NULL,
    [channel_id] BIGINT NOT NULL,
    [channel_code] NVARCHAR(20) NOT NULL,
    [enabled] BIT NOT NULL,
    [reset_time] INT,
    [preset_id] INT,
    [enabled_encoding] NVARCHAR(32) NOT NULL CONSTRAINT [df_ptz_home_enabled_encoding] DEFAULT 'numeric',
    [confirmed_at] DATETIME2(3) NOT NULL,
    [source] NVARCHAR(32) NOT NULL,
    [verification] NVARCHAR(16) NOT NULL,
    [source_sn] INT NOT NULL CONSTRAINT [df_ptz_home_source_sn] DEFAULT 0,
    [source_operation_id] NVARCHAR(64),
    [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_ptz_home_source_seq] DEFAULT 0,
    [raw_summary] NVARCHAR(MAX),
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_ptz_home_position] PRIMARY KEY ([id]),
    CONSTRAINT [uk_ptz_home_position_channel] UNIQUE ([channel_id])
);

CREATE INDEX [idx_ptz_operation_channel_time] ON [gb_ptz_operation] ([channel_id], [created_at]);
CREATE INDEX [idx_ptz_operation_channel_cmd_id] ON [gb_ptz_operation] ([channel_id], [cmd_type], [id]);
CREATE INDEX [idx_ptz_operation_device_sn] ON [gb_ptz_operation] ([device_id], [sn]);
CREATE INDEX [idx_ptz_operation_status_time] ON [gb_ptz_operation] ([status], [created_at]);
CREATE INDEX [idx_ptz_operation_status_next_attempt] ON [gb_ptz_operation] ([status], [next_attempt_at]);
CREATE INDEX [idx_ptz_operation_status_queue_deadline] ON [gb_ptz_operation] ([status], [queue_deadline_at]);
CREATE INDEX [idx_ptz_operation_status_transport_deadline] ON [gb_ptz_operation] ([status], [transport_deadline_at]);
CREATE INDEX [idx_ptz_operation_status_deadline] ON [gb_ptz_operation] ([status], [deadline_at]);
CREATE INDEX [idx_ptz_operation_call_id] ON [gb_ptz_operation] ([call_id]);
CREATE INDEX [idx_ptz_state_device] ON [gb_ptz_state] ([device_id]);
CREATE INDEX [idx_ptz_state_received] ON [gb_ptz_state] ([received_at]);
CREATE INDEX [idx_ptz_preset_device] ON [gb_ptz_preset] ([device_id]);
CREATE INDEX [idx_ptz_cruise_device] ON [gb_ptz_cruise_track] ([device_id]);
CREATE INDEX [idx_ptz_attempt_status_lease] ON [gb_ptz_operation_attempt] ([status], [lease_until]);
CREATE INDEX [idx_ptz_home_position_device] ON [gb_ptz_home_position] ([device_id]);
IF OBJECT_ID('sys_department', 'U') IS NOT NULL DROP TABLE [sys_department];
CREATE TABLE [sys_department] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [parent_id] BIGINT DEFAULT 0,
    [name] NVARCHAR(255),
    [status] TINYINT,
    [leader] NVARCHAR(255),
    [phone] NVARCHAR(255),
    [email] NVARCHAR(255),
    [sort] INT DEFAULT 0,
    [describe] NVARCHAR(255),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT,
    PRIMARY KEY ([id])
);


-- Records of sys_department
SET IDENTITY_INSERT [sys_department] ON;
INSERT INTO [sys_department] ([id], [parent_id], [name], [status], [leader], [phone], [email], [sort], [describe], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1, 0, '总部', 1, '张明', '13800000001', 'headquarters@company.com', 1, '公司总部管理部门', '2023-01-15 09:00:00', '2025-10-31 17:05:24', NULL, 1);
-- Table structure for sys_dict
SET IDENTITY_INSERT [sys_department] OFF;
IF OBJECT_ID('sys_dict', 'U') IS NOT NULL DROP TABLE [sys_dict];
CREATE TABLE [sys_dict] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [name] NVARCHAR(255),
    [code] NVARCHAR(255),
    [status] TINYINT,
    [description] NVARCHAR(500),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT,
    PRIMARY KEY ([id])
);


-- Records of sys_dict
SET IDENTITY_INSERT [sys_dict] ON;
INSERT INTO [sys_dict] ([id], [name], [code], [status], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1, '性别', 'gender', 1, '这是一个性别字典', '2024-07-01 10:00:00', NULL, NULL, 1);
INSERT INTO [sys_dict] ([id], [name], [code], [status], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (2, '状态', 'status', 1, '状态字段可以用这个', '2024-07-01 10:00:00', NULL, NULL, 1);
INSERT INTO [sys_dict] ([id], [name], [code], [status], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (3, '岗位', 'post', 1, '岗位字段', '2024-07-01 10:00:00', NULL, NULL, 1);
INSERT INTO [sys_dict] ([id], [name], [code], [status], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (4, '任务状态', 'taskStatus', 1, '任务状态字段可以用它', '2024-07-01 10:00:00', NULL, NULL, 1);
-- Table structure for sys_dict_item
SET IDENTITY_INSERT [sys_dict] OFF;
IF OBJECT_ID('sys_dict_item', 'U') IS NOT NULL DROP TABLE [sys_dict_item];
CREATE TABLE [sys_dict_item] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [name] NVARCHAR(255),
    [value] NVARCHAR(255),
    [status] TINYINT,
    [dict_id] BIGINT,
    PRIMARY KEY ([id])
);


-- Records of sys_dict_item
SET IDENTITY_INSERT [sys_dict_item] ON;
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (11, '男', '1', 1, 1);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (12, '女', '0', 1, 1);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (13, '其它', '2', 1, 1);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (21, '禁用', '0', 1, 2);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (22, '启用', '1', 1, 2);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (31, '总经理', '1', 1, 3);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (32, '总监', '2', 1, 3);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (33, '人事主管', '3', 1, 3);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (34, '开发部主管', '4', 1, 3);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (35, '普通职员', '5', 1, 3);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (36, '其它', '999', 1, 3);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (41, '失败', '0', 1, 4);
INSERT INTO [sys_dict_item] ([id], [name], [value], [status], [dict_id]) VALUES (42, '成功', '1', 1, 4);
-- Table structure for sys_gen
SET IDENTITY_INSERT [sys_dict_item] OFF;
IF OBJECT_ID('sys_gen', 'U') IS NOT NULL DROP TABLE [sys_gen];
CREATE TABLE [sys_gen] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [db_type] NVARCHAR(255),
    [database] NVARCHAR(255),
    [name] NVARCHAR(255),
    [module_name] NVARCHAR(255),
    [file_name] NVARCHAR(255),
    [describe] NVARCHAR(1000),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT,
    [is_cover] TINYINT DEFAULT 0,
    [is_menu] TINYINT DEFAULT 0,
    [is_tree] TINYINT DEFAULT 0,
    [is_relation_tree] TINYINT DEFAULT 0,
    [relation_tree_table] BIGINT DEFAULT 0,
    [relation_field] BIGINT DEFAULT 0,
    PRIMARY KEY ([id])
);


-- Records of sys_gen
SET IDENTITY_INSERT [sys_gen] ON;
INSERT INTO [sys_gen] ([id], [db_type], [database], [name], [module_name], [file_name], [describe], [created_at], [updated_at], [deleted_at], [created_by], [is_cover], [is_menu], [is_tree], [is_relation_tree], [relation_tree_table], [relation_field]) VALUES (23, 'mysql', 'uvp-gb28181', 'demo_students', 'test_school', 'demo_students', '学员管理', '2025-11-13 15:17:27', '2025-11-17 16:31:43', NULL, 1, 1, 1, NULL, 0, 0, 0);
INSERT INTO [sys_gen] ([id], [db_type], [database], [name], [module_name], [file_name], [describe], [created_at], [updated_at], [deleted_at], [created_by], [is_cover], [is_menu], [is_tree], [is_relation_tree], [relation_tree_table], [relation_field]) VALUES (24, 'mysql', 'uvp-gb28181', 'demo_teacher', 'test_school', 'demo_teacher', '教师表', '2025-11-13 15:17:27', '2025-11-17 17:29:28', NULL, 1, 1, 1, NULL, 0, 0, 0);
-- Table structure for sys_gen_field
SET IDENTITY_INSERT [sys_gen] OFF;
IF OBJECT_ID('sys_gen_field', 'U') IS NOT NULL DROP TABLE [sys_gen_field];
CREATE TABLE [sys_gen_field] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [gen_id] BIGINT,
    [data_name] NVARCHAR(255),
    [data_type] NVARCHAR(255),
    [data_comment] NVARCHAR(255),
    [data_extra] NVARCHAR(255),
    [data_column_key] NVARCHAR(255),
    [data_unsigned] BIGINT DEFAULT 0,
    [is_primary] TINYINT DEFAULT 0,
    [go_type] NVARCHAR(255),
    [front_type] NVARCHAR(255),
    [custom_name] NVARCHAR(255) DEFAULT '',
    [require] TINYINT DEFAULT 0,
    [list_show] TINYINT DEFAULT 0,
    [form_show] TINYINT DEFAULT 0,
    [query_show] TINYINT DEFAULT 0,
    [query_type] NVARCHAR(255),
    [form_type] NVARCHAR(255),
    [dict_type] NVARCHAR(255),
    [gorm_tag] NVARCHAR(255),
    PRIMARY KEY ([id])
);


-- Records of sys_gen_field
SET IDENTITY_INSERT [sys_gen_field] ON;
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (185, 23, 'student_id', 'int', 'ID', 'auto_increment', 'PRI', 1, 1, 'uint', 'number', 'stu_id', 1, 0, 0, 1, 'EQ', '', '', 'column:student_id;primaryKey;not NULL;autoIncrement');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (186, 23, 'student_name', 'varchar', '姓名', '', '', 0, 0, 'string', 'string', 'stu_name', 1, 1, 1, 1, 'LIKE', 'textarea', '', 'column:student_name;not NULL');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (187, 23, 'age', 'int', '年龄', '', '', 0, 0, 'int', 'number', 'age', 1, 1, 1, 1, 'LIKE', '', '', 'column:age;not NULL;default:18');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (188, 23, 'gender', 'varchar', '性别', '', '', 0, 0, 'string', 'string', 'gender', 1, 1, 1, 1, 'BETWEEN', 'radio', 'gender', 'column:gender;not NULL;default:''''');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (189, 23, 'class_name', 'varchar', '班级名称', '', '', 0, 0, 'string', 'string', 'class_name', 0, 1, 1, 0, '', 'checkbox', 'class', 'column:class_name;not NULL');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (190, 23, 'admission_date', 'datetime', '入学日期', '', '', 0, 0, 'time.Time', 'string', 'admission_date', 0, 0, 1, 0, '', '', '', 'column:admission_date;not NULL');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (191, 23, 'email', 'varchar', ' 邮箱', '', 'UNI', 0, 0, 'string', 'string', 'email', 0, 0, 1, 1, '', 'checkbox', 'status', 'column:email;uniqueIndex');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (192, 23, 'phone', 'varchar', '电话号码', '', '', 0, 0, 'string', 'string', 'phone', 0, 0, 0, 0, '', '', '', 'column:phone');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (193, 23, 'address', 'text', '地址', '', '', 0, 0, 'string', 'string', 'address', 0, 0, 1, 1, '', 'select', 'status', 'column:address');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (194, 23, 'created_at', 'datetime', '创建时间', '', '', 0, 0, 'time.Time', 'string', 'created_at', NULL, NULL, 1, 1, 'BETWEEN', '', '', 'column:created_at');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (195, 23, 'updated_at', 'datetime', '更新时间', '', '', 0, 0, 'time.Time', 'string', 'updated_at', NULL, NULL, 1, NULL, '', '', '', 'column:updated_at');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (196, 23, 'deleted_at', 'datetime', '删除时间', '', '', 0, 0, 'time.Time', 'string', 'deleted_at', NULL, NULL, 1, NULL, '', '', '', 'column:deleted_at');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (197, 23, 'created_by', 'int', '创建人', '', '', 1, 0, 'uint', 'number', 'created_by', NULL, NULL, 1, NULL, '', '', '', 'column:created_by');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (199, 24, 'id', 'int', '主键ID', 'auto_increment', 'PRI', 1, 1, 'uint', 'number', 'tc_id', 1, 1, 1, 1, '', '', '', 'column:id;primaryKey;not NULL;autoIncrement');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (200, 24, 'name', 'varchar', '教师姓名', '', '', 0, 0, 'string', 'string', 'tc_name', 1, 1, 1, 1, 'LIKE', 'input', '', 'column:name;not NULL');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (201, 24, 'employee_id', 'varchar', '工号', '', '', 0, 0, 'string', 'string', 'employee_id', 1, 1, 1, 1, 'BETWEEN', '', '', 'column:employee_id');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (202, 24, 'gender', 'tinyint', '性别', '', '', 0, 0, 'int', 'number', 'gender', 1, 1, 1, 1, 'EQ', 'select', 'gender', 'column:gender;default:0');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (203, 24, 'phone', 'varchar', '手机号', '', '', 0, 0, 'string', 'string', 'phone', 1, 1, 1, 1, 'GT', '', '', 'column:phone');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (204, 24, 'email', 'varchar', '邮箱', '', '', 0, 0, 'string', 'string', 'email', 1, 1, 1, 1, 'NE', '', '', 'column:email');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (205, 24, 'subject', 'varchar', '所教学科', '', '', 0, 0, 'string', 'string', 'subject', 1, 1, 1, 1, '', '', '', 'column:subject');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (206, 24, 'title', 'varchar', '职称', '', '', 0, 0, 'string', 'string', 'title', 1, 1, 1, 1, '', '', '', 'column:title');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (207, 24, 'status', 'tinyint', '状态', '', '', 0, 0, 'int', 'number', 'status', 1, 1, 1, 1, '', 'select', 'status', 'column:status;default:1');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (208, 24, 'hire_date', 'date', '入职日期', '', '', 0, 0, 'time.Time', 'string', 'hire_date', 1, 1, 1, 1, 'BETWEEN', '', '', 'column:hire_date');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (209, 24, 'birth_date', 'date', '出生日期', '', '', 0, 0, 'time.Time', 'string', 'birth_date', 1, 1, 1, 1, '', 'select', 'test_date', 'column:birth_date');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (210, 24, 'created_at', 'datetime', '创建时间', '', '', 0, 0, 'time.Time', 'string', 'created_at', NULL, NULL, NULL, NULL, '', '', '', 'column:created_at');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (211, 24, 'updated_at', 'datetime', '更新时间', '', '', 0, 0, 'time.Time', 'string', 'updated_at', NULL, NULL, NULL, NULL, '', '', '', 'column:updated_at');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (212, 24, 'deleted_at', 'datetime', '删除时间', '', '', 0, 0, 'time.Time', 'string', 'deleted_at', NULL, NULL, NULL, NULL, '', '', '', 'column:deleted_at');
INSERT INTO [sys_gen_field] ([id], [gen_id], [data_name], [data_type], [data_comment], [data_extra], [data_column_key], [data_unsigned], [is_primary], [go_type], [front_type], [custom_name], [require], [list_show], [form_show], [query_show], [query_type], [form_type], [dict_type], [gorm_tag]) VALUES (213, 24, 'created_by', 'int', '创建人', '', '', 1, 0, 'uint', 'number', 'created_by', NULL, NULL, NULL, NULL, '', '', '', 'column:created_by');
-- Table structure for sys_jobs
SET IDENTITY_INSERT [sys_gen_field] OFF;
IF OBJECT_ID('sys_jobs', 'U') IS NOT NULL DROP TABLE [sys_jobs];
CREATE TABLE [sys_jobs] (
    [id] NVARCHAR(255) NOT NULL,
    [group] NVARCHAR(100) NOT NULL,
    [name] NVARCHAR(200) NOT NULL,
    [description] NVARCHAR(MAX),
    [executor_name] NVARCHAR(100) NOT NULL,
    [execution_policy] TINYINT NOT NULL DEFAULT 1,
    [status] TINYINT NOT NULL DEFAULT 1,
    [cron_expression] NVARCHAR(100) NOT NULL,
    [parameters] NVARCHAR(MAX),
    [blocking_policy] TINYINT NOT NULL DEFAULT 0,
    [timeout] BIGINT NOT NULL DEFAULT 30000000000,
    [max_retry] INT NOT NULL DEFAULT 0,
    [retry_interval] BIGINT NOT NULL DEFAULT 10000000000,
    [parallel_num] INT NOT NULL DEFAULT 1,
    [running_count] INT NOT NULL DEFAULT 0,
    [created_at] DATETIME NOT NULL DEFAULT GETDATE(),
    [updated_at] DATETIME NOT NULL DEFAULT GETDATE(),
    [deleted_at] DATETIME,
    [created_by] BIGINT,
    PRIMARY KEY ([id])
);


-- Records of sys_jobs
-- Table structure for sys_job_results
IF OBJECT_ID('sys_job_results', 'U') IS NOT NULL DROP TABLE [sys_job_results];
CREATE TABLE [sys_job_results] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [job_id] NVARCHAR(255) NOT NULL,
    [status] NVARCHAR(20) NOT NULL,
    [error] NVARCHAR(MAX),
    [start_time] DATETIME NOT NULL,
    [end_time] DATETIME NOT NULL,
    [duration] BIGINT NOT NULL,
    [retry_count] INT NOT NULL DEFAULT 0,
    [created_at] DATETIME NOT NULL DEFAULT GETDATE(),
    PRIMARY KEY ([id]),
    [CONSTRAINT] NVARCHAR(MAX)
);


-- Records of sys_job_results
-- Table structure for sys_menu
IF OBJECT_ID('sys_menu', 'U') IS NOT NULL DROP TABLE [sys_menu];
CREATE TABLE [sys_menu] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [parent_id] BIGINT NOT NULL DEFAULT 0,
    [path] NVARCHAR(255) NOT NULL,
    [name] NVARCHAR(100) NOT NULL,
    [redirect] NVARCHAR(255),
    [component] NVARCHAR(255),
    [title] NVARCHAR(100),
    [is_full] TINYINT DEFAULT 0,
    [hide] TINYINT DEFAULT 0,
    [disable] TINYINT DEFAULT 0,
    [keep_alive] TINYINT DEFAULT 0,
    [affix] TINYINT DEFAULT 0,
    [link] NVARCHAR(500) DEFAULT '',
    [iframe] TINYINT DEFAULT 0,
    [svg_icon] NVARCHAR(100) DEFAULT '',
    [icon] NVARCHAR(100) DEFAULT '',
    [sort] INT DEFAULT 0,
    [type] TINYINT DEFAULT 2,
    [is_link] TINYINT DEFAULT 0,
    [permission] NVARCHAR(255) DEFAULT '',
    [created_at] DATETIME DEFAULT GETDATE(),
    [updated_at] DATETIME DEFAULT GETDATE(),
    [deleted_at] DATETIME,
    [created_by] BIGINT,
    PRIMARY KEY ([id])
);


-- Records of sys_menu
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1, 0, '/home', 'home', NULL, 'home/home', 'home', 0, 0, 0, 0, 1, '', 0, 'home', '', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-08-27 09:09:44', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (10, 0, '/system', 'system', NULL, NULL, 'system', 0, 0, 0, 1, 0, '', 0, 'set', '', 0, 1, 0, '', '2025-08-27 09:09:44', '2025-08-27 09:09:44', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1001, 10, '/system/account', 'account', '', 'system/account/account', 'account', 0, 0, 0, 1, 0, '', 0, '', 'IconUser', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-10-11 15:37:41', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1002, 10, '/system/role', 'role', '', 'system/role/role', 'role', 0, 0, 0, 1, 0, '', 0, '', 'IconUserGroup', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-10-11 16:16:08', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1003, 10, '/system/menu', 'menu', NULL, 'system/menu/menu', 'menu', 0, 0, 0, 1, 0, '', 0, '', 'icon-menu', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-08-27 09:09:44', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1004, 10, '/system/division', 'division', '', 'system/division/division', 'division', 0, 0, 0, 1, 0, '', 0, '', 'IconMindMapping', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-10-11 16:23:14', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1005, 10, '/system/dictionary', 'dictionary', '', 'system/dictionary/dictionary', 'dictionary', 0, 0, 0, 1, 0, '', 0, '', 'IconBook', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-10-11 16:23:47', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1006, 10, '/system/log', 'log', '', 'system/log/log', 'log', 0, 0, 0, 1, 0, '', 0, '', 'IconCommon', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-10-20 17:14:19', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1007, 10, '/system/userinfo', 'userinfo', '', 'system/userinfo/userinfo', 'userinfo', 0, 1, 0, 1, 0, '', 0, '', 'icon-menu', 0, 2, 0, '', '2025-08-27 09:09:44', '2025-09-17 11:19:11', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140213, 10, '/system/api', 'SystemApi', '', 'system/sysapi/sysapi', 'api-management', 0, 0, 0, 1, 0, '', 0, '', 'IconFile', 0, 2, 0, '', '2025-09-03 10:53:57', '2025-10-16 08:53:42', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140214, 1001, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:account:add', '2025-09-03 16:11:58', '2025-09-03 16:11:58', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140215, 1001, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:account:edit', '2025-09-03 17:11:24', '2025-09-03 17:11:24', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140216, 1001, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:account:delete', '2025-09-03 17:12:22', '2025-09-03 17:12:22', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140218, 1002, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:add', '2025-09-04 16:43:54', '2025-09-04 16:43:54', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140219, 1002, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:edit', '2025-09-04 16:47:15', '2025-09-04 16:47:15', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140220, 1002, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:delete', '2025-09-04 16:50:19', '2025-09-04 16:50:19', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140221, 1002, '', '', '', '', '分配权限', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:addRoleMenu', '2025-09-04 16:53:09', '2025-09-04 16:53:09', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140222, 1003, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:add', '2025-09-04 17:07:16', '2025-09-04 17:07:16', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140223, 1003, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:edit', '2025-09-04 17:11:51', '2025-09-04 17:11:51', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140224, 1003, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:delete', '2025-09-04 17:12:24', '2025-09-04 17:12:24', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140225, 1003, '', '', '', '', '分配权限', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:setMenuApis', '2025-09-04 17:20:09', '2025-09-04 17:20:09', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140226, 140213, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:api:add', '2025-09-04 17:30:56', '2025-09-04 17:30:56', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140227, 140213, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:api:edit', '2025-09-04 17:31:20', '2025-09-04 17:31:20', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140228, 140213, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:api:delete', '2025-09-04 17:31:38', '2025-09-04 17:31:38', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140229, 1004, '', '', '', '', '新增部门', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:division:add', '2025-09-12 14:50:55', '2025-09-12 14:50:55', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140230, 1004, '', '', '', '', '编辑部门', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:division:edit', '2025-09-12 14:51:17', '2025-09-12 14:51:17', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140231, 1004, '', '', '', '', '删除部门', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:division:delete', '2025-09-12 14:51:51', '2025-09-12 14:51:51', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140232, 1005, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dict:add', '2025-09-16 16:38:06', '2025-09-16 16:38:06', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140233, 1005, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dict:edit', '2025-09-16 16:39:58', '2025-09-16 16:39:58', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140234, 1005, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dict:delete', '2025-09-16 16:40:19', '2025-09-16 16:40:19', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140235, 1005, '', '', '', '', '字典项管理', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:list', '2025-09-16 17:09:58', '2025-09-16 17:31:35', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140236, 1005, '', '', '', '', '新增字典项', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:add', '2025-09-16 17:32:06', '2025-09-16 17:32:06', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140237, 1005, '', '', '', '', '编辑字典项', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:edit', '2025-09-16 17:33:16', '2025-09-16 17:33:16', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140238, 1005, '', '', '', '', '删除字典项', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:dictitem:delete', '2025-09-16 17:33:41', '2025-09-16 17:33:41', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140239, 10, '/system/affix', 'SystemAffix', '', 'system/affix/affix', 'file-manager', 0, 0, 0, 1, 0, '', 0, '', 'IconFolder', 0, 2, 0, '', '2025-09-25 15:17:00', '2025-10-15 18:14:16', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140240, 140239, '', '', '', '', '文件上传', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:upload', '2025-09-25 15:45:29', '2025-09-25 15:46:29', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140241, 140239, '', '', '', '', '删除文件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:delete', '2025-09-25 15:46:52', '2025-09-25 15:46:52', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140242, 140239, '', '', '', '', '修改文件名', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:updateName', '2025-09-25 15:47:41', '2025-09-25 15:47:41', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140243, 140239, '', '', '', '', '下载文件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:download', '2025-09-25 15:48:56', '2025-09-25 15:48:56', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140244, 1002, '', '', '', '', '数据权限', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:role:dataScope', '2025-09-26 17:07:16', '2025-09-26 17:07:16', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140245, 10, '/system/sysconfig', 'SystemSysconfig', '', 'system/sysconfig/sysconfig', 'system-config', 0, 0, 0, 1, 0, '', 0, '', 'IconSettings', 0, 2, 0, '', '2025-10-09 16:15:21', '2025-10-15 18:10:54', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140246, 140245, '', '', '', '', '修改系统配置', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:config:update', '2025-10-09 16:24:33', '2025-10-09 16:24:33', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140247, 0, '/demo', 'Demo', '', '', 'plugin-example', 0, 0, 0, 1, 0, '', 0, 'more', '', 0, 1, 0, '', '2025-10-13 14:38:38', '2025-10-16 08:55:06', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140248, 140247, '/plugins/example', 'PluginsExample', '', 'plugins/example/views/examplelist', 'plugin-example', 0, 0, 0, 1, 0, '', 0, '', 'IconMenu', 0, 2, 0, '', '2025-10-13 15:19:20', '2025-10-16 08:55:19', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140249, 140248, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'plugins:example:add', '2025-10-14 11:02:42', '2025-10-14 11:02:42', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140250, 140248, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'plugins:example:edit', '2025-10-14 11:03:08', '2025-10-14 11:03:08', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140251, 140248, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'plugins:example:delete', '2025-10-14 11:03:25', '2025-10-14 11:03:25', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140252, 1007, '', '', '', '', '修改密码、手机号等', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:userinfo:updateAccount', '2025-10-17 11:12:56', '2025-10-17 11:12:56', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140254, 140239, '', '', '', '', '复制链接', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:copy', '2025-10-17 11:38:09', '2025-10-17 11:38:09', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140255, 1006, '', '', '', '', '导出', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:log:export', '2025-10-20 10:16:51', '2025-10-20 10:16:51', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140256, 1006, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:log:delete', '2025-10-20 10:17:19', '2025-10-20 10:17:19', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140257, 1003, '', '', '', '', '导出', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:export', '2025-10-20 17:18:01', '2025-10-20 17:18:13', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140258, 1003, '', '', '', '', '导入', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:menu:import', '2025-10-21 11:29:45', '2025-10-21 11:29:45', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140264, 1007, '', '', '', '', '修改用户基本信息', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:userinfo:updateBasicInfo', '2025-10-31 09:26:42', '2025-10-31 09:26:42', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140265, 10, '/system/codegen', 'SystemCodegen', '', 'system/codegen/codegen', 'codegen', 0, 0, 0, 1, 0, '', 0, '', 'IconCode', 0, 2, 0, '', '2025-11-04 11:45:49', '2025-11-04 11:45:49', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140329, 140265, '', '', '', '', '导入表', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:batchInsert', '2025-11-17 15:32:25', '2025-11-17 15:32:25', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140330, 140265, '', '', '', '', '配置', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:update', '2025-11-17 15:33:57', '2025-11-17 15:33:57', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140331, 140265, '', '', '', '', '预览', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:preview', '2025-11-17 15:34:24', '2025-11-17 15:34:24', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140332, 140265, '', '', '', '', '生成代码文件', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:generate', '2025-11-17 15:35:00', '2025-11-17 15:35:00', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140333, 140265, '', '', '', '', '同步数据库', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:refreshFields', '2025-11-17 15:35:51', '2025-11-17 15:35:51', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140334, 140265, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 1, 3, 0, 'system:codegen:delete', '2025-11-17 15:36:50', '2025-11-17 15:36:50', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140335, 140265, '', '', '', '', '生成菜单', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:codegen:insertmenuandapi', '2025-11-26 15:16:32', '2025-11-26 15:16:32', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140336, 10, '/system/pluginsmanager', 'SystemPluginsmanager', '', 'system/pluginsmanager/pluginsmanager', 'plugins-manager', 0, 0, 0, 1, 0, '', 0, '', 'IconApps', 0, 2, 0, '', '2025-12-05 17:59:34', '2025-12-05 17:59:34', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140338, 140336, '', '', '', '', '导出插件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:pluginsmanager:export', '2025-12-08 16:33:32', '2025-12-08 16:33:32', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140339, 140336, '', '', '', '', '导入插件', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:pluginsmanager:import', '2025-12-08 16:33:51', '2025-12-08 16:33:51', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140340, 140336, '', '', '', '', '插件卸载', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:pluginsmanager:uninstall', '2025-12-08 16:34:53', '2025-12-08 16:34:53', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140341, 0, '/sysjobs', 'Sysjobs', '', '', 'sysjobs', 0, 0, 0, 1, 0, '', 0, 'functions', '', 0, 1, 0, '', '2026-02-11 11:29:40', '2026-02-11 11:38:29', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140342, 140341, '/system/sysjobslist', 'SystemSysjobslist', '', 'system/sysjobs/sysjobslist', 'jobslist', 0, 0, 0, 1, 0, '', 0, '', 'IconList', 0, 2, 0, '', '2026-02-11 11:36:54', '2026-02-11 11:36:54', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140343, 140342, '', '', '', '', '新增', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:add', '2026-02-11 11:43:35', '2026-02-11 11:43:35', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140344, 140342, '', '', '', '', '编辑', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:edit', '2026-02-11 11:44:00', '2026-02-11 11:44:00', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140345, 140342, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:delete', '2026-02-11 11:44:22', '2026-02-11 11:44:22', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140346, 140342, '', '', '', '', '执行一次', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobs:executeNow', '2026-02-12 17:59:02', '2026-02-12 17:59:02', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140347, 140341, '/system/joblog', 'SystemJoblog', '', 'system/sysjobresults/sysjobresultslist', 'joblog', 0, 0, 0, 1, 0, '', 0, '', 'IconHistory', 0, 2, 0, '', '2026-02-11 11:41:27', '2026-02-11 11:41:27', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140348, 140347, '', '', '', '', '删除', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:sysjobresults:delete', '2026-02-11 11:45:18', '2026-02-11 11:45:18', NULL, 1);
INSERT INTO [sys_menu] ([id], [parent_id], [path], [name], [redirect], [component], [title], [is_full], [hide], [disable], [keep_alive], [affix], [link], [iframe], [svg_icon], [icon], [sort], [type], [is_link], [permission], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (140349, 140239, '', '', '', '', '大文件上传', 0, 0, 0, 1, 0, '', 0, '', '', 0, 3, 0, 'system:affix:bigupload', '2026-04-09 15:47:39', '2026-04-09 15:47:39', NULL, 1);
-- Table structure for sys_menu_api
SET IDENTITY_INSERT [sys_menu] OFF;
IF OBJECT_ID('sys_menu_api', 'U') IS NOT NULL DROP TABLE [sys_menu_api];
CREATE TABLE [sys_menu_api] (
    [menu_id] BIGINT NOT NULL,
    [api_id] BIGINT NOT NULL,
    PRIMARY KEY ([menu_id], [api_id])
);


-- Records of sys_menu_api
INSERT INTO [sys_menu_api] VALUES (10, 5);
INSERT INTO [sys_menu_api] VALUES (10, 6);
INSERT INTO [sys_menu_api] VALUES (10, 7);
INSERT INTO [sys_menu_api] VALUES (10, 12);
INSERT INTO [sys_menu_api] VALUES (10, 27);
INSERT INTO [sys_menu_api] VALUES (10, 54);
INSERT INTO [sys_menu_api] VALUES (10, 202);
INSERT INTO [sys_menu_api] VALUES (1001, 7);
INSERT INTO [sys_menu_api] VALUES (1001, 8);
INSERT INTO [sys_menu_api] VALUES (1001, 18);
INSERT INTO [sys_menu_api] VALUES (1001, 19);
INSERT INTO [sys_menu_api] VALUES (1002, 19);
INSERT INTO [sys_menu_api] VALUES (1003, 13);
INSERT INTO [sys_menu_api] VALUES (1004, 18);
INSERT INTO [sys_menu_api] VALUES (1004, 37);
INSERT INTO [sys_menu_api] VALUES (1005, 41);
INSERT INTO [sys_menu_api] VALUES (1006, 70);
INSERT INTO [sys_menu_api] VALUES (1007, 6);
INSERT INTO [sys_menu_api] VALUES (140213, 29);
INSERT INTO [sys_menu_api] VALUES (140214, 9);
INSERT INTO [sys_menu_api] VALUES (140215, 10);
INSERT INTO [sys_menu_api] VALUES (140216, 11);
INSERT INTO [sys_menu_api] VALUES (140218, 24);
INSERT INTO [sys_menu_api] VALUES (140219, 25);
INSERT INTO [sys_menu_api] VALUES (140220, 26);
INSERT INTO [sys_menu_api] VALUES (140221, 13);
INSERT INTO [sys_menu_api] VALUES (140221, 20);
INSERT INTO [sys_menu_api] VALUES (140221, 21);
INSERT INTO [sys_menu_api] VALUES (140222, 15);
INSERT INTO [sys_menu_api] VALUES (140223, 16);
INSERT INTO [sys_menu_api] VALUES (140224, 17);
INSERT INTO [sys_menu_api] VALUES (140224, 197);
INSERT INTO [sys_menu_api] VALUES (140225, 29);
INSERT INTO [sys_menu_api] VALUES (140225, 35);
INSERT INTO [sys_menu_api] VALUES (140225, 36);
INSERT INTO [sys_menu_api] VALUES (140226, 31);
INSERT INTO [sys_menu_api] VALUES (140227, 30);
INSERT INTO [sys_menu_api] VALUES (140227, 32);
INSERT INTO [sys_menu_api] VALUES (140228, 33);
INSERT INTO [sys_menu_api] VALUES (140229, 38);
INSERT INTO [sys_menu_api] VALUES (140230, 39);
INSERT INTO [sys_menu_api] VALUES (140231, 40);
INSERT INTO [sys_menu_api] VALUES (140232, 43);
INSERT INTO [sys_menu_api] VALUES (140233, 44);
INSERT INTO [sys_menu_api] VALUES (140234, 45);
INSERT INTO [sys_menu_api] VALUES (140235, 48);
INSERT INTO [sys_menu_api] VALUES (140236, 50);
INSERT INTO [sys_menu_api] VALUES (140237, 51);
INSERT INTO [sys_menu_api] VALUES (140238, 52);
INSERT INTO [sys_menu_api] VALUES (140239, 58);
INSERT INTO [sys_menu_api] VALUES (140240, 55);
INSERT INTO [sys_menu_api] VALUES (140241, 56);
INSERT INTO [sys_menu_api] VALUES (140242, 57);
INSERT INTO [sys_menu_api] VALUES (140243, 60);
INSERT INTO [sys_menu_api] VALUES (140244, 61);
INSERT INTO [sys_menu_api] VALUES (140245, 62);
INSERT INTO [sys_menu_api] VALUES (140245, 64);
INSERT INTO [sys_menu_api] VALUES (140246, 63);
INSERT INTO [sys_menu_api] VALUES (140248, 65);
INSERT INTO [sys_menu_api] VALUES (140248, 69);
INSERT INTO [sys_menu_api] VALUES (140249, 66);
INSERT INTO [sys_menu_api] VALUES (140250, 67);
INSERT INTO [sys_menu_api] VALUES (140251, 68);
INSERT INTO [sys_menu_api] VALUES (140252, 53);
INSERT INTO [sys_menu_api] VALUES (140254, 60);
INSERT INTO [sys_menu_api] VALUES (140255, 73);
INSERT INTO [sys_menu_api] VALUES (140256, 72);
INSERT INTO [sys_menu_api] VALUES (140257, 74);
INSERT INTO [sys_menu_api] VALUES (140258, 75);
INSERT INTO [sys_menu_api] VALUES (140264, 89);
INSERT INTO [sys_menu_api] VALUES (140265, 190);
INSERT INTO [sys_menu_api] VALUES (140329, 188);
INSERT INTO [sys_menu_api] VALUES (140329, 191);
INSERT INTO [sys_menu_api] VALUES (140330, 192);
INSERT INTO [sys_menu_api] VALUES (140330, 193);
INSERT INTO [sys_menu_api] VALUES (140331, 189);
INSERT INTO [sys_menu_api] VALUES (140332, 105);
INSERT INTO [sys_menu_api] VALUES (140333, 195);
INSERT INTO [sys_menu_api] VALUES (140334, 194);
INSERT INTO [sys_menu_api] VALUES (140335, 196);
INSERT INTO [sys_menu_api] VALUES (140336, 198);
INSERT INTO [sys_menu_api] VALUES (140338, 199);
INSERT INTO [sys_menu_api] VALUES (140339, 200);
INSERT INTO [sys_menu_api] VALUES (140340, 201);
INSERT INTO [sys_menu_api] VALUES (140342, 203);
INSERT INTO [sys_menu_api] VALUES (140342, 204);
INSERT INTO [sys_menu_api] VALUES (140343, 205);
INSERT INTO [sys_menu_api] VALUES (140344, 206);
INSERT INTO [sys_menu_api] VALUES (140344, 207);
INSERT INTO [sys_menu_api] VALUES (140344, 208);
INSERT INTO [sys_menu_api] VALUES (140345, 209);
INSERT INTO [sys_menu_api] VALUES (140346, 210);
INSERT INTO [sys_menu_api] VALUES (140347, 211);
INSERT INTO [sys_menu_api] VALUES (140348, 212);
INSERT INTO [sys_menu_api] VALUES (140349, 55);
INSERT INTO [sys_menu_api] VALUES (140349, 213);
INSERT INTO [sys_menu_api] VALUES (140349, 214);
INSERT INTO [sys_menu_api] VALUES (140349, 215);
INSERT INTO [sys_menu_api] VALUES (140349, 216);
-- Table structure for sys_operation_logs
IF OBJECT_ID('sys_operation_logs', 'U') IS NOT NULL DROP TABLE [sys_operation_logs];
CREATE TABLE [sys_operation_logs] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [user_id] BIGINT,
    [username] NVARCHAR(50),
    [module] NVARCHAR(100),
    [operation] NVARCHAR(100),
    [method] NVARCHAR(10),
    [path] NVARCHAR(500),
    [ip] NVARCHAR(50),
    [user_agent] NVARCHAR(500),
    [request_data] NVARCHAR(MAX),
    [response_data] NVARCHAR(MAX),
    [status_code] INT,
    [duration] BIGINT,
    [error_msg] NVARCHAR(MAX),
    [location] NVARCHAR(100),
    PRIMARY KEY ([id])
);


-- Records of sys_operation_logs
-- Table structure for sys_role
IF OBJECT_ID('sys_role', 'U') IS NOT NULL DROP TABLE [sys_role];
CREATE TABLE [sys_role] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [name] NVARCHAR(255) DEFAULT '',
    [sort] INT DEFAULT 0,
    [status] TINYINT DEFAULT 0,
    [description] NVARCHAR(255),
    [parent_id] BIGINT DEFAULT 0,
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT,
    [data_scope] INT DEFAULT 0,
    [checked_depts] NVARCHAR(1000),
    PRIMARY KEY ([id])
);


-- Records of sys_role
SET IDENTITY_INSERT [sys_role] ON;
INSERT INTO [sys_role] ([id], [name], [sort], [status], [description], [parent_id], [created_at], [updated_at], [deleted_at], [created_by], [data_scope], [checked_depts]) VALUES (1, '系统管理员', 0, 1, '最高权限管理员角色', 0, '2025-09-01 17:32:12', '2025-09-30 15:53:24', NULL, 1, 1, '');
INSERT INTO [sys_role] ([id], [name], [sort], [status], [description], [parent_id], [created_at], [updated_at], [deleted_at], [created_by], [data_scope], [checked_depts]) VALUES (2, '演示', 0, 1, '', 0, '2025-10-14 15:12:09', '2025-10-17 15:34:47', NULL, 1, 0, '');
-- Table structure for sys_role_menu
SET IDENTITY_INSERT [sys_role] OFF;
IF OBJECT_ID('sys_role_menu', 'U') IS NOT NULL DROP TABLE [sys_role_menu];
CREATE TABLE [sys_role_menu] (
    [role_id] BIGINT NOT NULL,
    [menu_id] BIGINT NOT NULL,
    PRIMARY KEY ([role_id], [menu_id])
);


-- Records of sys_role_menu
INSERT INTO [sys_role_menu] VALUES (1, 1);
INSERT INTO [sys_role_menu] VALUES (1, 10);
INSERT INTO [sys_role_menu] VALUES (1, 1001);
INSERT INTO [sys_role_menu] VALUES (1, 1002);
INSERT INTO [sys_role_menu] VALUES (1, 1003);
INSERT INTO [sys_role_menu] VALUES (1, 1004);
INSERT INTO [sys_role_menu] VALUES (1, 1005);
INSERT INTO [sys_role_menu] VALUES (1, 1006);
INSERT INTO [sys_role_menu] VALUES (1, 1007);
INSERT INTO [sys_role_menu] VALUES (1, 140213);
INSERT INTO [sys_role_menu] VALUES (1, 140214);
INSERT INTO [sys_role_menu] VALUES (1, 140215);
INSERT INTO [sys_role_menu] VALUES (1, 140216);
INSERT INTO [sys_role_menu] VALUES (1, 140218);
INSERT INTO [sys_role_menu] VALUES (1, 140219);
INSERT INTO [sys_role_menu] VALUES (1, 140220);
INSERT INTO [sys_role_menu] VALUES (1, 140221);
INSERT INTO [sys_role_menu] VALUES (1, 140222);
INSERT INTO [sys_role_menu] VALUES (1, 140223);
INSERT INTO [sys_role_menu] VALUES (1, 140224);
INSERT INTO [sys_role_menu] VALUES (1, 140225);
INSERT INTO [sys_role_menu] VALUES (1, 140226);
INSERT INTO [sys_role_menu] VALUES (1, 140227);
INSERT INTO [sys_role_menu] VALUES (1, 140228);
INSERT INTO [sys_role_menu] VALUES (1, 140229);
INSERT INTO [sys_role_menu] VALUES (1, 140230);
INSERT INTO [sys_role_menu] VALUES (1, 140231);
INSERT INTO [sys_role_menu] VALUES (1, 140232);
INSERT INTO [sys_role_menu] VALUES (1, 140233);
INSERT INTO [sys_role_menu] VALUES (1, 140234);
INSERT INTO [sys_role_menu] VALUES (1, 140235);
INSERT INTO [sys_role_menu] VALUES (1, 140236);
INSERT INTO [sys_role_menu] VALUES (1, 140237);
INSERT INTO [sys_role_menu] VALUES (1, 140238);
INSERT INTO [sys_role_menu] VALUES (1, 140239);
INSERT INTO [sys_role_menu] VALUES (1, 140240);
INSERT INTO [sys_role_menu] VALUES (1, 140241);
INSERT INTO [sys_role_menu] VALUES (1, 140242);
INSERT INTO [sys_role_menu] VALUES (1, 140243);
INSERT INTO [sys_role_menu] VALUES (1, 140244);
INSERT INTO [sys_role_menu] VALUES (1, 140245);
INSERT INTO [sys_role_menu] VALUES (1, 140246);
INSERT INTO [sys_role_menu] VALUES (1, 140247);
INSERT INTO [sys_role_menu] VALUES (1, 140248);
INSERT INTO [sys_role_menu] VALUES (1, 140249);
INSERT INTO [sys_role_menu] VALUES (1, 140250);
INSERT INTO [sys_role_menu] VALUES (1, 140251);
INSERT INTO [sys_role_menu] VALUES (1, 140252);
INSERT INTO [sys_role_menu] VALUES (1, 140254);
INSERT INTO [sys_role_menu] VALUES (1, 140255);
INSERT INTO [sys_role_menu] VALUES (1, 140256);
INSERT INTO [sys_role_menu] VALUES (1, 140257);
INSERT INTO [sys_role_menu] VALUES (1, 140258);
INSERT INTO [sys_role_menu] VALUES (1, 140264);
INSERT INTO [sys_role_menu] VALUES (1, 140265);
INSERT INTO [sys_role_menu] VALUES (1, 140329);
INSERT INTO [sys_role_menu] VALUES (1, 140330);
INSERT INTO [sys_role_menu] VALUES (1, 140331);
INSERT INTO [sys_role_menu] VALUES (1, 140332);
INSERT INTO [sys_role_menu] VALUES (1, 140333);
INSERT INTO [sys_role_menu] VALUES (1, 140334);
INSERT INTO [sys_role_menu] VALUES (1, 140335);
INSERT INTO [sys_role_menu] VALUES (1, 140336);
INSERT INTO [sys_role_menu] VALUES (1, 140338);
INSERT INTO [sys_role_menu] VALUES (1, 140339);
INSERT INTO [sys_role_menu] VALUES (1, 140340);
INSERT INTO [sys_role_menu] VALUES (2, 1);
INSERT INTO [sys_role_menu] VALUES (2, 10);
INSERT INTO [sys_role_menu] VALUES (2, 1001);
INSERT INTO [sys_role_menu] VALUES (2, 1002);
INSERT INTO [sys_role_menu] VALUES (2, 1003);
INSERT INTO [sys_role_menu] VALUES (2, 1004);
INSERT INTO [sys_role_menu] VALUES (2, 1005);
INSERT INTO [sys_role_menu] VALUES (2, 1006);
INSERT INTO [sys_role_menu] VALUES (2, 1007);
INSERT INTO [sys_role_menu] VALUES (2, 140213);
INSERT INTO [sys_role_menu] VALUES (2, 140214);
INSERT INTO [sys_role_menu] VALUES (2, 140215);
INSERT INTO [sys_role_menu] VALUES (2, 140216);
INSERT INTO [sys_role_menu] VALUES (2, 140218);
INSERT INTO [sys_role_menu] VALUES (2, 140219);
INSERT INTO [sys_role_menu] VALUES (2, 140220);
INSERT INTO [sys_role_menu] VALUES (2, 140221);
INSERT INTO [sys_role_menu] VALUES (2, 140222);
INSERT INTO [sys_role_menu] VALUES (2, 140223);
INSERT INTO [sys_role_menu] VALUES (2, 140224);
INSERT INTO [sys_role_menu] VALUES (2, 140225);
INSERT INTO [sys_role_menu] VALUES (2, 140226);
INSERT INTO [sys_role_menu] VALUES (2, 140227);
INSERT INTO [sys_role_menu] VALUES (2, 140228);
INSERT INTO [sys_role_menu] VALUES (2, 140229);
INSERT INTO [sys_role_menu] VALUES (2, 140230);
INSERT INTO [sys_role_menu] VALUES (2, 140231);
INSERT INTO [sys_role_menu] VALUES (2, 140232);
INSERT INTO [sys_role_menu] VALUES (2, 140233);
INSERT INTO [sys_role_menu] VALUES (2, 140234);
INSERT INTO [sys_role_menu] VALUES (2, 140235);
INSERT INTO [sys_role_menu] VALUES (2, 140236);
INSERT INTO [sys_role_menu] VALUES (2, 140237);
INSERT INTO [sys_role_menu] VALUES (2, 140238);
INSERT INTO [sys_role_menu] VALUES (2, 140239);
INSERT INTO [sys_role_menu] VALUES (2, 140240);
INSERT INTO [sys_role_menu] VALUES (2, 140241);
INSERT INTO [sys_role_menu] VALUES (2, 140242);
INSERT INTO [sys_role_menu] VALUES (2, 140243);
INSERT INTO [sys_role_menu] VALUES (2, 140244);
INSERT INTO [sys_role_menu] VALUES (2, 140245);
INSERT INTO [sys_role_menu] VALUES (2, 140246);
INSERT INTO [sys_role_menu] VALUES (2, 140247);
INSERT INTO [sys_role_menu] VALUES (2, 140248);
INSERT INTO [sys_role_menu] VALUES (2, 140249);
INSERT INTO [sys_role_menu] VALUES (2, 140250);
INSERT INTO [sys_role_menu] VALUES (2, 140251);
INSERT INTO [sys_role_menu] VALUES (2, 140252);
INSERT INTO [sys_role_menu] VALUES (2, 140254);
INSERT INTO [sys_role_menu] VALUES (2, 140255);
INSERT INTO [sys_role_menu] VALUES (2, 140256);
INSERT INTO [sys_role_menu] VALUES (2, 140257);
INSERT INTO [sys_role_menu] VALUES (2, 140258);
INSERT INTO [sys_role_menu] VALUES (2, 140264);
INSERT INTO [sys_role_menu] VALUES (2, 140265);
INSERT INTO [sys_role_menu] VALUES (2, 140329);
INSERT INTO [sys_role_menu] VALUES (2, 140330);
INSERT INTO [sys_role_menu] VALUES (2, 140331);
INSERT INTO [sys_role_menu] VALUES (2, 140332);
INSERT INTO [sys_role_menu] VALUES (2, 140333);
INSERT INTO [sys_role_menu] VALUES (2, 140334);
INSERT INTO [sys_role_menu] VALUES (2, 140335);
INSERT INTO [sys_role_menu] VALUES (2, 140336);
INSERT INTO [sys_role_menu] VALUES (2, 140338);
INSERT INTO [sys_role_menu] VALUES (2, 140339);
INSERT INTO [sys_role_menu] VALUES (2, 140340);
-- Table structure for sys_users
IF OBJECT_ID('sys_users', 'U') IS NOT NULL DROP TABLE [sys_users];
CREATE TABLE [sys_users] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [username] NVARCHAR(50) NOT NULL DEFAULT '',
    [password] NVARCHAR(255) NOT NULL DEFAULT '',
    [email] NVARCHAR(100) DEFAULT '',
    [status] TINYINT DEFAULT 1,
    [dept_id] BIGINT DEFAULT 0,
    [phone] NVARCHAR(64) DEFAULT '',
    [sex] NVARCHAR(64) DEFAULT '',
    [nick_name] NVARCHAR(100) DEFAULT '',
    [avatar] NVARCHAR(255) DEFAULT '',
    [description] NVARCHAR(500),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT DEFAULT 0,
    PRIMARY KEY ([id])
);


-- Records of sys_users
SET IDENTITY_INSERT [sys_users] ON;
INSERT INTO [sys_users] ([id], [username], [password], [email], [status], [dept_id], [phone], [sex], [nick_name], [avatar], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (1, 'admin', '/PXiqzsBr7huy.Dqdwucyb795qiWcA6fsn0Lu.GLA.C', 'admin@example.com', 1, 1, '18800000006', '1', '超级管理员', '/public/uploads/2025-11-04/20251104_0945787a-8536-45fc-ba75-e94c8daaec06.jpeg', '超级管理员', '2025-08-18 14:55:05', '2025-11-17 17:38:01', NULL, 0);
INSERT INTO [sys_users] ([id], [username], [password], [email], [status], [dept_id], [phone], [sex], [nick_name], [avatar], [description], [created_at], [updated_at], [deleted_at], [created_by]) VALUES (4, 'demo', '/hhQYUffheRnDopYjiq1AKGdgrg1oatLha7tc/.Qe', '', 1, 1, '', '1', '演示账号', '', '演示账号', '2025-10-17 15:38:37', '2025-10-31 16:32:34', NULL, 1);
-- Table structure for sys_user_role
SET IDENTITY_INSERT [sys_users] OFF;
IF OBJECT_ID('sys_user_role', 'U') IS NOT NULL DROP TABLE [sys_user_role];
CREATE TABLE [sys_user_role] (
    [user_id] BIGINT NOT NULL DEFAULT 0,
    [role_id] BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY ([user_id], [role_id])
);


-- Records of sys_user_role
INSERT INTO [sys_user_role] VALUES (1, 1);
INSERT INTO [sys_user_role] VALUES (4, 2);


-- Table structure for sys_param
IF OBJECT_ID('sys_param', 'U') IS NOT NULL DROP TABLE [sys_param];
CREATE TABLE [sys_param] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [name] NVARCHAR(255),
    [code] NVARCHAR(255) NOT NULL,
    [value] NVARCHAR(MAX),
    [status] TINYINT DEFAULT 1,
    [description] NVARCHAR(500),
    [created_at] DATETIME,
    [updated_at] DATETIME,
    [deleted_at] DATETIME,
    [created_by] BIGINT DEFAULT 0,
    PRIMARY KEY ([id])
);

CREATE UNIQUE INDEX [idx_sys_param_code] ON [sys_param] ([code]);
CREATE INDEX [idx_sys_param_deleted_at] ON [sys_param] ([deleted_at]);

-- SIP 首次部署配置 (2026-07-20 起 gb_sip_config 是引导判据的唯一权威源)
IF OBJECT_ID('gb_sip_config', 'U') IS NOT NULL DROP TABLE [gb_sip_config];

CREATE TABLE [gb_sip_config] (
    [id] SMALLINT NOT NULL,
    [deployment_mode] NVARCHAR(8) NOT NULL,
    [listen_ip] NVARCHAR(45) NOT NULL,
    [advertise_ip] NVARCHAR(45) NOT NULL,
    [advertise_ip_inferred] BIT NOT NULL CONSTRAINT [df_gb_sip_config_advertise_ip_inferred] DEFAULT 0,
    [port] INT NOT NULL,
    [domain] NVARCHAR(10) NOT NULL,
    [server_id] NVARCHAR(20) NOT NULL,
    [password] NVARCHAR(255) NOT NULL,
    [created_at] DATETIME NOT NULL CONSTRAINT [df_gb_sip_config_created_at] DEFAULT GETDATE(),
    [updated_at] DATETIME NOT NULL CONSTRAINT [df_gb_sip_config_updated_at] DEFAULT GETDATE(),
    CONSTRAINT [pk_gb_sip_config] PRIMARY KEY ([id]),
    CONSTRAINT [chk_gb_sip_config_singleton] CHECK ([id] = 1),
    CONSTRAINT [chk_gb_sip_config_deployment_mode] CHECK ([deployment_mode] IN ('lan', 'public')),
    CONSTRAINT [chk_gb_sip_config_port] CHECK ([port] BETWEEN 1 AND 65535)
);

-- 首装用户不 seed gb_sip_config,DB 为空触发引导页.
-- 老 stack 升级由 setup.MigrateYAMLToDB 一次性从 config.yml 搬迁到本表.

-- 创建索引
CREATE INDEX [sys_jobs_idx_group] ON [sys_jobs] ([group]);
CREATE INDEX [sys_jobs_idx_status] ON [sys_jobs] ([status]);
CREATE INDEX [sys_jobs_idx_executor_name] ON [sys_jobs] ([executor_name]);
CREATE INDEX [sys_jobs_idx_created_at] ON [sys_jobs] ([created_at]);
CREATE INDEX [sys_job_results_idx_job_id] ON [sys_job_results] ([job_id]);
CREATE INDEX [sys_job_results_idx_status] ON [sys_job_results] ([status]);
CREATE INDEX [sys_job_results_idx_start_time] ON [sys_job_results] ([start_time]);
CREATE INDEX [sys_job_results_idx_created_at] ON [sys_job_results] ([created_at]);
CREATE INDEX [sys_operation_logs_idx_sys_operation_logs_deleted_at] ON [sys_operation_logs] ([deleted_at]);
CREATE INDEX [sys_operation_logs_idx_user_id] ON [sys_operation_logs] ([user_id]);
CREATE UNIQUE INDEX [sys_users_username] ON [sys_users] ([username]);
CREATE INDEX [sys_affix_idx_sys_affix_file_md5] ON [sys_affix] ([file_md5]);
CREATE UNIQUE INDEX [sys_casbin_rule_idx_casbin_rule] ON [sys_casbin_rule] ([ptype], [v0], [v1], [v2], [v3], [v4], [v5]);
CREATE INDEX [sys_affix_chunk_idx_upload_id] ON [sys_affix_chunk] ([upload_id]);
CREATE INDEX [sys_affix_chunk_idx_file_md5] ON [sys_affix_chunk] ([file_md5]);
CREATE INDEX [sys_menu_idx_parent_id] ON [sys_menu] ([parent_id]);
CREATE INDEX [sys_menu_idx_sort] ON [sys_menu] ([sort]);
CREATE INDEX [sys_menu_idx_type] ON [sys_menu] ([type]);

-- UVP UI language: use Lucide icons for menu entries.
UPDATE [sys_menu]
SET
    [svg_icon] = '',
    [icon] = CASE [id]
        WHEN 1 THEN 'lucide:Gauge'
        WHEN 10 THEN 'lucide:Settings'
        WHEN 1001 THEN 'lucide:UserRound'
        WHEN 1002 THEN 'lucide:Shield'
        WHEN 1003 THEN 'lucide:Menu'
        WHEN 1004 THEN 'lucide:Building2'
        WHEN 1005 THEN 'lucide:BookOpen'
        WHEN 1006 THEN 'lucide:FileText'
        WHEN 1007 THEN 'lucide:UserCog'
        WHEN 140213 THEN 'lucide:Network'
        WHEN 140239 THEN 'lucide:Folder'
        WHEN 140245 THEN 'lucide:SlidersHorizontal'
        WHEN 140247 THEN 'lucide:Box'
        WHEN 140248 THEN 'lucide:Box'
        WHEN 140265 THEN 'lucide:CodeXml'
        WHEN 140336 THEN 'lucide:Blocks'
        WHEN 140341 THEN 'lucide:CalendarClock'
        WHEN 140342 THEN 'lucide:ListTodo'
        WHEN 140347 THEN 'lucide:History'
        WHEN 140350 THEN 'lucide:Cctv'
        WHEN 140351 THEN 'lucide:Server'
        WHEN 140352 THEN 'lucide:Server'
        WHEN 140353 THEN 'lucide:Workflow'
        WHEN 140354 THEN 'lucide:History'
        WHEN 140355 THEN 'lucide:Clapperboard'
        WHEN 140357 THEN 'lucide:Activity'
        WHEN 140358 THEN 'lucide:MonitorPlay'
        ELSE [icon]
    END
WHERE [id] IN (
    1, 10, 1001, 1002, 1003, 1004, 1005, 1006, 1007,
    140213, 140239, 140245, 140247, 140248, 140265,
    140336, 140341, 140342, 140347, 140350, 140351, 140352,
    140353, 140354, 140355, 140357, 140358
);

SET NOCOUNT OFF;

-- SIP setup API, UI permissions and administrator policies.
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id],[title],[path],[method],[api_group],[created_at],[updated_at],[deleted_at],[created_by]) VALUES
(217,N'读取 SIP 配置状态','/api/gb28181/sip/setup/status','GET',N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(218,N'读取本机网络接口','/api/gb28181/sip/setup/network-interfaces','GET',N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(219,N'读取 SIP 平台信息','/api/gb28181/sip/platform','GET',N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(220,N'保存 SIP 配置','/api/gb28181/sip/setup/config','PUT',N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(221,N'暂缓 SIP 配置','/api/gb28181/sip/setup/skip','POST',N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[hide],[type],[permission],[created_at],[updated_at],[created_by]) VALUES
(140359,140355,'','','',N'查看 SIP 配置',1,3,'gb28181:sip:config:view',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),
(140360,140355,'','','',N'修改 SIP 配置',1,3,'gb28181:sip:config:update',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
SET IDENTITY_INSERT [sys_menu] OFF;
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140359),(1,140360);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) VALUES
(140359,217),(140359,218),(140359,219),(140360,220),(140360,221);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id],[ptype],[v0],[v1],[v2],[v3],[v4],[v5]) VALUES
(7561,'p','role_1','/api/gb28181/sip/setup/status','GET','*','',''),
(7562,'p','role_1','/api/gb28181/sip/setup/network-interfaces','GET','*','',''),
(7563,'p','role_1','/api/gb28181/sip/platform','GET','*','',''),
(7564,'p','role_1','/api/gb28181/sip/setup/config','PUT','*','',''),
(7565,'p','role_1','/api/gb28181/sip/setup/skip','POST','*','','');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- Custom device groups reuse the device management page and expose one hidden permission.
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id],[title],[path],[method],[api_group],[created_at],[updated_at],[deleted_at],[created_by]) VALUES
(222,N'创建自定义分组','/api/gb28181/device-mgmt/custom-groups','POST',N'GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(223,N'修改自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','PATCH',N'GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(224,N'移动自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/move','POST',N'GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(225,N'删除自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','DELETE',N'GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(226,N'添加分组设备','/api/gb28181/device-mgmt/custom-groups/:id/devices','POST',N'GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(227,N'移除分组设备','/api/gb28181/device-mgmt/custom-groups/:id/devices/remove','POST',N'GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[hide],[type],[permission],[created_at],[updated_at],[created_by]) VALUES
(140361,140355,'','','',N'管理自定义分组',1,3,'gb28181:device-group:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
SET IDENTITY_INSERT [sys_menu] OFF;
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140361);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) VALUES
(140361,222),(140361,223),(140361,224),(140361,225),(140361,226),(140361,227);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id],[ptype],[v0],[v1],[v2],[v3],[v4],[v5]) VALUES
(7566,'p','role_1','/api/gb28181/device-mgmt/custom-groups','POST','*','',''),
(7567,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id','PATCH','*','',''),
(7568,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id/move','POST','*','',''),
(7569,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id','DELETE','*','',''),
(7570,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id/devices','POST','*','',''),
(7571,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id/devices/remove','POST','*','','');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- Playback authorization settings for fresh SQL Server installs.
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id],[title],[path],[method],[api_group],[created_at],[updated_at],[deleted_at],[created_by]) VALUES
(247,N'读取播放鉴权配置','/api/gb28181/sip/service-config/play-auth','GET',N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(248,N'修改播放鉴权配置','/api/gb28181/sip/service-config/play-auth','PUT',N'GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(249,N'发起实时点播','/api/gb28181/play/:deviceId/:channelId','POST',N'GB28181 播放鉴权',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(250,N'申请固定播放地址授权','/api/gb28181/play/:deviceId/:channelId/authorization','POST',N'GB28181 播放鉴权',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[hide],[type],[permission],[created_at],[updated_at],[created_by]) VALUES
(140371,140355,'','','',N'发起实时点播',1,3,'gb28181:play:start',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
SET IDENTITY_INSERT [sys_menu] OFF;
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140371);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) VALUES (140359,247),(140360,248),(140371,249),(140371,250);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id],[ptype],[v0],[v1],[v2],[v3],[v4],[v5]) VALUES
(7591,'p','role_1','/api/gb28181/sip/service-config/play-auth','GET','*','',''),
(7592,'p','role_1','/api/gb28181/sip/service-config/play-auth','PUT','*','',''),
(7593,'p','role_1','/api/gb28181/play/:deviceId/:channelId','POST','*','',''),
(7594,'p','role_1','/api/gb28181/play/:deviceId/:channelId/authorization','POST','*','','');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- Cloud recording download task control APIs for fresh SQL Server installs.
-- The content route is authorized by a one-time HttpOnly cookie and is not seeded here.
SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id],[title],[path],[method],[api_group],[created_at],[updated_at],[deleted_at],[created_by]) VALUES
(251,N'创建云端录像下载','/api/gb28181/cloud-recordings/files/:id/downloads','POST',N'GB28181 云端录像下载',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(252,N'查询云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','GET',N'GB28181 云端录像下载',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(253,N'取消云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE',N'GB28181 云端录像下载',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a
WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL
  AND a.[id] IN (251,252,253);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id],[ptype],[v0],[v1],[v2],[v3],[v4],[v5])
VALUES
(7595,'p','role_1','/api/gb28181/cloud-recordings/files/:id/downloads','POST','*','',''),
(7596,'p','role_1','/api/gb28181/cloud-recordings/downloads/:taskId','GET','*','',''),
(7597,'p','role_1','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE','*','','');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

IF COL_LENGTH(N'gb_ptz_operation', N'profile_version') IS NULL ALTER TABLE [gb_ptz_operation] ADD [profile_version] NVARCHAR(8) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'profile_charset') IS NULL ALTER TABLE [gb_ptz_operation] ADD [profile_charset] NVARCHAR(16) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'target_scope') IS NULL ALTER TABLE [gb_ptz_operation] ADD [target_scope] NVARCHAR(16) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'target_code') IS NULL ALTER TABLE [gb_ptz_operation] ADD [target_code] NVARCHAR(20) NULL;
IF COL_LENGTH(N'gb_ptz_operation', N'scope_key') IS NULL ALTER TABLE [gb_ptz_operation] ADD [scope_key] NVARCHAR(64) NULL;
IF OBJECT_ID(N'gb_device_control_state', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_device_control_state] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_id] BIGINT NOT NULL CONSTRAINT [df_full_control_state_channel] DEFAULT 0,
        [target_scope] NVARCHAR(16) NOT NULL,
        [target_code] NVARCHAR(20) NOT NULL,
        [record_state] NVARCHAR(8) NOT NULL CONSTRAINT [df_full_control_state_record] DEFAULT N'unknown',
        [guard_state] NVARCHAR(8) NOT NULL CONSTRAINT [df_full_control_state_guard] DEFAULT N'unknown',
        [freshness] NVARCHAR(8) NOT NULL CONSTRAINT [df_full_control_state_freshness] DEFAULT N'unknown',
        [observed_at] DATETIME2(3) NOT NULL,
        [source] NVARCHAR(32) NOT NULL CONSTRAINT [df_full_control_state_source] DEFAULT N'device_status',
        [source_sn] INT NOT NULL CONSTRAINT [df_full_control_state_source_sn] DEFAULT 0,
        [source_operation_id] NVARCHAR(64) NULL,
        [source_operation_seq] BIGINT NOT NULL CONSTRAINT [df_full_control_state_source_operation_seq] DEFAULT 0,
        [raw_summary] NVARCHAR(MAX) NULL,
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_full_gb_device_control_state] PRIMARY KEY ([id]),
        CONSTRAINT [uk_control_state_target] UNIQUE ([device_id], [target_scope], [target_code])
    );
END;

IF OBJECT_ID(N'gb_sip_trace_capture', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_sip_trace_capture] (
        [id] CHAR(36) NOT NULL PRIMARY KEY, [device_id] BIGINT NOT NULL, [device_code] VARCHAR(20) NOT NULL,
        [created_by] BIGINT NOT NULL, [started_at] DATETIME2(3) NOT NULL, [planned_end_at] DATETIME2(3) NOT NULL,
        [ended_at] DATETIME2(3) NULL, [end_reason] VARCHAR(16) NOT NULL DEFAULT '', [active_key] VARCHAR(64) NULL,
        [created_at] DATETIME2(3) NOT NULL, [updated_at] DATETIME2(3) NOT NULL
    );
    CREATE UNIQUE INDEX [uk_sip_trace_capture_active] ON [gb_sip_trace_capture] ([active_key]) WHERE [active_key] IS NOT NULL;
    CREATE INDEX [idx_sip_trace_capture_device_started] ON [gb_sip_trace_capture] ([device_id], [started_at]);
    CREATE INDEX [idx_sip_trace_capture_device_code] ON [gb_sip_trace_capture] ([device_code]);
    CREATE INDEX [idx_sip_trace_capture_created_by] ON [gb_sip_trace_capture] ([created_by]);
    CREATE INDEX [idx_sip_trace_capture_planned_end] ON [gb_sip_trace_capture] ([planned_end_at]);
END;

IF OBJECT_ID(N'gb_sip_trace_message', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_sip_trace_message] (
        [event_id] VARCHAR(36) NOT NULL PRIMARY KEY, [occurred_at] DATETIME2(6) NOT NULL, [direction] VARCHAR(16) NOT NULL,
        [transport] VARCHAR(16) NOT NULL, [local_addr] VARCHAR(255) NOT NULL, [remote_addr] VARCHAR(255) NOT NULL,
        [device_id] VARCHAR(64) NOT NULL, [method] VARCHAR(32) NOT NULL, [status_code] SMALLINT NOT NULL,
        [call_id] VARCHAR(255) NOT NULL, [cseq] INT NOT NULL, [cseq_method] VARCHAR(32) NOT NULL,
        [from_uri] VARCHAR(512) NOT NULL, [to_uri] VARCHAR(512) NOT NULL,
        [from_id] VARCHAR(64) NOT NULL DEFAULT '', [to_id] VARCHAR(64) NOT NULL DEFAULT '',
        [business_code] VARCHAR(64) NOT NULL DEFAULT 'unknown', [business_type] NVARCHAR(64) NOT NULL DEFAULT N'未知业务',
        [business_confidence] VARCHAR(16) NOT NULL DEFAULT 'none', [user_agent] VARCHAR(512) NOT NULL,
        [malformed] BIT NOT NULL DEFAULT 0, [parse_error] VARCHAR(1024) NOT NULL,
        [payload_nonce] VARBINARY(MAX) NOT NULL, [payload_ciphertext] VARBINARY(MAX) NOT NULL,
        [payload_algorithm] VARCHAR(32) NOT NULL, [payload_key_version] VARCHAR(64) NOT NULL,
        [payload_digest_sha256] CHAR(64) NOT NULL
    );
    CREATE INDEX [idx_gb_sip_trace_occurred_event] ON [gb_sip_trace_message] ([occurred_at], [event_id]);
    CREATE INDEX [idx_gb_sip_trace_device_occurred] ON [gb_sip_trace_message] ([device_id], [occurred_at], [event_id]);
    CREATE INDEX [idx_gb_sip_trace_call_occurred] ON [gb_sip_trace_message] ([call_id], [occurred_at], [event_id]);
    CREATE INDEX [idx_gb_sip_trace_business_occurred] ON [gb_sip_trace_message] ([business_code], [occurred_at]);
END;
IF OBJECT_ID(N'gb_sip_trace_session_diagnosis', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_sip_trace_session_diagnosis] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [session_day] DATE NOT NULL,
        [observed_at] DATETIME2(6) NOT NULL,
        [correlation_key] VARCHAR(128) NOT NULL,
        [state] VARCHAR(16) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_state] DEFAULT 'active',
        [category] VARCHAR(32) NOT NULL,
        [code] VARCHAR(64) NOT NULL,
        [stage] VARCHAR(32) NOT NULL,
        [source] VARCHAR(32) NOT NULL,
        [device_id] VARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_device_id] DEFAULT '',
        [channel_id] VARCHAR(64) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_channel_id] DEFAULT '',
        [call_id] VARCHAR(255) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_call_id] DEFAULT '',
        [cseq] INT NOT NULL CONSTRAINT [df_sip_trace_diagnosis_cseq] DEFAULT 0,
        [method] VARCHAR(32) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_method] DEFAULT '',
        [status_code] SMALLINT NOT NULL CONSTRAINT [df_sip_trace_diagnosis_status_code] DEFAULT 0,
        [stream_id] VARCHAR(255) NOT NULL CONSTRAINT [df_sip_trace_diagnosis_stream_id] DEFAULT '',
        [resolved_at] DATETIME2(6) NULL,
        CONSTRAINT [pk_sip_trace_session_diagnosis] PRIMARY KEY ([id]),
        CONSTRAINT [uk_sip_trace_diagnosis_session] UNIQUE ([session_day], [category], [correlation_key])
    );
    CREATE INDEX [idx_sip_trace_diagnosis_category_state_observed]
        ON [gb_sip_trace_session_diagnosis] ([session_day], [category], [state], [observed_at]);
    CREATE INDEX [idx_sip_trace_diagnosis_device_observed]
        ON [gb_sip_trace_session_diagnosis] ([device_id], [observed_at]);
    CREATE INDEX [idx_sip_trace_diagnosis_call_cseq]
        ON [gb_sip_trace_session_diagnosis] ([call_id], [cseq]);
END;
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_control_state_device_target')
    CREATE INDEX [idx_control_state_device_target] ON [gb_device_control_state] ([device_id], [target_scope], [target_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_device_control_state') AND name = N'idx_control_state_channel')
    CREATE INDEX [idx_control_state_channel] ON [gb_device_control_state] ([channel_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_target')
    CREATE INDEX [idx_ptz_operation_target] ON [gb_ptz_operation] ([device_code], [target_scope], [target_code], [status]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_ptz_operation') AND name = N'idx_ptz_operation_device_scope_time')
    CREATE INDEX [idx_ptz_operation_device_scope_time] ON [gb_ptz_operation] ([device_id], [scope_key], [created_at]);

IF OBJECT_ID(N'gb_alarm_resource', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_alarm_resource] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [owner_dept_id] BIGINT NOT NULL,
        [device_id] BIGINT NOT NULL CONSTRAINT [df_full_alarm_resource_device_id] DEFAULT 0,
        [device_code] NVARCHAR(20) NOT NULL,
        [alarm_code] NVARCHAR(20) NOT NULL,
        [resource_type] NVARCHAR(16) NOT NULL,
        [type_code] NVARCHAR(3) NOT NULL,
        [name] NVARCHAR(255) NOT NULL,
        [raw_parent_ids] NVARCHAR(512) NOT NULL CONSTRAINT [df_full_alarm_resource_parents] DEFAULT N'',
        [status] SMALLINT NOT NULL CONSTRAINT [df_full_alarm_resource_status] DEFAULT 0,
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        [deleted_at] DATETIME2(3) NULL,
        CONSTRAINT [pk_full_gb_alarm_resource] PRIMARY KEY ([id]),
        CONSTRAINT [uk_alarm_resource_code] UNIQUE ([owner_dept_id], [device_code], [alarm_code])
    );
END;

IF OBJECT_ID(N'gb_alarm_resource_parent', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_alarm_resource_parent] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [alarm_resource_id] BIGINT NOT NULL,
        [parent_code] NVARCHAR(20) NOT NULL,
        [created_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_full_gb_alarm_resource_parent] PRIMARY KEY ([id]),
        CONSTRAINT [uk_alarm_resource_parent] UNIQUE ([alarm_resource_id], [parent_code])
    );
END;

IF OBJECT_ID(N'gb_alarm_binding', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_alarm_binding] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [device_id] BIGINT NOT NULL,
        [channel_code] NVARCHAR(20) NOT NULL,
        [alarm_resource_id] BIGINT NOT NULL,
        [source] NVARCHAR(16) NOT NULL CONSTRAINT [df_full_alarm_binding_source] DEFAULT N'manual',
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_full_gb_alarm_binding] PRIMARY KEY ([id]),
        CONSTRAINT [uk_alarm_binding_channel] UNIQUE ([device_id], [channel_code])
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_device')
    CREATE INDEX [idx_alarm_resource_device] ON [gb_alarm_resource] ([owner_dept_id], [device_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_device_id')
    CREATE INDEX [idx_alarm_resource_device_id] ON [gb_alarm_resource] ([device_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_alarm_code')
    CREATE INDEX [idx_alarm_resource_alarm_code] ON [gb_alarm_resource] ([alarm_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_type')
    CREATE INDEX [idx_alarm_resource_type] ON [gb_alarm_resource] ([resource_type]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource') AND name = N'idx_alarm_resource_deleted_at')
    CREATE INDEX [idx_alarm_resource_deleted_at] ON [gb_alarm_resource] ([deleted_at]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource_parent') AND name = N'idx_alarm_parent_resource')
    CREATE INDEX [idx_alarm_parent_resource] ON [gb_alarm_resource_parent] ([alarm_resource_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_resource_parent') AND name = N'idx_alarm_parent_code')
    CREATE INDEX [idx_alarm_parent_code] ON [gb_alarm_resource_parent] ([parent_code]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_binding') AND name = N'idx_alarm_binding_device')
    CREATE INDEX [idx_alarm_binding_device] ON [gb_alarm_binding] ([device_id]);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE object_id = OBJECT_ID(N'gb_alarm_binding') AND name = N'idx_alarm_binding_resource')
    CREATE INDEX [idx_alarm_binding_resource] ON [gb_alarm_binding] ([alarm_resource_id]);

IF OBJECT_ID(N'gb_playback_scheme', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_playback_scheme] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [owner_user_id] BIGINT NOT NULL,
        [owner_dept_id] BIGINT NOT NULL,
        [name] NVARCHAR(64) NOT NULL,
        [layout_size] SMALLINT NOT NULL,
        [slot_count] INT NOT NULL CONSTRAINT [df_full_playback_scheme_slot_count] DEFAULT 0,
        [created_by] BIGINT NOT NULL,
        [updated_by] BIGINT NOT NULL,
        [created_at] DATETIME2(3) NOT NULL,
        [updated_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_full_gb_playback_scheme] PRIMARY KEY ([id]),
        CONSTRAINT [uk_playback_scheme_owner_name] UNIQUE ([owner_user_id], [name])
    );
    CREATE INDEX [idx_playback_scheme_owner_updated] ON [gb_playback_scheme] ([owner_user_id], [updated_at]);
    CREATE INDEX [idx_playback_scheme_dept] ON [gb_playback_scheme] ([owner_dept_id]);
END;

IF OBJECT_ID(N'gb_playback_scheme_slot', N'U') IS NULL
BEGIN
    CREATE TABLE [gb_playback_scheme_slot] (
        [id] BIGINT IDENTITY(1,1) NOT NULL,
        [scheme_id] BIGINT NOT NULL,
        [slot_index] INT NOT NULL,
        [device_code] NVARCHAR(20) NOT NULL,
        [channel_code] NVARCHAR(20) NOT NULL,
        [device_name_snapshot] NVARCHAR(255) NOT NULL,
        [channel_name_snapshot] NVARCHAR(255) NOT NULL,
        [created_at] DATETIME2(3) NOT NULL,
        CONSTRAINT [pk_full_gb_playback_scheme_slot] PRIMARY KEY ([id]),
        CONSTRAINT [uk_playback_scheme_slot] UNIQUE ([scheme_id], [slot_index])
    );
    CREATE INDEX [idx_playback_scheme_slot_scheme] ON [gb_playback_scheme_slot] ([scheme_id]);
END;

-- 在线用户会话与权限 seed。
IF OBJECT_ID(N'sys_user_sessions', N'U') IS NULL
BEGIN
    CREATE TABLE [sys_user_sessions] (
        [sid] VARCHAR(36) NOT NULL PRIMARY KEY,
        [user_id] BIGINT NOT NULL,
        [refresh_token_hash] CHAR(64) NULL,
        [refresh_jti] VARCHAR(36) NULL,
        [client_ip] VARCHAR(50) NOT NULL CONSTRAINT [df_user_session_client_ip] DEFAULT '',
        [login_location] NVARCHAR(100) NOT NULL CONSTRAINT [df_user_session_login_location] DEFAULT N'未知',
        [user_agent] NVARCHAR(500) NOT NULL CONSTRAINT [df_user_session_user_agent] DEFAULT N'',
        [browser] NVARCHAR(100) NOT NULL CONSTRAINT [df_user_session_browser] DEFAULT N'未知',
        [os] NVARCHAR(100) NOT NULL CONSTRAINT [df_user_session_os] DEFAULT N'未知',
        [login_at] DATETIME2 NOT NULL,
        [last_active_at] DATETIME2 NOT NULL,
        [session_expires_at] DATETIME2 NOT NULL,
        [revoked_at] DATETIME2 NULL,
        [revoke_reason] VARCHAR(32) NULL,
        [revoked_by] BIGINT NULL,
        [created_at] DATETIME2 NULL,
        [updated_at] DATETIME2 NULL
    );
    CREATE INDEX [idx_user_id] ON [sys_user_sessions] ([user_id]);
    CREATE INDEX [idx_session_valid] ON [sys_user_sessions] ([revoked_at],[session_expires_at],[login_at]);
    CREATE INDEX [idx_client_ip] ON [sys_user_sessions] ([client_ip]);
END;

SET IDENTITY_INSERT [sys_api] ON;
INSERT INTO [sys_api] ([id],[title],[path],[method],[api_group],[created_at],[updated_at],[deleted_at],[created_by]) VALUES
(339,N'查询在线用户','/api/sysOnlineUser/list','GET',N'系统管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(340,N'强制下线会话','/api/sysOnlineUser/forceLogout','POST',N'系统管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
SET IDENTITY_INSERT [sys_api] OFF;
SET IDENTITY_INSERT [sys_menu] ON;
INSERT INTO [sys_menu] ([id],[parent_id],[path],[name],[component],[title],[hide],[disable],[sort],[type],[permission],[icon],[created_at],[updated_at],[created_by]) VALUES
(140382,10,'/system/online-user','SystemOnlineUser','system/online-user/index',N'在线用户',0,0,8,2,'system:online-user:list','lucide:UsersRound',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),
(140383,140382,'','SystemOnlineUserForceLogout','',N'强制下线',1,0,1,3,'system:online-user:force-logout','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
SET IDENTITY_INSERT [sys_menu] OFF;
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) VALUES (1,140382),(1,140383);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) VALUES (140382,339),(140383,340);
SET IDENTITY_INSERT [sys_casbin_rule] ON;
INSERT INTO [sys_casbin_rule] ([id],[ptype],[v0],[v1],[v2],[v3],[v4],[v5]) VALUES
(7806,'p','role_1','/api/sysOnlineUser/list','GET','*','',''),
(7807,'p','role_1','/api/sysOnlineUser/forceLogout','POST','*','','');
SET IDENTITY_INSERT [sys_casbin_rule] OFF;

-- Device permission workbench baseline seed (SQL Server).

-- 设备分配菜单 + 按钮权限 + 角色绑定(SQL Server,幂等)
IF NOT EXISTS (SELECT 1 FROM sys_menu WHERE name='device-assignment' AND deleted_at IS NULL)
BEGIN
    INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,is_full,hide,disable,keep_alive,affix,is_link,link,iframe,svg_icon,icon,sort,type,permission,created_by,created_at,updated_at)
    VALUES (0,'/gb28181/device-assignment','device-assignment','','gb28181/device-assignment/index',N'设备分配',0,0,0,0,0,0,'',0,'','lucide:KeyRound',9,2,'',1,GETDATE(),GETDATE());
END;

IF NOT EXISTS (SELECT 1 FROM sys_menu WHERE name='device-assignment-assign' AND deleted_at IS NULL)
BEGIN
    INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,is_full,hide,disable,keep_alive,affix,is_link,link,iframe,svg_icon,icon,sort,type,permission,created_by,created_at,updated_at)
    SELECT m.id,'','device-assignment-assign','','',N'分配设备归属',0,0,0,0,0,0,'',0,'','',1,3,'gb28181:device:assign',1,GETDATE(),GETDATE()
    FROM sys_menu m WHERE m.name='device-assignment' AND m.deleted_at IS NULL;
END;

IF NOT EXISTS (SELECT 1 FROM sys_menu WHERE name='device-assignment-share' AND deleted_at IS NULL)
BEGIN
    INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,is_full,hide,disable,keep_alive,affix,is_link,link,iframe,svg_icon,icon,sort,type,permission,created_by,created_at,updated_at)
    SELECT m.id,'','device-assignment-share','','',N'共享设备',0,0,0,0,0,0,'',0,'','',2,3,'gb28181:device:share',1,GETDATE(),GETDATE()
    FROM sys_menu m WHERE m.name='device-assignment' AND m.deleted_at IS NULL;
END;

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT rm.role_id, m.id
FROM sys_role_menu rm
JOIN sys_menu src ON src.id=rm.menu_id AND src.name='device-mgmt-list' AND src.deleted_at IS NULL
JOIN sys_menu m ON m.name IN ('device-assignment','device-assignment-assign','device-assignment-share') AND m.deleted_at IS NULL
WHERE NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=rm.role_id AND x.menu_id=m.id);

-- 设备权限工作台 API 权限迁移(SQL Server,幂等)。

UPDATE [sys_menu] SET [title]=N'设备权限工作台', [updated_at]=GETDATE()
WHERE [name]=N'device-assignment' AND [deleted_at] IS NULL;

UPDATE [sys_api] SET [deleted_at]=GETDATE(), [updated_at]=GETDATE()
WHERE [deleted_at] IS NULL AND [path] IN (N'/api/gb28181/device-mgmt/assign',N'/api/gb28181/device-mgmt/assign-dept',N'/api/gb28181/device-mgmt/device/:id/grants',N'/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE ma FROM [sys_menu_api] ma JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE a.[path] IN (N'/api/gb28181/device-mgmt/assign',N'/api/gb28181/device-mgmt/assign-dept',N'/api/gb28181/device-mgmt/device/:id/grants',N'/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM [sys_casbin_rule] WHERE [v1] IN (N'/api/gb28181/device-mgmt/assign',N'/api/gb28181/device-mgmt/assign-dept',N'/api/gb28181/device-mgmt/device/:id/grants',N'/api/gb28181/device-mgmt/device/:id/grants/:grantId');

UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'查询设备权限汇总',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/summary' AND [method]=N'GET';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'查询设备权限汇总',N'/api/gb28181/device-mgmt/permission-workbench/summary',N'GET',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/summary' AND [method]=N'GET' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'解析工作台设备',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'解析工作台设备',N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'查询设备共享授权',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/query' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'查询设备共享授权',N'/api/gb28181/device-mgmt/permission-workbench/grants/query',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/query' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'查询共享目标',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND [method]=N'GET';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'查询共享目标',N'/api/gb28181/device-mgmt/permission-workbench/grant-targets',N'GET',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND [method]=N'GET' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'调整设备归属',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'调整设备归属',N'/api/gb28181/device-mgmt/permission-workbench/assignments',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'整部门调整设备归属',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'整部门调整设备归属',N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND [method]=N'POST' AND [deleted_at] IS NULL);
UPDATE [sys_api] SET [deleted_at]=NULL,[title]=N'应用设备共享授权',[api_group]=N'GB28181设备管理',[updated_at]=GETDATE() WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND [method]=N'POST';
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by]) SELECT N'应用设备共享授权',N'/api/gb28181/device-mgmt/permission-workbench/grants/apply',N'POST',N'GB28181设备管理',GETDATE(),GETDATE(),1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND [method]=N'POST' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON 1=1
WHERE m.[name]=N'device-assignment' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
  AND ((a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/summary' AND a.[method]=N'GET') OR (a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND a.[method]=N'POST') OR (a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/query' AND a.[method]=N'POST') OR (a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND a.[method]=N'GET'))
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON 1=1
WHERE m.[permission]=N'gb28181:device:assign' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[path] IN (N'/api/gb28181/device-mgmt/permission-workbench/assignments',N'/api/gb28181/device-mgmt/permission-workbench/assignments/departments') AND a.[method]=N'POST'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m JOIN [sys_api] a ON 1=1
WHERE m.[permission]=N'gb28181:device:share' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[path]=N'/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND a.[method]=N'POST'
  AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT DISTINCT 'p',CONCAT('role_',rm.[role_id]),a.[path],a.[method],'*','',''
FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id]
JOIN [sys_menu_api] ma ON ma.[menu_id]=m.[id]
JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE m.[name] IN (N'device-assignment',N'device-assignment-assign',N'device-assignment-share') AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
  AND a.[path] LIKE N'/api/gb28181/device-mgmt/permission-workbench/%'
  AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]=CONCAT('role_',rm.[role_id]) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');

-- Cloud recording physical deletion for fresh SQL Server installs.
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT m.[id],'','','',N'删除录像文件',3,'gb28181:recording:delete',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] m WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording:delete' AND [deleted_at] IS NULL);
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.title,v.path,v.method,N'GB28181 云端录像删除',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES (N'删除单个云端录像','/api/gb28181/cloud-recordings/files/:id','DELETE'),(N'批量删除云端录像','/api/gb28181/cloud-recordings/files/batch-delete','POST')) v(title,path,method) WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=v.path AND a.[method]=v.method AND a.[deleted_at] IS NULL);
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission]='gb28181:recording:delete' AND m.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording:delete' AND m.[deleted_at] IS NULL AND ((a.[path]='/api/gb28181/cloud-recordings/files/:id' AND a.[method]='DELETE') OR (a.[path]='/api/gb28181/cloud-recordings/files/batch-delete' AND a.[method]='POST')) AND a.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT 'p','role_1',a.[path],a.[method],'*','','' FROM [sys_api] a WHERE ((a.[path]='/api/gb28181/cloud-recordings/files/:id' AND a.[method]='DELETE') OR (a.[path]='/api/gb28181/cloud-recordings/files/batch-delete' AND a.[method]='POST')) AND a.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_1' AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');

-- Cloud recording stop control for fresh SQL Server installs.
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT m.[id],'','','',N'停止录像',3,'gb28181:recording:stop',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] m WHERE m.[path]='/gb28181/cloud-recordings' AND m.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording:stop' AND [deleted_at] IS NULL);
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT N'停止云端录像','/api/gb28181/cloud-recordings/active/:id/stop','POST',N'GB28181 云端录像控制',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM [sys_api] WHERE [path]='/api/gb28181/cloud-recordings/active/:id/stop' AND [method]='POST' AND [deleted_at] IS NULL);
INSERT INTO [sys_role_menu] ([role_id],[menu_id]) SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission]='gb28181:recording:stop' AND m.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id]) SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording:stop' AND m.[deleted_at] IS NULL AND a.[path]='/api/gb28181/cloud-recordings/active/:id/stop' AND a.[method]='POST' AND a.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5]) SELECT 'p','role_1',a.[path],a.[method],'*','','' FROM [sys_api] a WHERE a.[path]='/api/gb28181/cloud-recordings/active/:id/stop' AND a.[method]='POST' AND a.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_1' AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');

-- Recording plan menus and API permissions for fresh SQL Server installs.
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[icon],[sort],[created_at],[updated_at],[created_by])
SELECT 0,'/gb28181/recording-schedules','gb28181-recording-schedules','gb28181/recording-schedules/index',N'录像计划',2,'gb28181:recording-plan:view','lucide:CalendarClock',34,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording-plan:view' AND [deleted_at] IS NULL);
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'维护录像计划',3,'gb28181:recording-plan:maintain',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p WHERE p.[permission]='gb28181:recording-plan:view' AND p.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording-plan:maintain' AND [deleted_at] IS NULL);
INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'分配录像计划',3,'gb28181:recording-plan:assign',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p WHERE p.[permission]='gb28181:recording-plan:view' AND p.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording-plan:assign' AND [deleted_at] IS NULL);
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT 1,m.[id] FROM [sys_menu] m WHERE m.[permission] IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);
INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT s.[title],s.[path],s.[method],N'GB28181 录像计划',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 (N'查询录像计划','/api/gb28181/recording-plans','GET'),(N'新建录像计划','/api/gb28181/recording-plans','POST'),(N'查看录像计划','/api/gb28181/recording-plans/:id','GET'),
 (N'编辑录像计划','/api/gb28181/recording-plans/:id','PUT'),(N'删除录像计划','/api/gb28181/recording-plans/:id','DELETE'),(N'启停录像计划','/api/gb28181/recording-plans/:id/status','PATCH'),
 (N'搜索分配设备','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),(N'搜索分配通道','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
 (N'分配录像计划','/api/gb28181/recording-plans/:id/assignments','POST'),(N'切换通道录像模式','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
 (N'查询计划通道状态','/api/gb28181/recording-plans/:id/channels','GET'),(N'诊断通道录像','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
 (N'查询通道执行时间线','/api/gb28181/recording-plans/channels/:channelId/timeline','GET')
) s([title],[path],[method]) WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=s.[path] AND a.[method]=s.[method] AND a.[deleted_at] IS NULL);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording-plan:view' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[method]='GET' AND a.[path] IN ('/api/gb28181/recording-plans','/api/gb28181/recording-plans/:id','/api/gb28181/recording-plans/:id/channels','/api/gb28181/recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/timeline') AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording-plan:maintain' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND ((a.[path]='/api/gb28181/recording-plans' AND a.[method]='POST') OR (a.[path]='/api/gb28181/recording-plans/:id' AND a.[method] IN ('PUT','DELETE')) OR (a.[path]='/api/gb28181/recording-plans/:id/status' AND a.[method]='PATCH')) AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT m.[id],a.[id] FROM [sys_menu] m CROSS JOIN [sys_api] a WHERE m.[permission]='gb28181:recording-plan:assign' AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND a.[path] IN ('/api/gb28181/recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignments','/api/gb28181/recording-plans/channels/:channelId/recording-mode') AND NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);
INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT DISTINCT 'p','role_'+CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','','' FROM [sys_role_menu] rm JOIN [sys_menu] m ON m.[id]=rm.[menu_id] JOIN [sys_menu_api] ma ON ma.[menu_id]=m.[id] JOIN [sys_api] a ON a.[id]=ma.[api_id] WHERE m.[permission] IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_'+CAST(rm.[role_id] AS varchar(20)) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');

-- media-management-baseline:start
-- Media management menus and exact backend permission bindings (sqlserver, idempotent).
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[keep_alive],[created_at],[updated_at],[created_by])
SELECT 0,'/media','Media','/gb28181/zlm/overview','',N'流媒体管理','lucide:Clapperboard',9,1,'',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL);
DECLARE @MEDIA_MENU_ID BIGINT;
SELECT TOP 1 @MEDIA_MENU_ID=[id] FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL ORDER BY [id];
UPDATE [sys_menu] SET [redirect]='/gb28181/zlm/overview',[title]=N'流媒体管理',[icon]='lucide:Clapperboard',[sort]=9,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'集群概览',[icon]='lucide:LayoutDashboard',[sort]=10,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/overview','gb28181-zlm-overview','','gb28181/zlm/ClusterOverview',N'集群概览','lucide:LayoutDashboard',10,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'节点管理',[icon]='lucide:Server',[sort]=11,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/nodes' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/nodes','gb28181-zlm-nodes','','gb28181/zlm/NodeList',N'节点管理','lucide:Server',11,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/nodes' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'调度策略',[icon]='lucide:Workflow',[sort]=12,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/scheduler','gb28181-zlm-scheduler-strategy','','gb28181/zlm/SchedulerStrategy',N'调度策略','lucide:Workflow',12,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/scheduler' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'调度日志',[icon]='lucide:History',[sort]=13,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler/logs' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/scheduler/logs','gb28181-zlm-scheduler-log','','gb28181/zlm/SchedulerLog',N'调度日志','lucide:History',13,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/scheduler/logs' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'运行监控',[icon]='lucide:Activity',[sort]=20,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/runtime','gb28181-zlm-runtime','','gb28181/zlm/RuntimeOverview',N'运行监控','lucide:Activity',20,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'流媒体',[icon]='lucide:RadioTower',[sort]=21,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/streams' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/streams','gb28181-zlm-streams','','gb28181/zlm/StreamManagement',N'流媒体','lucide:RadioTower',21,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/streams' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'会话管理',[icon]='lucide:Users',[sort]=22,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/sessions' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/sessions','gb28181-zlm-sessions','','gb28181/zlm/SessionManagement',N'会话管理','lucide:Users',22,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/sessions' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'拉流代理',[icon]='lucide:Network',[sort]=30,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/proxies' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/proxies','gb28181-zlm-proxies','','gb28181/zlm/ProxyManagement',N'拉流代理','lucide:Network',30,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/proxies' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'FFmpeg 源',[icon]='lucide:Clapperboard',[sort]=31,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/ffmpeg-sources' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/ffmpeg-sources','gb28181-zlm-ffmpeg-sources','','gb28181/zlm/FFmpegSources',N'FFmpeg 源','lucide:Clapperboard',31,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/ffmpeg-sources' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'RTP 服务',[icon]='lucide:Waypoints',[sort]=32,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/rtp-servers' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/rtp-servers','gb28181-zlm-rtp-servers','','gb28181/zlm/RTPServices',N'RTP 服务','lucide:Waypoints',32,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/rtp-servers' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'录制管理',[icon]='lucide:Cloud',[sort]=40,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/cloud-recordings' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/cloud-recordings','gb28181-cloud-recordings','','gb28181/cloud-recordings/index',N'录制管理','lucide:Cloud',40,2,'gb28181:recording:view',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/cloud-recordings' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'录像计划',[icon]='lucide:CalendarClock',[sort]=41,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/recording-schedules' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/recording-schedules','gb28181-recording-schedules','','gb28181/recording-schedules/index',N'录像计划','lucide:CalendarClock',41,2,'gb28181:recording-plan:view',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/recording-schedules' AND [deleted_at] IS NULL);

UPDATE [sys_menu] SET [parent_id]=@MEDIA_MENU_ID,[redirect]='',[title]=N'服务配置',[icon]='lucide:Settings2',[sort]=42,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/config' AND [deleted_at] IS NULL;
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT @MEDIA_MENU_ID,'/gb28181/zlm/config','gb28181-zlm-config','','gb28181/zlm/ServerConfig',N'服务配置','lucide:Settings2',42,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE @MEDIA_MENU_ID IS NOT NULL AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/gb28181/zlm/config' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'管理节点',3,'gb28181:zlm:node:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/nodes' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:node:manage' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'踢除节点会话',3,'gb28181:zlm:node:kick',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/nodes' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:node:kick' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'切换调度策略',3,'gb28181:zlm:scheduler:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/scheduler' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:scheduler:manage' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'预览与截图',3,'gb28181:zlm:stream:preview',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/streams' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:stream:preview' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'关闭流',3,'gb28181:zlm:stream:close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/streams' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:stream:close' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'强制关闭流',3,'gb28181:zlm:stream:force-close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/streams' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:stream:force-close' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'踢除会话',3,'gb28181:zlm:session:kick',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/sessions' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:session:kick' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'管理代理',3,'gb28181:zlm:proxy:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/proxies' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:proxy:manage' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'管理 FFmpeg 源',3,'gb28181:zlm:ffmpeg:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/ffmpeg-sources' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:ffmpeg:manage' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'管理 RTP 服务',3,'gb28181:zlm:rtp:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/rtp-servers' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:rtp:manage' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'强制关闭 RTP 服务',3,'gb28181:zlm:rtp:force-close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/rtp-servers' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:rtp:force-close' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'手工录制控制',3,'gb28181:recording:control',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/cloud-recordings' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording:control' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'强制停止录制',3,'gb28181:recording:force-stop',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/cloud-recordings' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:recording:force-stop' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'更新服务配置',3,'gb28181:zlm:config:update',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/config' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:config:update' AND [deleted_at] IS NULL);

INSERT INTO [sys_menu] ([parent_id],[path],[name],[component],[title],[type],[permission],[hide],[created_at],[updated_at],[created_by])
SELECT p.[id],'','','',N'重启媒体服务',3,'gb28181:zlm:restart',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/gb28181/zlm/config' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [permission]='gb28181:zlm:restart' AND [deleted_at] IS NULL);

INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT 1,m.[id] FROM [sys_menu] m
WHERE m.[deleted_at] IS NULL AND (m.[path] IN ('/media','/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config')
OR m.[permission] IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] x WHERE x.[role_id]=1 AND x.[menu_id]=m.[id]);

INSERT INTO [sys_api] ([title],[path],[method],[api_group],[created_at],[updated_at],[created_by])
SELECT v.[title],v.[path],v.[method],N'GB28181 媒体管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
  (N'媒体管理 GET zlm/overview','/api/gb28181/zlm/overview','GET'),
  (N'媒体管理 GET zlm/nodes','/api/gb28181/zlm/nodes','GET'),
  (N'媒体管理 GET zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','GET'),
  (N'媒体管理 GET zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','GET'),
  (N'媒体管理 GET zlm/scheduler','/api/gb28181/zlm/scheduler','GET'),
  (N'媒体管理 GET zlm/scheduler/logs','/api/gb28181/zlm/scheduler/logs','GET'),
  (N'媒体管理 GET zlm/nodes/:id/runtime','/api/gb28181/zlm/nodes/:id/runtime','GET'),
  (N'媒体管理 GET zlm/streams','/api/gb28181/zlm/streams','GET'),
  (N'媒体管理 GET zlm/nodes/:id/streams','/api/gb28181/zlm/nodes/:id/streams','GET'),
  (N'媒体管理 GET zlm/nodes/:id/streams/detail','/api/gb28181/zlm/nodes/:id/streams/detail','GET'),
  (N'媒体管理 GET zlm/nodes/:id/streams/viewers','/api/gb28181/zlm/nodes/:id/streams/viewers','GET'),
  (N'媒体管理 GET zlm/nodes/:id/sessions/network','/api/gb28181/zlm/nodes/:id/sessions/network','GET'),
  (N'媒体管理 GET zlm/nodes/:id/sessions/viewers','/api/gb28181/zlm/nodes/:id/sessions/viewers','GET'),
  (N'媒体管理 GET zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','GET'),
  (N'媒体管理 GET zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','GET'),
  (N'媒体管理 GET zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','GET'),
  (N'媒体管理 GET zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','GET'),
  (N'媒体管理 GET zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','GET'),
  (N'媒体管理 GET zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','GET'),
  (N'媒体管理 GET cloud-recordings/files','/api/gb28181/cloud-recordings/files','GET'),
  (N'媒体管理 GET cloud-recordings/files/options','/api/gb28181/cloud-recordings/files/options','GET'),
  (N'媒体管理 GET cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','GET'),
  (N'媒体管理 POST cloud-recordings/files/:id/access','/api/gb28181/cloud-recordings/files/:id/access','POST'),
  (N'媒体管理 POST cloud-recordings/files/:id/downloads','/api/gb28181/cloud-recordings/files/:id/downloads','POST'),
  (N'媒体管理 GET cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','GET'),
  (N'媒体管理 DELETE cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'),
  (N'媒体管理 GET cloud-recordings/active','/api/gb28181/cloud-recordings/active','GET'),
  (N'媒体管理 GET zlm/nodes/:id/recordings/runtime/status','/api/gb28181/zlm/nodes/:id/recordings/runtime/status','GET'),
  (N'媒体管理 GET cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','GET'),
  (N'媒体管理 POST cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','POST'),
  (N'媒体管理 POST cloud-recordings/files/batch-delete','/api/gb28181/cloud-recordings/files/batch-delete','POST'),
  (N'媒体管理 DELETE cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','DELETE'),
  (N'媒体管理 POST cloud-recordings/active/:id/stop','/api/gb28181/cloud-recordings/active/:id/stop','POST'),
  (N'媒体管理 GET recording-plans','/api/gb28181/recording-plans','GET'),
  (N'媒体管理 GET recording-plans/:id','/api/gb28181/recording-plans/:id','GET'),
  (N'媒体管理 GET recording-plans/:id/channels','/api/gb28181/recording-plans/:id/channels','GET'),
  (N'媒体管理 GET recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
  (N'媒体管理 GET recording-plans/channels/:channelId/timeline','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'),
  (N'媒体管理 POST recording-plans','/api/gb28181/recording-plans','POST'),
  (N'媒体管理 PUT recording-plans/:id','/api/gb28181/recording-plans/:id','PUT'),
  (N'媒体管理 DELETE recording-plans/:id','/api/gb28181/recording-plans/:id','DELETE'),
  (N'媒体管理 PATCH recording-plans/:id/status','/api/gb28181/recording-plans/:id/status','PATCH'),
  (N'媒体管理 PATCH recording-plans/channels/:channelId/recording-mode','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
  (N'媒体管理 GET recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),
  (N'媒体管理 GET recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
  (N'媒体管理 POST recording-plans/:id/assignments','/api/gb28181/recording-plans/:id/assignments','POST'),
  (N'媒体管理 POST zlm/nodes','/api/gb28181/zlm/nodes','POST'),
  (N'媒体管理 PUT zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','PUT'),
  (N'媒体管理 DELETE zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','DELETE'),
  (N'媒体管理 POST zlm/nodes/:id/maintenance','/api/gb28181/zlm/nodes/:id/maintenance','POST'),
  (N'媒体管理 POST zlm/nodes/:id/activate','/api/gb28181/zlm/nodes/:id/activate','POST'),
  (N'媒体管理 POST zlm/nodes/:id/kick','/api/gb28181/zlm/nodes/:id/kick','POST'),
  (N'媒体管理 PUT zlm/scheduler','/api/gb28181/zlm/scheduler','PUT'),
  (N'媒体管理 POST zlm/nodes/:id/streams/playback-grant','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST'),
  (N'媒体管理 GET zlm/nodes/:id/streams/snapshot','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET'),
  (N'媒体管理 POST zlm/nodes/:id/streams/close/preflight','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST'),
  (N'媒体管理 POST zlm/nodes/:id/streams/close','/api/gb28181/zlm/nodes/:id/streams/close','POST'),
  (N'媒体管理 POST zlm/nodes/:id/streams/close/batch/preflight','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST'),
  (N'媒体管理 POST zlm/nodes/:id/streams/close/batch','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST'),
  (N'媒体管理 POST zlm/nodes/:id/streams/force-close','/api/gb28181/zlm/nodes/:id/streams/force-close','POST'),
  (N'媒体管理 POST zlm/nodes/:id/sessions/kick','/api/gb28181/zlm/nodes/:id/sessions/kick','POST'),
  (N'媒体管理 POST zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','POST'),
  (N'媒体管理 POST zlm/nodes/:id/proxies/pull/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST'),
  (N'媒体管理 DELETE zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE'),
  (N'媒体管理 POST zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','POST'),
  (N'媒体管理 POST zlm/nodes/:id/proxies/push/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST'),
  (N'媒体管理 DELETE zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE'),
  (N'媒体管理 POST zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST'),
  (N'媒体管理 POST zlm/nodes/:id/ffmpeg-sources/:key/preflight','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST'),
  (N'媒体管理 DELETE zlm/nodes/:id/ffmpeg-sources/:key','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE'),
  (N'媒体管理 POST zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','POST'),
  (N'媒体管理 POST zlm/nodes/:id/rtp-servers/close/preflight','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST'),
  (N'媒体管理 POST zlm/nodes/:id/rtp-servers/close','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST'),
  (N'媒体管理 POST zlm/nodes/:id/rtp-servers/force-close','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST'),
  (N'媒体管理 POST zlm/nodes/:id/recordings/runtime/start/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST'),
  (N'媒体管理 POST zlm/nodes/:id/recordings/runtime/start','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST'),
  (N'媒体管理 POST zlm/nodes/:id/recordings/runtime/stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST'),
  (N'媒体管理 POST zlm/nodes/:id/recordings/runtime/stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST'),
  (N'媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST'),
  (N'媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST'),
  (N'媒体管理 PUT zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','PUT'),
  (N'媒体管理 POST zlm/nodes/:id/config/test-connection','/api/gb28181/zlm/nodes/:id/config/test-connection','POST'),
  (N'媒体管理 GET zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','GET'),
  (N'媒体管理 POST zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','POST')
) v([title],[path],[method])
WHERE NOT EXISTS (SELECT 1 FROM [sys_api] a WHERE a.[path]=v.[path] AND a.[method]=v.[method] AND a.[deleted_at] IS NULL);

INSERT INTO [sys_menu_api] ([menu_id],[api_id])
SELECT DISTINCT m.[id],a.[id] FROM (VALUES
  ('path','/gb28181/zlm/overview','/api/gb28181/zlm/overview','GET'),
  ('path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id','GET'),
  ('path','/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/config','GET'),
  ('path','/gb28181/zlm/scheduler','/api/gb28181/zlm/scheduler','GET'),
  ('path','/gb28181/zlm/scheduler/logs','/api/gb28181/zlm/scheduler/logs','GET'),
  ('path','/gb28181/zlm/scheduler/logs','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/runtime','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/runtime','/api/gb28181/zlm/nodes/:id/runtime','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/streams','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams/detail','GET'),
  ('path','/gb28181/zlm/streams','/api/gb28181/zlm/nodes/:id/streams/viewers','GET'),
  ('path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes/:id/sessions/network','GET'),
  ('path','/gb28181/zlm/sessions','/api/gb28181/zlm/nodes/:id/sessions/viewers','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/pull','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/push','GET'),
  ('path','/gb28181/zlm/proxies','/api/gb28181/zlm/nodes/:id/proxies/push/:key','GET'),
  ('path','/gb28181/zlm/ffmpeg-sources','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','GET'),
  ('path','/gb28181/zlm/rtp-servers','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','GET'),
  ('path','/gb28181/zlm/config','/api/gb28181/zlm/nodes','GET'),
  ('path','/gb28181/zlm/config','/api/gb28181/zlm/nodes/:id/config','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/options','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id/access','POST'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/files/:id/downloads','POST'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/downloads/:taskId','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'),
  ('permission','gb28181:recording:view','/api/gb28181/cloud-recordings/active','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/zlm/nodes','GET'),
  ('permission','gb28181:recording:view','/api/gb28181/zlm/nodes/:id/recordings/runtime/status','GET'),
  ('permission','gb28181:recording:reconcile','/api/gb28181/cloud-recordings/reconciliations','GET'),
  ('permission','gb28181:recording:reconcile','/api/gb28181/cloud-recordings/reconciliations','POST'),
  ('permission','gb28181:recording:delete','/api/gb28181/cloud-recordings/files/batch-delete','POST'),
  ('permission','gb28181:recording:delete','/api/gb28181/cloud-recordings/files/:id','DELETE'),
  ('permission','gb28181:recording:stop','/api/gb28181/cloud-recordings/active/:id/stop','POST'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/:id','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/:id/channels','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
  ('permission','gb28181:recording-plan:view','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans','POST'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id','PUT'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id','DELETE'),
  ('permission','gb28181:recording-plan:maintain','/api/gb28181/recording-plans/:id/status','PATCH'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
  ('permission','gb28181:recording-plan:assign','/api/gb28181/recording-plans/:id/assignments','POST'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes','POST'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id','PUT'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id','DELETE'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id/maintenance','POST'),
  ('permission','gb28181:zlm:node:manage','/api/gb28181/zlm/nodes/:id/activate','POST'),
  ('permission','gb28181:zlm:node:kick','/api/gb28181/zlm/nodes/:id/kick','POST'),
  ('permission','gb28181:zlm:scheduler:manage','/api/gb28181/zlm/scheduler','PUT'),
  ('permission','gb28181:zlm:stream:preview','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST'),
  ('permission','gb28181:zlm:stream:preview','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close','POST'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST'),
  ('permission','gb28181:zlm:stream:close','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST'),
  ('permission','gb28181:zlm:stream:force-close','/api/gb28181/zlm/nodes/:id/streams/force-close','POST'),
  ('permission','gb28181:zlm:session:kick','/api/gb28181/zlm/nodes/:id/sessions/kick','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST'),
  ('permission','gb28181:zlm:proxy:manage','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE'),
  ('permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST'),
  ('permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST'),
  ('permission','gb28181:zlm:ffmpeg:manage','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE'),
  ('permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers','POST'),
  ('permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST'),
  ('permission','gb28181:zlm:rtp:manage','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST'),
  ('permission','gb28181:zlm:rtp:force-close','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST'),
  ('permission','gb28181:recording:control','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST'),
  ('permission','gb28181:recording:force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST'),
  ('permission','gb28181:recording:force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST'),
  ('permission','gb28181:zlm:config:update','/api/gb28181/zlm/nodes/:id/config','PUT'),
  ('permission','gb28181:zlm:config:update','/api/gb28181/zlm/nodes/:id/config/test-connection','POST'),
  ('permission','gb28181:zlm:restart','/api/gb28181/zlm/nodes/:id/restart','GET'),
  ('permission','gb28181:zlm:restart','/api/gb28181/zlm/nodes/:id/restart','POST')
) b([selector_type],[selector],[api_path],[method])
JOIN [sys_menu] m ON ((b.[selector_type]='path' AND m.[path]=b.[selector]) OR (b.[selector_type]='permission' AND m.[permission]=b.[selector])) AND m.[deleted_at] IS NULL
JOIN [sys_api] a ON a.[path]=b.[api_path] AND a.[method]=b.[method] AND a.[deleted_at] IS NULL
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu_api] x WHERE x.[menu_id]=m.[id] AND x.[api_id]=a.[id]);

INSERT INTO [sys_casbin_rule] ([ptype],[v0],[v1],[v2],[v3],[v4],[v5])
SELECT DISTINCT 'p','role_'+CAST(rm.[role_id] AS varchar(20)),a.[path],a.[method],'*','','' FROM [sys_role_menu] rm
JOIN [sys_menu] m ON m.[id]=rm.[menu_id]
JOIN [sys_menu_api] ma ON ma.[menu_id]=m.[id]
JOIN [sys_api] a ON a.[id]=ma.[api_id]
WHERE m.[deleted_at] IS NULL AND a.[deleted_at] IS NULL
AND (m.[path] IN ('/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config') OR m.[permission] IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND (a.[path] LIKE '/api/gb28181/zlm/%' OR a.[path] LIKE '/api/gb28181/cloud-recordings/%' OR a.[path] LIKE '/api/gb28181/recording-plans%')
AND NOT EXISTS (SELECT 1 FROM [sys_casbin_rule] c WHERE c.[ptype]='p' AND c.[v0]='role_'+CAST(rm.[role_id] AS varchar(20)) AND c.[v1]=a.[path] AND c.[v2]=a.[method] AND c.[v3]='*');
-- media-management-baseline:end

-- media-workbench-v2:start
-- Flatten media management into six visible workspaces while preserving legacy permission anchors (SQL Server).
INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT 0,'/media','Media','','gb28181/zlm/workbench/MediaEntry',N'流媒体管理','lucide:Clapperboard',9,1,'',0,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=0,[name]='Media',[redirect]='',[component]='gb28181/zlm/workbench/MediaEntry',[title]=N'流媒体管理',[icon]='lucide:Clapperboard',[sort]=9,[type]=1,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media' AND [deleted_at] IS NULL;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT p.[id],'/media/overview','media-overview','','gb28181/zlm/workbench/MediaOverview',N'媒体总览','lucide:LayoutDashboard',10,2,'',0,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/media' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media/overview' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='media-overview',[redirect]='',[component]='gb28181/zlm/workbench/MediaOverview',[title]=N'媒体总览',[icon]='lucide:LayoutDashboard',[sort]=10,[type]=2,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media/overview' AND [deleted_at] IS NULL;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT p.[id],'/media/monitoring','media-monitoring','','gb28181/zlm/workbench/MediaMonitoring',N'媒体监控','lucide:Activity',20,2,'',0,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/media' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media/monitoring' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='media-monitoring',[redirect]='',[component]='gb28181/zlm/workbench/MediaMonitoring',[title]=N'媒体监控',[icon]='lucide:Activity',[sort]=20,[type]=2,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media/monitoring' AND [deleted_at] IS NULL;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT p.[id],'/media/ingress','media-ingress','','gb28181/zlm/workbench/IngressManagement',N'接入管理','lucide:RadioTower',30,2,'',0,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/media' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media/ingress' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='media-ingress',[redirect]='',[component]='gb28181/zlm/workbench/IngressManagement',[title]=N'接入管理',[icon]='lucide:RadioTower',[sort]=30,[type]=2,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media/ingress' AND [deleted_at] IS NULL;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT p.[id],'/media/recordings','media-recordings','','gb28181/zlm/workbench/RecordingCenter',N'录制中心','lucide:Cloud',40,2,'',0,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/media' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media/recordings' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='media-recordings',[redirect]='',[component]='gb28181/zlm/workbench/RecordingCenter',[title]=N'录制中心',[icon]='lucide:Cloud',[sort]=40,[type]=2,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media/recordings' AND [deleted_at] IS NULL;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT p.[id],'/media/nodes','media-nodes','','gb28181/zlm/workbench/NodeManagement',N'节点管理','lucide:Server',50,2,'',0,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/media' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media/nodes' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='media-nodes',[redirect]='',[component]='gb28181/zlm/workbench/NodeManagement',[title]=N'节点管理',[icon]='lucide:Server',[sort]=50,[type]=2,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media/nodes' AND [deleted_at] IS NULL;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT p.[id],'/media/scheduling','media-scheduling','','gb28181/zlm/workbench/SchedulingManagement',N'调度管理','lucide:Workflow',60,2,'',0,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/media' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media/scheduling' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[name]='media-scheduling',[redirect]='',[component]='gb28181/zlm/workbench/SchedulingManagement',[title]=N'调度管理',[icon]='lucide:Workflow',[sort]=60,[type]=2,[permission]='',[hide]=0,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media/scheduling' AND [deleted_at] IS NULL;

INSERT INTO [sys_menu] ([parent_id],[path],[name],[redirect],[component],[title],[icon],[sort],[type],[permission],[hide],[keep_alive],[created_at],[updated_at],[created_by])
SELECT p.[id],'/media/nodes/:id','media-node-detail','','gb28181/zlm/workbench/nodes/NodeDetail',N'节点详情','lucide:Server',99,2,'',1,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM [sys_menu] p
WHERE p.[path]='/media/nodes' AND p.[deleted_at] IS NULL
AND NOT EXISTS (SELECT 1 FROM [sys_menu] WHERE [path]='/media/nodes/:id' AND [deleted_at] IS NULL);
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media/nodes' AND [deleted_at] IS NULL),[name]='media-node-detail',[redirect]='',[component]='gb28181/zlm/workbench/nodes/NodeDetail',[title]=N'节点详情',[icon]='lucide:Server',[sort]=99,[type]=2,[permission]='',[hide]=1,[keep_alive]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/media/nodes/:id' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/streams' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/sessions' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/proxies' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/ffmpeg-sources' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/rtp-servers' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/cloud-recordings' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/recording-schedules' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/nodes' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/nodes/:id' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/config' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler' AND [deleted_at] IS NULL;

UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP
WHERE [path]='/gb28181/zlm/scheduler/logs' AND [deleted_at] IS NULL;

-- workspace-role-union:/media
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- workspace-role-union:/media/overview
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media/overview' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/zlm/overview')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- workspace-role-union:/media/monitoring
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media/monitoring' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- workspace-role-union:/media/ingress
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media/ingress' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- workspace-role-union:/media/recordings
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media/recordings' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/cloud-recordings','/gb28181/recording-schedules')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- workspace-role-union:/media/nodes
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media/nodes' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- workspace-role-union:/media/nodes/:id
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media/nodes/:id' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- workspace-role-union:/media/scheduling
INSERT INTO [sys_role_menu] ([role_id],[menu_id])
SELECT DISTINCT rm.[role_id],target.[id]
FROM [sys_role_menu] rm
JOIN [sys_menu] source ON source.[id]=rm.[menu_id] AND source.[deleted_at] IS NULL
JOIN [sys_menu] target ON target.[path]='/media/scheduling' AND target.[deleted_at] IS NULL
WHERE source.[path] IN ('/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM [sys_role_menu] existing WHERE existing.[role_id]=rm.[role_id] AND existing.[menu_id]=target.[id]);

-- media-workbench-v2:end
-- zlm-admin-parity-v3:start
-- Restore zlm-admin-style direct pages and keep GB28181 recordings independent (SQL Server).
UPDATE [sys_menu] SET [redirect]='/gb28181/zlm/overview',[component]='',[title]=N'流媒体管理',[icon]='lucide:Clapperboard',[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/media' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/ClusterOverview',[title]=N'集群总览',[icon]='lucide:LayoutDashboard',[sort]=10,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/overview' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/NodeList',[title]=N'节点管理',[icon]='lucide:Server',[sort]=20,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/nodes' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/RuntimeOverview',[title]=N'总览',[icon]='lucide:Gauge',[sort]=30,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/runtime' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/StreamManagement',[title]=N'流管理',[icon]='lucide:RadioTower',[sort]=40,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/streams' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/SessionManagement',[title]=N'会话管理',[icon]='lucide:Users',[sort]=50,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/sessions' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/ProxyManagement',[title]=N'拉流/推流代理',[icon]='lucide:Network',[sort]=60,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/proxies' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/FFmpegSources',[title]=N'FFmpeg 源',[icon]='lucide:Clapperboard',[sort]=70,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/ffmpeg-sources' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/RTPServices',[title]=N'RTP 服务',[icon]='lucide:Waypoints',[sort]=80,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/rtp-servers' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/ServerConfig',[title]=N'服务器配置',[icon]='lucide:Settings2',[sort]=90,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/config' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/SchedulerStrategy',[title]=N'调度策略',[icon]='lucide:Workflow',[sort]=100,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/scheduler' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT MIN([id]) FROM [sys_menu] WHERE [path]='/media' AND [deleted_at] IS NULL),[component]='gb28181/zlm/SchedulerLog',[title]=N'调度日志',[icon]='lucide:History',[sort]=110,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/scheduler/logs' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=0,[component]='gb28181/zlm/NodeDetail',[title]=N'节点详情',[hide]=1,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/zlm/nodes/:id' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT TOP 1 dm.[parent_id] FROM [sys_menu] dm WHERE dm.[path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND dm.[deleted_at] IS NULL ORDER BY CASE WHEN dm.[path]='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,dm.[id]),[component]='gb28181/cloud-recordings/index',[title]=N'云端录像',[icon]='lucide:Cloud',[sort]=35,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/cloud-recordings' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [parent_id]=(SELECT TOP 1 dm.[parent_id] FROM [sys_menu] dm WHERE dm.[path] IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND dm.[deleted_at] IS NULL ORDER BY CASE WHEN dm.[path]='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,dm.[id]),[component]='gb28181/recording-schedules/index',[title]=N'录像计划',[icon]='lucide:CalendarClock',[sort]=36,[hide]=0,[updated_at]=CURRENT_TIMESTAMP WHERE [path]='/gb28181/recording-schedules' AND [deleted_at] IS NULL;
UPDATE [sys_menu] SET [component]='gb28181/zlm/workbench/LegacyMediaRoute',[hide]=1,[updated_at]=CURRENT_TIMESTAMP WHERE [path] IN ('/media/overview','/media/monitoring','/media/ingress','/media/recordings','/media/nodes','/media/scheduling','/media/nodes/:id') AND [deleted_at] IS NULL;
-- zlm-admin-parity-v3:end
