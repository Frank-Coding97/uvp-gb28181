-- PostgreSQL SQL 转换文件
-- 由 MySQL SQL 转换而来

SET session_replication_role = replica;
SET client_min_messages TO WARNING;

DROP TABLE IF EXISTS gb_zlm_managed_resource;
CREATE TABLE gb_zlm_managed_resource (
  id BIGSERIAL PRIMARY KEY,
  node_id BIGINT NOT NULL,
  resource_type VARCHAR(32) NOT NULL,
  resource_key VARCHAR(255) NOT NULL,
  app VARCHAR(64) NOT NULL DEFAULT '',
  stream VARCHAR(255) NOT NULL DEFAULT '',
  identity_fingerprint CHAR(64) NOT NULL,
  summary VARCHAR(512) NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL,
  last_observed_at TIMESTAMP(3),
  tombstoned_at TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL,
  CONSTRAINT uk_gb_zlm_managed_resource_identity UNIQUE (node_id,resource_type,resource_key)
);
CREATE INDEX idx_gb_zlm_managed_resource_observed ON gb_zlm_managed_resource(node_id,last_observed_at);
CREATE INDEX idx_gb_zlm_managed_resource_tombstone ON gb_zlm_managed_resource(node_id,tombstoned_at);

DO $$ BEGIN IF to_regclass('public.gb_channel') IS NOT NULL THEN ALTER TABLE gb_channel ADD COLUMN IF NOT EXISTS recording_mode VARCHAR(16) NOT NULL DEFAULT 'off'; END IF; END $$;
DROP TABLE IF EXISTS gb_recording_plan_gap;
CREATE TABLE gb_recording_plan_gap (id BIGSERIAL PRIMARY KEY, plan_id BIGINT, channel_id BIGINT NOT NULL, started_at TIMESTAMP(3) NOT NULL, ended_at TIMESTAMP(3), duration_ms BIGINT NOT NULL DEFAULT 0, reason_code VARCHAR(64) NOT NULL, reason_message VARCHAR(500) NOT NULL DEFAULT '', recovered BOOLEAN NOT NULL DEFAULT FALSE, execution_id BIGINT, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL);
DROP TABLE IF EXISTS gb_recording_plan_execution;
CREATE TABLE gb_recording_plan_execution (id BIGSERIAL PRIMARY KEY, plan_id BIGINT, channel_id BIGINT NOT NULL, device_id VARCHAR(20) NOT NULL DEFAULT '', action VARCHAR(32) NOT NULL, trigger_source VARCHAR(32) NOT NULL, stage VARCHAR(32) NOT NULL DEFAULT '', attempt INTEGER NOT NULL DEFAULT 1, result VARCHAR(24) NOT NULL, reason_code VARCHAR(64) NOT NULL DEFAULT '', reason_message VARCHAR(500) NOT NULL DEFAULT '', stream_id VARCHAR(64) NOT NULL DEFAULT '', node_id VARCHAR(64) NOT NULL DEFAULT '', recording_session_id BIGINT, generation BIGINT NOT NULL DEFAULT 0, started_at TIMESTAMP(3) NOT NULL, ended_at TIMESTAMP(3), duration_ms BIGINT NOT NULL DEFAULT 0, created_at TIMESTAMP(3) NOT NULL);
DROP TABLE IF EXISTS gb_recording_plan_channel_state;
CREATE TABLE gb_recording_plan_channel_state (channel_id BIGINT PRIMARY KEY, plan_id BIGINT, plan_version BIGINT NOT NULL DEFAULT 0, recorder_owner_kind VARCHAR(20) NOT NULL DEFAULT '', recorder_owner_id VARCHAR(128) NOT NULL DEFAULT '', recorder_claim_version BIGINT NOT NULL DEFAULT 0, desired_state VARCHAR(24) NOT NULL, actual_state VARCHAR(32) NOT NULL, reason_code VARCHAR(64) NOT NULL DEFAULT '', reason_message VARCHAR(500) NOT NULL DEFAULT '', next_transition_at TIMESTAMP(3), next_retry_at TIMESTAMP(3), reconcile_at TIMESTAMP(3) NOT NULL, attempt_count INTEGER NOT NULL DEFAULT 0, generation BIGINT NOT NULL DEFAULT 0, stream_id VARCHAR(64) NOT NULL DEFAULT '', recording_session_id BIGINT, node_id VARCHAR(64) NOT NULL DEFAULT '', last_media_at TIMESTAMP(3), last_success_at TIMESTAMP(3), lease_owner VARCHAR(128) NOT NULL DEFAULT '', lease_until TIMESTAMP(3), state_version BIGINT NOT NULL DEFAULT 0, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL);
CREATE INDEX idx_recording_plan_state_reconcile ON gb_recording_plan_channel_state(reconcile_at,channel_id);
DROP TABLE IF EXISTS gb_recording_plan_binding;
CREATE TABLE gb_recording_plan_binding (id BIGSERIAL PRIMARY KEY, plan_id BIGINT NOT NULL, channel_id BIGINT NOT NULL, owner_dept_id BIGINT NOT NULL, assigned_by BIGINT NOT NULL DEFAULT 0, assigned_at TIMESTAMP(3) NOT NULL, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL, CONSTRAINT uk_recording_plan_binding_channel UNIQUE(channel_id));
DROP TABLE IF EXISTS gb_recording_plan_period;
CREATE TABLE gb_recording_plan_period (id BIGSERIAL PRIMARY KEY, plan_id BIGINT NOT NULL, weekday SMALLINT NOT NULL, start_slot SMALLINT NOT NULL, end_slot SMALLINT NOT NULL, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL);
DROP TABLE IF EXISTS gb_recording_plan;
CREATE TABLE gb_recording_plan (id BIGSERIAL PRIMARY KEY, name VARCHAR(128) NOT NULL, description VARCHAR(500) NOT NULL DEFAULT '', status SMALLINT NOT NULL DEFAULT 1, version BIGINT NOT NULL DEFAULT 1, owner_dept_id BIGINT NOT NULL, created_by BIGINT NOT NULL DEFAULT 0, updated_by BIGINT NOT NULL DEFAULT 0, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL, deleted_at TIMESTAMP(3));

DROP TABLE IF EXISTS gb_channel_favorite_item;
CREATE TABLE gb_channel_favorite_item (id BIGSERIAL PRIMARY KEY, group_id BIGINT NOT NULL, device_code VARCHAR(64) NOT NULL, channel_code VARCHAR(64) NOT NULL, device_name VARCHAR(255) NOT NULL DEFAULT '', channel_name VARCHAR(255) NOT NULL DEFAULT '', created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL);
CREATE UNIQUE INDEX uk_gb_channel_favorite_item_code ON gb_channel_favorite_item(group_id,device_code,channel_code);
CREATE INDEX idx_gb_channel_favorite_item_group ON gb_channel_favorite_item(group_id);
DROP TABLE IF EXISTS gb_channel_favorite_group;
CREATE TABLE gb_channel_favorite_group (id BIGSERIAL PRIMARY KEY, owner_user_id BIGINT NOT NULL, name VARCHAR(64) NOT NULL, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL);
CREATE UNIQUE INDEX uk_gb_channel_favorite_group_owner_name ON gb_channel_favorite_group(owner_user_id,name);
CREATE INDEX idx_gb_channel_favorite_group_owner ON gb_channel_favorite_group(owner_user_id);
DROP TABLE IF EXISTS gb_custom_group_device;
CREATE TABLE gb_custom_group_device (
    id BIGSERIAL PRIMARY KEY, group_id BIGINT NOT NULL, device_id BIGINT NOT NULL,
    created_by BIGINT NOT NULL, created_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_custom_group_device UNIQUE (group_id, device_id)
);
CREATE INDEX idx_custom_group_device_group ON gb_custom_group_device (group_id);
CREATE INDEX idx_custom_group_device_device ON gb_custom_group_device (device_id);

DROP TABLE IF EXISTS gb_custom_group;
CREATE TABLE gb_custom_group (
    id BIGSERIAL PRIMARY KEY, owner_dept_id BIGINT NOT NULL,
    parent_id BIGINT NOT NULL DEFAULT 0, path VARCHAR(1024) NOT NULL,
    depth SMALLINT NOT NULL DEFAULT 0, name VARCHAR(64) NOT NULL,
    created_by BIGINT NOT NULL, created_at TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_custom_group_sibling_name UNIQUE (owner_dept_id, parent_id, name)
);
CREATE INDEX idx_custom_group_parent ON gb_custom_group (parent_id);
CREATE INDEX idx_custom_group_dept_path ON gb_custom_group (owner_dept_id, path);

-- Table structure for demo_students
DROP TABLE IF EXISTS demo_students;
CREATE TABLE demo_students (
    student_id SERIAL,
    student_name VARCHAR(50) NOT NULL,
    age INTEGER NOT NULL DEFAULT 18,
    gender VARCHAR(50) NOT NULL DEFAULT '',
    class_name VARCHAR(20) NOT NULL,
    admission_date TIMESTAMP NOT NULL,
    email VARCHAR(100),
    phone VARCHAR(20),
    address TEXT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER DEFAULT 0,
    PRIMARY KEY (student_id)
);

COMMENT ON COLUMN demo_students.student_name IS '姓名';
COMMENT ON COLUMN demo_students.age IS '年龄';
COMMENT ON COLUMN demo_students.gender IS '性别';
COMMENT ON COLUMN demo_students.admission_date IS '入学日期';
COMMENT ON COLUMN demo_students.email IS ' 邮箱';
COMMENT ON COLUMN demo_students.phone IS '电话号码';
COMMENT ON COLUMN demo_students.created_at IS '创建时间';
COMMENT ON COLUMN demo_students.updated_at IS '更新时间';
COMMENT ON COLUMN demo_students.class_name IS '班级名称';
COMMENT ON COLUMN demo_students.address IS '地址';
COMMENT ON COLUMN demo_students.deleted_at IS '删除时间';
COMMENT ON COLUMN demo_students.created_by IS '创建人';

-- Records of demo_students
-- Table structure for demo_teacher
DROP TABLE IF EXISTS demo_teacher;
CREATE TABLE demo_teacher (
    id SERIAL,
    name VARCHAR(50) NOT NULL,
    employee_id VARCHAR(20),
    gender BOOLEAN DEFAULT false,
    phone VARCHAR(20),
    email VARCHAR(100),
    subject VARCHAR(50),
    title VARCHAR(50),
    status BOOLEAN DEFAULT true,
    hire_date DATE,
    birth_date DATE,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER DEFAULT 0,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN demo_teacher.phone IS '手机号';
COMMENT ON COLUMN demo_teacher.status IS '状态：0-离职 1-在职';
COMMENT ON COLUMN demo_teacher.hire_date IS '入职日期';
COMMENT ON COLUMN demo_teacher.updated_at IS '更新时间';
COMMENT ON COLUMN demo_teacher.created_by IS '创建人';
COMMENT ON COLUMN demo_teacher.id IS '主键ID';
COMMENT ON COLUMN demo_teacher.title IS '职称';
COMMENT ON COLUMN demo_teacher.birth_date IS '出生日期';
COMMENT ON COLUMN demo_teacher.deleted_at IS '删除时间';
COMMENT ON COLUMN demo_teacher.gender IS '性别：0-未知 1-男 2-女';
COMMENT ON COLUMN demo_teacher.email IS '邮箱';
COMMENT ON COLUMN demo_teacher.subject IS '所教学科';
COMMENT ON COLUMN demo_teacher.created_at IS '创建时间';
COMMENT ON COLUMN demo_teacher.name IS '教师姓名';
COMMENT ON COLUMN demo_teacher.employee_id IS '工号';

-- Records of demo_teacher
-- Table structure for example
DROP TABLE IF EXISTS example;
CREATE TABLE example (
    id SERIAL,
    name VARCHAR(255) NOT NULL,
    description VARCHAR(255),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN example.name IS '名称';
COMMENT ON COLUMN example.description IS '描述';

-- Records of example
INSERT INTO example VALUES (1, '项目管理系统', '用于管理项目进度和任务分配的系统', '2024-01-15 09:30:00', '2024-01-20 14:25:00', NULL, 1);
INSERT INTO example VALUES (2, '客户关系管理', '帮助企业维护客户关系的软件平台', '2024-01-16 10:15:00', '2024-01-22 11:40:00', NULL, 1);
INSERT INTO example VALUES (3, '财务分析工具', '提供财务报表和数据分析功能', '2024-01-17 14:20:00', '2024-01-25 16:30:00', NULL, 1);
INSERT INTO example VALUES (4, '库存管理系统', '实时跟踪和管理库存水平', '2024-01-18 08:45:00', '2024-01-26 09:15:00', NULL, 1);
INSERT INTO example VALUES (5, '人力资源平台', '员工信息管理和招聘流程优化', '2024-01-19 11:30:00', '2024-01-27 13:20:00', NULL, 1);
INSERT INTO example VALUES (6, '在线学习系统', '提供课程管理和在线学习功能', '2024-01-20 15:10:00', '2024-01-28 17:05:00', NULL, 1);
INSERT INTO example VALUES (7, '营销自动化', '自动化营销活动和客户跟进', '2024-01-21 09:00:00', '2024-01-29 10:45:00', NULL, 1);
INSERT INTO example VALUES (8, '数据可视化', '将数据转化为直观的图表和报告', '2024-01-22 13:25:00', '2024-01-30 15:30:00', NULL, 1);
INSERT INTO example VALUES (9, '移动应用开发', '跨平台移动应用开发框架', '2024-01-23 16:40:00', '2024-01-31 18:20:00', NULL, 1);
INSERT INTO example VALUES (10, '云存储服务', '安全可靠的云端文件存储解决方案', '2024-01-24 10:50:00', '2024-02-01 12:35:00', NULL, 1);
INSERT INTO example VALUES (11, '智能客服系统', '基于AI的智能客户服务助手', '2024-01-25 14:15:00', '2024-02-02 16:10:00', NULL, 1);
INSERT INTO example VALUES (12, '供应链管理', '优化供应链流程和物流管理', '2024-01-26 08:30:00', '2024-02-03 10:25:00', NULL, 1);
INSERT INTO example VALUES (13, '质量控制系统', '产品质量检测和流程监控', '2024-01-27 11:45:00', '2024-02-04 13:40:00', NULL, 1);
INSERT INTO example VALUES (14, '企业门户网站', '企业信息发布和员工协作平台', '2024-01-28 15:20:00', '2024-02-05 17:15:00', NULL, 1);
INSERT INTO example VALUES (15, '数据分析平台', '大数据处理和分析工具集', '2024-01-29 09:35:00', '2024-02-06 11:30:00', NULL, 1);
-- Table structure for sys_affix
DROP TABLE IF EXISTS sys_affix;
CREATE TABLE sys_affix (
    id SERIAL,
    name VARCHAR(255),
    path VARCHAR(255),
    url VARCHAR(255),
    file_md5 VARCHAR(32) DEFAULT '',
    size INTEGER,
    ftype VARCHAR(100),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    suffix VARCHAR(100),
    thumbnail_path VARCHAR(255),
    thumbnail_name VARCHAR(255),
    thumbnail_url VARCHAR(255),
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_affix.name IS '文件名';
COMMENT ON COLUMN sys_affix.path IS '路径';
COMMENT ON COLUMN sys_affix.url IS '文件url';
COMMENT ON COLUMN sys_affix.ftype IS '文件类型';
COMMENT ON COLUMN sys_affix.suffix IS '文件后缀';
COMMENT ON COLUMN sys_affix.thumbnail_url IS '缩略图URL';
COMMENT ON COLUMN sys_affix.file_md5 IS '文件MD5(秒传检测)';
COMMENT ON COLUMN sys_affix.size IS '文件大小';
COMMENT ON COLUMN sys_affix.thumbnail_path IS '缩略图路径';
COMMENT ON COLUMN sys_affix.thumbnail_name IS '缩略图名称';
COMMENT ON COLUMN sys_affix.id IS 'ID';

-- Records of sys_affix
-- Table structure for sys_affix_chunk
DROP TABLE IF EXISTS sys_affix_chunk;
CREATE TABLE sys_affix_chunk (
    id SERIAL,
    upload_id VARCHAR(64) NOT NULL,
    file_md5 VARCHAR(32) NOT NULL,
    file_name VARCHAR(255),
    file_size BIGINT,
    chunk_size INTEGER,
    total_chunks INTEGER,
    chunk_index INTEGER NOT NULL,
    chunk_path VARCHAR(255),
    status SMALLINT DEFAULT 0,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_affix_chunk.file_size IS '文件总大小';
COMMENT ON COLUMN sys_affix_chunk.chunk_index IS '当前分片序号';
COMMENT ON COLUMN sys_affix_chunk.chunk_path IS '分片文件路径';
COMMENT ON COLUMN sys_affix_chunk.status IS '0-上传中 1-已合并 2-已取消';
COMMENT ON COLUMN sys_affix_chunk.file_name IS '原始文件名';
COMMENT ON COLUMN sys_affix_chunk.chunk_size IS '分片大小';
COMMENT ON COLUMN sys_affix_chunk.total_chunks IS '总分片数';
COMMENT ON COLUMN sys_affix_chunk.created_by IS '创建者ID';
COMMENT ON COLUMN sys_affix_chunk.id IS 'ID';
COMMENT ON COLUMN sys_affix_chunk.upload_id IS '上传会话ID';
COMMENT ON COLUMN sys_affix_chunk.file_md5 IS '文件MD5';

-- Records of sys_affix_chunk
-- Table structure for sys_api
DROP TABLE IF EXISTS sys_api;
CREATE TABLE sys_api (
    id SERIAL,
    title VARCHAR(255),
    path VARCHAR(255),
    method VARCHAR(32),
    api_group VARCHAR(255),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_api.title IS '权限名称';
COMMENT ON COLUMN sys_api.path IS '权限路径';
COMMENT ON COLUMN sys_api.method IS '请求方法';
COMMENT ON COLUMN sys_api.api_group IS '分组';

-- Records of sys_api
INSERT INTO sys_api VALUES (1, '用户登录', '/api/login', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (2, '刷新Token', '/api/refreshToken', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (3, '生成验证码ID', '/api/captcha/id', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (4, '获取验证码图片', '/api/captcha/image', 'GET', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (5, '用户登出', '/api/users/logout', 'POST', '认证管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (6, '获取当前用户信息', '/api/users/profile', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (7, '根据ID获取用户信息', '/api/users/:id', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (8, '用户列表', '/api/users/list', 'GET', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (9, '新增用户', '/api/users/add', 'POST', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (10, '更新用户信息', '/api/users/edit', 'PUT', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (11, '删除用户', '/api/users/delete', 'DELETE', '用户管理', '2025-09-03 11:13:09', '2025-09-03 11:13:09', NULL, 1);
INSERT INTO sys_api VALUES (12, '获取用户权限菜单', '/api/sysMenu/getRouters', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (13, '获取完整菜单列表', '/api/sysMenu/getMenuList', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (14, '根据ID获取菜单信息', '/api/sysMenu/:id', 'GET', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (15, '新增菜单', '/api/sysMenu/add', 'POST', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (16, '更新菜单', '/api/sysMenu/edit', 'PUT', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (17, '删除菜单', '/api/sysMenu/delete', 'DELETE', '菜单管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (18, '获取部门列表', '/api/sysDepartment/getDivision', 'GET', '部门管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (19, '获取所有角色数据', '/api/sysRole/getRoles', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (20, '根据角色ID获取角色菜单权限', '/api/sysRole/getUserPermission/:roleId', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (21, '添加角色的菜单权限', '/api/sysRole/addRoleMenu', 'POST', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (22, '角色分页列表', '/api/sysRole/list', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (23, '根据ID获取角色信息', '/api/sysRole/:id', 'GET', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (24, '新增角色', '/api/sysRole/add', 'POST', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (25, '更新角色', '/api/sysRole/edit', 'PUT', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (26, '删除角色', '/api/sysRole/delete', 'DELETE', '角色管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (27, '获取所有字典数据', '/api/sysDict/getAllDicts', 'GET', '字典管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (28, '根据字典编码获取字典', '/api/sysDict/getByCode/:code', 'GET', '字典管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (29, 'API列表', '/api/sysApi/list', 'GET', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (30, '根据ID获取API信息', '/api/sysApi/:id', 'GET', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (31, '新增API', '/api/sysApi/add', 'POST', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (32, '更新API', '/api/sysApi/edit', 'PUT', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (33, '删除API', '/api/sysApi/delete', 'DELETE', 'API管理', '2025-09-03 11:13:10', '2025-09-03 11:13:10', NULL, 1);
INSERT INTO sys_api VALUES (35, '根据菜单ID获取API的ID集合', '/api/sysMenu/apis/:id', 'GET', '菜单管理', '2025-09-04 17:25:14', '2025-09-04 17:25:14', NULL, 1);
INSERT INTO sys_api VALUES (36, '设置菜单API权限', '/api/sysMenu/setApis', 'POST', '菜单管理', '2025-09-04 17:26:04', '2025-09-04 17:26:04', NULL, 1);
INSERT INTO sys_api VALUES (37, '根据ID获取部门信息', '/api/sysDepartment/:id', 'GET', '部门管理', '2025-09-12 14:46:42', '2025-09-12 14:46:42', NULL, 1);
INSERT INTO sys_api VALUES (38, '新增部门', '/api/sysDepartment/add', 'POST', '部门管理', '2025-09-12 14:47:27', '2025-09-12 14:47:27', NULL, 1);
INSERT INTO sys_api VALUES (39, '更新部门', '/api/sysDepartment/edit', 'PUT', '部门管理', '2025-09-12 14:48:15', '2025-09-12 14:48:27', NULL, 1);
INSERT INTO sys_api VALUES (40, '删除部门', '/api/sysDepartment/delete', 'DELETE', '部门管理', '2025-09-12 14:49:15', '2025-09-12 14:49:15', NULL, 1);
INSERT INTO sys_api VALUES (41, '字典分页列表', '/api/sysDict/list', 'GET', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (42, '根据ID获取字典信息', '/api/sysDict/:id', 'GET', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (43, '新增字典', '/api/sysDict/add', 'POST', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (44, '更新字典', '/api/sysDict/edit', 'PUT', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (45, '删除字典', '/api/sysDict/delete', 'DELETE', '字典管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (46, '字典项列表', '/api/sysDictItem/list', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (47, '根据ID获取字典项信息', '/api/sysDictItem/:id', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (48, '根据字典ID获取字典项列表', '/api/sysDictItem/getByDictId/:dictId', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (49, '根据字典编码获取字典项列表', '/api/sysDictItem/getByDictCode/:dictCode', 'GET', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (50, '新增字典项', '/api/sysDictItem/add', 'POST', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (51, '更新字典项', '/api/sysDictItem/edit', 'PUT', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (52, '删除字典项', '/api/sysDictItem/delete', 'DELETE', '字典项管理', '2025-09-16 16:31:15', '2025-09-16 16:31:15', NULL, 1);
INSERT INTO sys_api VALUES (53, '修改用户密码、手机号及邮箱', '/api/users/updateAccount', 'PUT', '用户管理', '2025-09-18 18:11:01', '2025-09-18 18:11:01', NULL, 1);
INSERT INTO sys_api VALUES (54, '头像上传', '/api/users/uploadAvatar', 'POST', '用户管理', '2025-09-24 17:01:05', '2025-09-24 17:01:05', NULL, 1);
INSERT INTO sys_api VALUES (55, '上传文件', '/api/sysAffix/upload', 'POST', '文件管理', '2025-09-25 15:51:04', '2025-09-25 15:51:04', NULL, 1);
INSERT INTO sys_api VALUES (56, '删除文件', '/api/sysAffix/delete', 'DELETE', '文件管理', '2025-09-25 15:51:38', '2025-09-25 15:51:38', NULL, 1);
INSERT INTO sys_api VALUES (57, '修改文件名', '/api/sysAffix/updateName', 'PUT', '文件管理', '2025-09-25 15:52:31', '2025-09-25 15:52:31', NULL, 1);
INSERT INTO sys_api VALUES (58, '文件列表', '/api/sysAffix/list', 'GET', '文件管理', '2025-09-25 15:54:03', '2025-09-25 15:54:03', NULL, 1);
INSERT INTO sys_api VALUES (59, '获取文件详情', '/api/sysAffix/:id', 'GET', '文件管理', '2025-09-25 15:54:55', '2025-09-25 15:54:55', NULL, 1);
INSERT INTO sys_api VALUES (60, '下载文件', '/api/sysAffix/download/:id', 'GET', '文件管理', '2025-09-25 15:56:15', '2025-09-25 15:58:06', NULL, 1);
INSERT INTO sys_api VALUES (61, '设置数据权限', '/api/sysRole/dataScope', 'PUT', '角色管理', '2025-09-26 17:04:15', '2025-09-26 17:04:15', NULL, 1);
INSERT INTO sys_api VALUES (62, '读取系统配置', '/api/config/get', 'GET', '系统配置', '2025-10-09 16:21:29', '2025-10-09 16:21:29', NULL, 1);
INSERT INTO sys_api VALUES (63, '修改系统配置', '/api/config/update', 'PUT', '系统配置', '2025-10-09 16:21:59', '2025-10-09 16:22:09', NULL, 1);
INSERT INTO sys_api VALUES (64, '查看内存缓存', '/api/config/viewCache', 'GET', '系统配置', '2025-10-10 17:41:33', '2025-10-10 17:41:33', NULL, 1);
INSERT INTO sys_api VALUES (65, '列表查询', '/api/plugins/example/list', 'GET', '插件示例', '2025-10-14 10:54:47', '2025-10-14 10:54:47', NULL, 1);
INSERT INTO sys_api VALUES (66, '新增', '/api/plugins/example/add', 'POST', '插件示例', '2025-10-14 10:56:43', '2025-10-14 10:56:43', NULL, 1);
INSERT INTO sys_api VALUES (67, '修改', '/api/plugins/example/edit', 'PUT', '插件示例', '2025-10-14 10:57:10', '2025-10-14 10:57:17', NULL, 1);
INSERT INTO sys_api VALUES (68, '删除', '/api/plugins/example/delete', 'DELETE', '插件示例', '2025-10-14 10:58:03', '2025-10-14 10:58:03', NULL, 1);
INSERT INTO sys_api VALUES (69, '查询单条数据', '/api/plugins/example/:id', 'GET', '插件示例', '2025-10-14 10:59:33', '2025-10-14 10:59:33', NULL, 1);
INSERT INTO sys_api VALUES (70, '日志列表', '/api/sysOperationLog/list', 'GET', '日志管理', '2025-10-20 10:10:58', '2025-10-20 10:10:58', NULL, 1);
INSERT INTO sys_api VALUES (72, '日志删除', '/api/sysOperationLog/delete', 'DELETE', '日志管理', '2025-10-20 10:13:19', '2025-10-20 10:13:19', NULL, 1);
INSERT INTO sys_api VALUES (73, '日志导出', '/api/sysOperationLog/export', 'GET', '日志管理', '2025-10-20 10:14:11', '2025-10-20 10:14:11', NULL, 1);
INSERT INTO sys_api VALUES (74, '导出菜单', '/api/sysMenu/export', 'GET', '菜单管理', '2025-10-20 17:17:07', '2025-10-20 17:17:07', NULL, 1);
INSERT INTO sys_api VALUES (75, '导入菜单', '/api/sysMenu/import', 'POST', '菜单管理', '2025-10-21 11:30:34', '2025-10-24 08:59:44', NULL, 1);
INSERT INTO sys_api VALUES (89, '修改用户基本信息', '/api/users/updateBasicInfo', 'PUT', '用户管理', '2025-10-31 09:05:00', '2025-10-31 09:05:00', NULL, 1);
INSERT INTO sys_api VALUES (105, '生成代码文件', '/api/codegen/generate', 'POST', '代码生成', '2025-11-07 15:32:53', '2025-11-07 15:32:53', NULL, 1);
INSERT INTO sys_api VALUES (106, '获取表的字段信息', '/api/codegen/columns', 'GET', '代码生成', '2025-11-07 15:33:52', '2025-11-07 15:33:52', NULL, 1);
INSERT INTO sys_api VALUES (187, '获取数据库列表', '/api/codegen/databases', 'GET', '代码生成', '2025-11-17 15:12:26', '2025-11-17 15:12:26', NULL, 1);
INSERT INTO sys_api VALUES (188, '获取指定数据库中的表集合', '/api/codegen/tables', 'GET', '代码生成', '2025-11-17 15:13:38', '2025-11-17 15:13:38', NULL, 1);
INSERT INTO sys_api VALUES (189, '代码预览', '/api/codegen/preview', 'GET', '代码生成', '2025-11-17 15:14:25', '2025-11-17 15:14:25', NULL, 1);
INSERT INTO sys_api VALUES (190, '代码生成配置列表', '/api/sysGen/list', 'GET', '代码生成', '2025-11-17 15:15:20', '2025-11-17 15:15:20', NULL, 1);
INSERT INTO sys_api VALUES (191, ' 批量创建代码生成配置', '/api/sysGen/batchInsert', 'POST', '代码生成', '2025-11-17 15:22:46', '2025-11-17 15:22:46', NULL, 1);
INSERT INTO sys_api VALUES (192, '获取代码生成配置详情', '/api/sysGen/:id', 'GET', '代码生成', '2025-11-17 15:23:29', '2025-11-17 15:23:29', NULL, 1);
INSERT INTO sys_api VALUES (193, '更新代码生成配置和字段信息', '/api/sysGen/update', 'PUT', '代码生成', '2025-11-17 15:24:41', '2025-11-17 15:24:41', NULL, 1);
INSERT INTO sys_api VALUES (194, '删除代码生成配置和字段信息', '/api/sysGen/:id', 'DELETE', '代码生成', '2025-11-17 15:26:44', '2025-11-17 15:26:44', NULL, 1);
INSERT INTO sys_api VALUES (195, '刷新代码生成配置的字段信息', '/api/sysGen/refreshFields', 'PUT', '代码生成', '2025-11-17 15:27:33', '2025-11-17 15:27:33', NULL, 1);
INSERT INTO sys_api VALUES (196, '生成菜单', '/api/codegen/insertmenuandapi', 'POST', '代码生成', '2025-11-26 15:12:56', '2025-11-26 15:12:56', NULL, 1);
INSERT INTO sys_api VALUES (197, '批量删除', '/api/sysMenu/batchDelete', 'DELETE', '菜单管理', '2025-12-05 17:48:52', '2025-12-05 17:48:52', NULL, 1);
INSERT INTO sys_api VALUES (198, '获取插件列表', '/api/pluginsmanager/exports', 'GET', '插件管理', '2025-12-08 16:38:26', '2025-12-08 16:38:26', NULL, 1);
INSERT INTO sys_api VALUES (199, '导出插件', '/api/pluginsmanager/export', 'POST', '插件管理', '2025-12-08 16:39:19', '2025-12-08 16:44:36', NULL, 1);
INSERT INTO sys_api VALUES (200, '导入插件', '/api/pluginsmanager/import', 'POST', '插件管理', '2025-12-08 16:47:11', '2025-12-08 16:47:11', NULL, 1);
INSERT INTO sys_api VALUES (201, '卸载插件', '/api/pluginsmanager/uninstall', 'DELETE', '插件管理', '2025-12-08 16:48:07', '2025-12-08 16:48:07', NULL, 1);
INSERT INTO sys_api VALUES (203, '定时任务列表', '/api/sysJobs/list', 'GET', '任务调度', '2026-02-11 11:56:54', '2026-02-11 11:56:54', NULL, 1);
INSERT INTO sys_api VALUES (204, '定时任务获取所有执行器列表', '/api/sysJobs/executors', 'GET', '任务调度', '2026-02-12 17:57:47', '2026-02-12 17:57:47', NULL, 1);
INSERT INTO sys_api VALUES (205, '定时任务新增', '/api/sysJobs/add', 'POST', '任务调度', '2026-02-11 11:57:33', '2026-02-11 11:57:33', NULL, 1);
INSERT INTO sys_api VALUES (206, '定时任务编辑', '/api/sysJobs/edit', 'PUT', '任务调度', '2026-02-11 11:57:58', '2026-02-11 11:57:58', NULL, 1);
INSERT INTO sys_api VALUES (207, '定时任务获取数据', '/api/sysJobs/:id', 'GET', '任务调度', '2026-02-11 11:59:54', '2026-02-11 11:59:54', NULL, 1);
INSERT INTO sys_api VALUES (208, '定时任务设置任务状态', '/api/sysJobs/setStatus', 'PUT', '任务调度', '2026-02-12 17:56:33', '2026-02-12 17:56:33', NULL, 1);
INSERT INTO sys_api VALUES (209, '定时任务删除', '/api/sysJobs/delete', 'DELETE', '任务调度', '2026-02-11 11:59:05', '2026-02-11 11:59:05', NULL, 1);
INSERT INTO sys_api VALUES (210, '定时任务立即执行任务', '/api/sysJobs/executeNow', 'POST', '任务调度', '2026-02-12 17:57:07', '2026-02-12 17:57:07', NULL, 1);
INSERT INTO sys_api VALUES (211, '定时任务日志', '/api/sysJobResults/list', 'GET', '任务调度', '2026-02-11 12:00:54', '2026-02-11 12:00:54', NULL, 1);
INSERT INTO sys_api VALUES (212, '定时任务日志删除', '/api/sysJobResults/delete', 'DELETE', '任务调度', '2026-02-11 12:01:22', '2026-02-11 12:01:22', NULL, 1);
INSERT INTO sys_api VALUES (213, '分片上传初始化', '/api/sysAffix/chunk/init', 'POST', '文件管理', '2026-04-09 15:12:48', '2026-04-09 15:12:48', NULL, 1);
INSERT INTO sys_api VALUES (214, '分片上传上传分片', '/api/sysAffix/chunk/upload', 'POST', '文件管理', '2026-04-09 15:37:44', '2026-04-09 15:37:44', NULL, 1);
INSERT INTO sys_api VALUES (215, '分片上传合并分片', '/api/sysAffix/chunk/merge', 'POST', '文件管理', '2026-04-09 15:39:26', '2026-04-09 15:39:26', NULL, 1);
INSERT INTO sys_api VALUES (216, '分片上传取消上传', '/api/sysAffix/chunk/cancel', 'DELETE', '文件管理', '2026-04-09 15:43:38', '2026-04-09 15:43:38', NULL, 1);
-- Table structure for sys_casbin_rule
DROP TABLE IF EXISTS sys_casbin_rule;
CREATE TABLE sys_casbin_rule (
    id SERIAL,
    ptype VARCHAR(100),
    v0 VARCHAR(100),
    v1 VARCHAR(100),
    v2 VARCHAR(100),
    v3 VARCHAR(100),
    v4 VARCHAR(100),
    v5 VARCHAR(100),
    PRIMARY KEY (id)
);


-- Records of sys_casbin_rule
INSERT INTO sys_casbin_rule VALUES (6266, 'g', 'user_1', 'role_1', '*', '', '', '');
INSERT INTO sys_casbin_rule VALUES (4189, 'g', 'user_4', 'role_2', '*', '', '', '');
INSERT INTO sys_casbin_rule VALUES (7386, 'p', 'role_1', '/api/codegen/generate', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7425, 'p', 'role_1', '/api/codegen/insertmenuandapi', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7443, 'p', 'role_1', '/api/codegen/preview', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7412, 'p', 'role_1', '/api/codegen/tables', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7384, 'p', 'role_1', '/api/config/get', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7452, 'p', 'role_1', '/api/config/update', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7442, 'p', 'role_1', '/api/config/viewCache', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7469, 'p', 'role_1', '/api/plugins/example/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7463, 'p', 'role_1', '/api/plugins/example/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7464, 'p', 'role_1', '/api/plugins/example/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7403, 'p', 'role_1', '/api/plugins/example/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7421, 'p', 'role_1', '/api/plugins/example/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7431, 'p', 'role_1', '/api/pluginsmanager/export', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7426, 'p', 'role_1', '/api/pluginsmanager/exports', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7459, 'p', 'role_1', '/api/pluginsmanager/import', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7454, 'p', 'role_1', '/api/pluginsmanager/uninstall', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7420, 'p', 'role_1', '/api/sysAffix/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7410, 'p', 'role_1', '/api/sysAffix/download/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7419, 'p', 'role_1', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7402, 'p', 'role_1', '/api/sysAffix/updateName', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7462, 'p', 'role_1', '/api/sysAffix/upload', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7423, 'p', 'role_1', '/api/sysApi/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7461, 'p', 'role_1', '/api/sysApi/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7456, 'p', 'role_1', '/api/sysApi/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7392, 'p', 'role_1', '/api/sysApi/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7467, 'p', 'role_1', '/api/sysApi/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7432, 'p', 'role_1', '/api/sysDepartment/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7417, 'p', 'role_1', '/api/sysDepartment/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7446, 'p', 'role_1', '/api/sysDepartment/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7393, 'p', 'role_1', '/api/sysDepartment/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7429, 'p', 'role_1', '/api/sysDepartment/getDivision', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7447, 'p', 'role_1', '/api/sysDict/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7468, 'p', 'role_1', '/api/sysDict/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7418, 'p', 'role_1', '/api/sysDict/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7455, 'p', 'role_1', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7449, 'p', 'role_1', '/api/sysDict/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7457, 'p', 'role_1', '/api/sysDictItem/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7409, 'p', 'role_1', '/api/sysDictItem/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7401, 'p', 'role_1', '/api/sysDictItem/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7433, 'p', 'role_1', '/api/sysDictItem/getByDictId/:dictId', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7414, 'p', 'role_1', '/api/sysGen/:id', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7413, 'p', 'role_1', '/api/sysGen/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7453, 'p', 'role_1', '/api/sysGen/batchInsert', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7411, 'p', 'role_1', '/api/sysGen/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7424, 'p', 'role_1', '/api/sysGen/refreshFields', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7382, 'p', 'role_1', '/api/sysGen/update', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7441, 'p', 'role_1', '/api/sysMenu/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7391, 'p', 'role_1', '/api/sysMenu/apis/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7390, 'p', 'role_1', '/api/sysMenu/batchDelete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7451, 'p', 'role_1', '/api/sysMenu/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7395, 'p', 'role_1', '/api/sysMenu/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7435, 'p', 'role_1', '/api/sysMenu/export', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7445, 'p', 'role_1', '/api/sysMenu/getMenuList', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7399, 'p', 'role_1', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7458, 'p', 'role_1', '/api/sysMenu/import', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7396, 'p', 'role_1', '/api/sysMenu/setApis', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7428, 'p', 'role_1', '/api/sysOperationLog/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7397, 'p', 'role_1', '/api/sysOperationLog/export', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7383, 'p', 'role_1', '/api/sysOperationLog/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7450, 'p', 'role_1', '/api/sysRole/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7408, 'p', 'role_1', '/api/sysRole/addRoleMenu', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7434, 'p', 'role_1', '/api/sysRole/dataScope', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7389, 'p', 'role_1', '/api/sysRole/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7400, 'p', 'role_1', '/api/sysRole/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7415, 'p', 'role_1', '/api/sysRole/getRoles', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7416, 'p', 'role_1', '/api/sysRole/getUserPermission/:roleId', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7444, 'p', 'role_1', '/api/users/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7387, 'p', 'role_1', '/api/users/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7388, 'p', 'role_1', '/api/users/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7460, 'p', 'role_1', '/api/users/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7466, 'p', 'role_1', '/api/users/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7440, 'p', 'role_1', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7427, 'p', 'role_1', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7394, 'p', 'role_1', '/api/users/updateAccount', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7465, 'p', 'role_1', '/api/users/updateBasicInfo', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7448, 'p', 'role_1', '/api/users/uploadAvatar', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4148, 'p', 'role_10', '/api/config/get', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4155, 'p', 'role_10', '/api/config/update', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4162, 'p', 'role_10', '/api/config/viewCache', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4168, 'p', 'role_10', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4125, 'p', 'role_10', '/api/sysApi/*', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4175, 'p', 'role_10', '/api/sysApi/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4179, 'p', 'role_10', '/api/sysApi/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4153, 'p', 'role_10', '/api/sysApi/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4158, 'p', 'role_10', '/api/sysApi/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4176, 'p', 'role_10', '/api/sysDepartment/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4133, 'p', 'role_10', '/api/sysDepartment/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4167, 'p', 'role_10', '/api/sysDepartment/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4122, 'p', 'role_10', '/api/sysDepartment/getDivision', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4136, 'p', 'role_10', '/api/sysDict/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4144, 'p', 'role_10', '/api/sysDict/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4137, 'p', 'role_10', '/api/sysDict/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4184, 'p', 'role_10', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4139, 'p', 'role_10', '/api/sysDict/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4146, 'p', 'role_10', '/api/sysDictItem/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4154, 'p', 'role_10', '/api/sysDictItem/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4147, 'p', 'role_10', '/api/sysDictItem/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4141, 'p', 'role_10', '/api/sysDictItem/getByDictId/*', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4174, 'p', 'role_10', '/api/sysMenu/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4145, 'p', 'role_10', '/api/sysMenu/apis/*', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4124, 'p', 'role_10', '/api/sysMenu/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4123, 'p', 'role_10', '/api/sysMenu/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4172, 'p', 'role_10', '/api/sysMenu/export', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4177, 'p', 'role_10', '/api/sysMenu/getMenuList', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4150, 'p', 'role_10', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4126, 'p', 'role_10', '/api/sysMenu/import', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4161, 'p', 'role_10', '/api/sysMenu/setApis', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4134, 'p', 'role_10', '/api/sysOperationLog/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4156, 'p', 'role_10', '/api/sysOperationLog/export', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4131, 'p', 'role_10', '/api/sysOperationLog/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4128, 'p', 'role_10', '/api/sysRole/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4135, 'p', 'role_10', '/api/sysRole/addRoleMenu', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4151, 'p', 'role_10', '/api/sysRole/dataScope', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4140, 'p', 'role_10', '/api/sysRole/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4166, 'p', 'role_10', '/api/sysRole/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4130, 'p', 'role_10', '/api/sysRole/getRoles', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4132, 'p', 'role_10', '/api/sysRole/getUserPermission/*', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4127, 'p', 'role_10', '/api/users/*', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4159, 'p', 'role_10', '/api/users/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4160, 'p', 'role_10', '/api/users/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4178, 'p', 'role_10', '/api/users/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4143, 'p', 'role_10', '/api/users/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4170, 'p', 'role_10', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4183, 'p', 'role_10', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4138, 'p', 'role_10', '/api/users/updateAccount', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (4171, 'p', 'role_10', '/api/users/uploadAvatar', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7481, 'p', 'role_2', '/api/codegen/generate', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7540, 'p', 'role_2', '/api/codegen/insertmenuandapi', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7539, 'p', 'role_2', '/api/codegen/preview', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7497, 'p', 'role_2', '/api/codegen/tables', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7550, 'p', 'role_2', '/api/config/get', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7553, 'p', 'role_2', '/api/config/update', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7472, 'p', 'role_2', '/api/config/viewCache', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7532, 'p', 'role_2', '/api/plugins/example/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7473, 'p', 'role_2', '/api/plugins/example/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7512, 'p', 'role_2', '/api/plugins/example/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7549, 'p', 'role_2', '/api/plugins/example/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7541, 'p', 'role_2', '/api/plugins/example/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7524, 'p', 'role_2', '/api/pluginsmanager/export', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7523, 'p', 'role_2', '/api/pluginsmanager/exports', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7486, 'p', 'role_2', '/api/pluginsmanager/import', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7525, 'p', 'role_2', '/api/pluginsmanager/uninstall', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7480, 'p', 'role_2', '/api/sysAffix/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7506, 'p', 'role_2', '/api/sysAffix/download/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7552, 'p', 'role_2', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7521, 'p', 'role_2', '/api/sysAffix/updateName', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7556, 'p', 'role_2', '/api/sysAffix/upload', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7529, 'p', 'role_2', '/api/sysApi/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7518, 'p', 'role_2', '/api/sysApi/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7479, 'p', 'role_2', '/api/sysApi/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7484, 'p', 'role_2', '/api/sysApi/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7476, 'p', 'role_2', '/api/sysApi/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7500, 'p', 'role_2', '/api/sysDepartment/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7530, 'p', 'role_2', '/api/sysDepartment/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7519, 'p', 'role_2', '/api/sysDepartment/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7560, 'p', 'role_2', '/api/sysDepartment/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7510, 'p', 'role_2', '/api/sysDepartment/getDivision', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7547, 'p', 'role_2', '/api/sysDict/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7520, 'p', 'role_2', '/api/sysDict/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7531, 'p', 'role_2', '/api/sysDict/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7534, 'p', 'role_2', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7475, 'p', 'role_2', '/api/sysDict/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7502, 'p', 'role_2', '/api/sysDictItem/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7548, 'p', 'role_2', '/api/sysDictItem/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7495, 'p', 'role_2', '/api/sysDictItem/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7501, 'p', 'role_2', '/api/sysDictItem/getByDictId/:dictId', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7522, 'p', 'role_2', '/api/sysGen/:id', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7498, 'p', 'role_2', '/api/sysGen/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7538, 'p', 'role_2', '/api/sysGen/batchInsert', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7490, 'p', 'role_2', '/api/sysGen/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7517, 'p', 'role_2', '/api/sysGen/refreshFields', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7558, 'p', 'role_2', '/api/sysGen/update', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7505, 'p', 'role_2', '/api/sysMenu/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7491, 'p', 'role_2', '/api/sysMenu/apis/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7535, 'p', 'role_2', '/api/sysMenu/batchDelete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7478, 'p', 'role_2', '/api/sysMenu/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7483, 'p', 'role_2', '/api/sysMenu/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7485, 'p', 'role_2', '/api/sysMenu/export', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7559, 'p', 'role_2', '/api/sysMenu/getMenuList', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7527, 'p', 'role_2', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7507, 'p', 'role_2', '/api/sysMenu/import', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7546, 'p', 'role_2', '/api/sysMenu/setApis', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7533, 'p', 'role_2', '/api/sysOperationLog/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7513, 'p', 'role_2', '/api/sysOperationLog/export', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7544, 'p', 'role_2', '/api/sysOperationLog/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7511, 'p', 'role_2', '/api/sysRole/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7487, 'p', 'role_2', '/api/sysRole/addRoleMenu', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7471, 'p', 'role_2', '/api/sysRole/dataScope', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7528, 'p', 'role_2', '/api/sysRole/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7482, 'p', 'role_2', '/api/sysRole/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7551, 'p', 'role_2', '/api/sysRole/getRoles', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7477, 'p', 'role_2', '/api/sysRole/getUserPermission/:roleId', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7488, 'p', 'role_2', '/api/users/:id', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7493, 'p', 'role_2', '/api/users/add', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7494, 'p', 'role_2', '/api/users/delete', 'DELETE', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7545, 'p', 'role_2', '/api/users/edit', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7543, 'p', 'role_2', '/api/users/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7526, 'p', 'role_2', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7499, 'p', 'role_2', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7489, 'p', 'role_2', '/api/users/updateAccount', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7509, 'p', 'role_2', '/api/users/updateBasicInfo', 'PUT', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (7542, 'p', 'role_2', '/api/users/uploadAvatar', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (2966, 'p', 'role_4', '/api/sysAffix/list', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (2971, 'p', 'role_4', '/api/sysDict/getAllDicts', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (2970, 'p', 'role_4', '/api/sysMenu/getRouters', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (2969, 'p', 'role_4', '/api/users/*', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (2967, 'p', 'role_4', '/api/users/logout', 'POST', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (2968, 'p', 'role_4', '/api/users/profile', 'GET', '*', '', '');
INSERT INTO sys_casbin_rule VALUES (2965, 'p', 'role_4', '/api/users/uploadAvatar', 'POST', '*', '', '');
-- Table structure for sys_department
DROP TABLE IF EXISTS sys_department;
CREATE TABLE sys_department (
    id SERIAL,
    parent_id INTEGER DEFAULT 0,
    name VARCHAR(255),
    status BOOLEAN,
    leader VARCHAR(255),
    phone VARCHAR(255),
    email VARCHAR(255),
    sort INTEGER DEFAULT 0,
    describe VARCHAR(255),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_department.status IS '状态： 0 停用 1 启用';
COMMENT ON COLUMN sys_department.email IS '邮箱';
COMMENT ON COLUMN sys_department.sort IS '排序';
COMMENT ON COLUMN sys_department.parent_id IS '父级';
COMMENT ON COLUMN sys_department.leader IS '负责人';
COMMENT ON COLUMN sys_department.phone IS '联系电话';
COMMENT ON COLUMN sys_department.describe IS '描述';
COMMENT ON COLUMN sys_department.name IS '部门名称';

-- Records of sys_department
INSERT INTO sys_department VALUES (1, 0, '总部', '1', '张明', '13800000001', 'headquarters@company.com', 1, '公司总部管理部门', '2023-01-15 09:00:00', '2025-10-31 17:05:24', NULL, 1);
-- Table structure for sys_dict
DROP TABLE IF EXISTS sys_dict;
CREATE TABLE sys_dict (
    id SERIAL,
    name VARCHAR(255),
    code VARCHAR(255),
    status BOOLEAN,
    description VARCHAR(500),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_dict.id IS 'ID';
COMMENT ON COLUMN sys_dict.name IS '字典名称';
COMMENT ON COLUMN sys_dict.code IS '字典编码';
COMMENT ON COLUMN sys_dict.status IS '状态';

-- Records of sys_dict
INSERT INTO sys_dict VALUES (1, '性别', 'gender', '1', '这是一个性别字典', '2024-07-01 10:00:00', NULL, NULL, 1);
INSERT INTO sys_dict VALUES (2, '状态', 'status', '1', '状态字段可以用这个', '2024-07-01 10:00:00', NULL, NULL, 1);
INSERT INTO sys_dict VALUES (3, '岗位', 'post', '1', '岗位字段', '2024-07-01 10:00:00', NULL, NULL, 1);
INSERT INTO sys_dict VALUES (4, '任务状态', 'taskStatus', '1', '任务状态字段可以用它', '2024-07-01 10:00:00', NULL, NULL, 1);
-- Table structure for sys_dict_item
DROP TABLE IF EXISTS sys_dict_item;
CREATE TABLE sys_dict_item (
    id SERIAL,
    name VARCHAR(255),
    value VARCHAR(255),
    status BOOLEAN,
    dict_id INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_dict_item.status IS '状态';

-- Records of sys_dict_item
INSERT INTO sys_dict_item VALUES (11, '男', '1', '1', 1);
INSERT INTO sys_dict_item VALUES (12, '女', '0', '1', 1);
INSERT INTO sys_dict_item VALUES (13, '其它', '2', '1', 1);
INSERT INTO sys_dict_item VALUES (21, '禁用', '0', '1', 2);
INSERT INTO sys_dict_item VALUES (22, '启用', '1', '1', 2);
INSERT INTO sys_dict_item VALUES (31, '总经理', '1', '1', 3);
INSERT INTO sys_dict_item VALUES (32, '总监', '2', '1', 3);
INSERT INTO sys_dict_item VALUES (33, '人事主管', '3', '1', 3);
INSERT INTO sys_dict_item VALUES (34, '开发部主管', '4', '1', 3);
INSERT INTO sys_dict_item VALUES (35, '普通职员', '5', '1', 3);
INSERT INTO sys_dict_item VALUES (36, '其它', '999', '1', 3);
INSERT INTO sys_dict_item VALUES (41, '失败', '0', '1', 4);
INSERT INTO sys_dict_item VALUES (42, '成功', '1', '1', 4);
-- Table structure for sys_gen
DROP TABLE IF EXISTS sys_gen;
CREATE TABLE sys_gen (
    id SERIAL,
    db_type VARCHAR(255),
    database VARCHAR(255),
    name VARCHAR(255),
    module_name VARCHAR(255),
    file_name VARCHAR(255),
    describe VARCHAR(1000),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    is_cover SMALLINT DEFAULT 0,
    is_menu SMALLINT DEFAULT 0,
    is_tree SMALLINT DEFAULT 0,
    is_relation_tree SMALLINT DEFAULT 0,
    relation_tree_table INTEGER DEFAULT 0,
    relation_field INTEGER DEFAULT 0,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_gen.created_at IS '创建时间';
COMMENT ON COLUMN sys_gen.is_cover IS '是否覆盖';
COMMENT ON COLUMN sys_gen.db_type IS '数据库类型';
COMMENT ON COLUMN sys_gen.module_name IS '模块名称';
COMMENT ON COLUMN sys_gen.file_name IS '文件名称';
COMMENT ON COLUMN sys_gen.is_relation_tree IS '是否关联树形分类';
COMMENT ON COLUMN sys_gen.relation_field IS '关联的字段ID';
COMMENT ON COLUMN sys_gen.name IS '数据库表名';
COMMENT ON COLUMN sys_gen.is_menu IS '是否生成菜单';
COMMENT ON COLUMN sys_gen.relation_tree_table IS '关联的树形表';
COMMENT ON COLUMN sys_gen.id IS 'ID';
COMMENT ON COLUMN sys_gen.database IS '数据库';
COMMENT ON COLUMN sys_gen.describe IS '描述';
COMMENT ON COLUMN sys_gen.updated_at IS '修改时间';
COMMENT ON COLUMN sys_gen.deleted_at IS '删除时间';
COMMENT ON COLUMN sys_gen.created_by IS '创建人';

-- Records of sys_gen
INSERT INTO sys_gen VALUES (23, 'mysql', 'uvp-gb28181', 'demo_students', 'test_school', 'demo_students', '学员管理', '2025-11-13 15:17:27', '2025-11-17 16:31:43', NULL, 1, 1, 1, NULL, 0, 0, 0);
INSERT INTO sys_gen VALUES (24, 'mysql', 'uvp-gb28181', 'demo_teacher', 'test_school', 'demo_teacher', '教师表', '2025-11-13 15:17:27', '2025-11-17 17:29:28', NULL, 1, 1, 1, NULL, 0, 0, 0);
-- Table structure for sys_gen_field
DROP TABLE IF EXISTS sys_gen_field;
CREATE TABLE sys_gen_field (
    id SERIAL,
    gen_id INTEGER,
    data_name VARCHAR(255),
    data_type VARCHAR(255),
    data_comment VARCHAR(255),
    data_extra VARCHAR(255),
    data_column_key VARCHAR(255),
    data_unsigned SMALLINT DEFAULT 0,
    is_primary SMALLINT DEFAULT 0,
    go_type VARCHAR(255),
    front_type VARCHAR(255),
    custom_name VARCHAR(255) DEFAULT '',
    require SMALLINT DEFAULT 0,
    list_show SMALLINT DEFAULT 0,
    form_show SMALLINT DEFAULT 0,
    query_show SMALLINT DEFAULT 0,
    query_type VARCHAR(255),
    form_type VARCHAR(255),
    dict_type VARCHAR(255),
    gorm_tag VARCHAR(255),
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_gen_field.dict_type IS '关联的字典';
COMMENT ON COLUMN sys_gen_field.gorm_tag IS 'gorm标签';
COMMENT ON COLUMN sys_gen_field.data_name IS '列名';
COMMENT ON COLUMN sys_gen_field.data_comment IS '列注释';
COMMENT ON COLUMN sys_gen_field.data_extra IS '额外信息';
COMMENT ON COLUMN sys_gen_field.data_unsigned IS '是否为无符号类型';
COMMENT ON COLUMN sys_gen_field.front_type IS '前端类型';
COMMENT ON COLUMN sys_gen_field.is_primary IS '是否主键';
COMMENT ON COLUMN sys_gen_field.require IS '是否必填';
COMMENT ON COLUMN sys_gen_field.query_show IS '查询显示';
COMMENT ON COLUMN sys_gen_field.custom_name IS '自定义字段名称';
COMMENT ON COLUMN sys_gen_field.list_show IS '列表显示';
COMMENT ON COLUMN sys_gen_field.data_type IS '数据类型';
COMMENT ON COLUMN sys_gen_field.data_column_key IS '列键信息';
COMMENT ON COLUMN sys_gen_field.go_type IS 'go类型';
COMMENT ON COLUMN sys_gen_field.form_show IS '表单显示';
COMMENT ON COLUMN sys_gen_field.query_type IS '查询方式\r\nEQ  等于\r\nNE 不等于\r\nGT 大于\r\nGTE 大于等于\r\nLT 小于\r\nLTE 小于等于\r\nLIKE 包含\r\nBETWEEN 范围';
COMMENT ON COLUMN sys_gen_field.form_type IS '表单类型\r\ninput 文本框\r\ntextarea 文本域\r\nnumber 数字输入框\r\nselect 下拉框\r\nradio 单选框\r\ncheckbox 复选框\r\ndatetime 日期时间';

-- Records of sys_gen_field
INSERT INTO sys_gen_field VALUES (185, 23, 'student_id', 'int', 'ID', 'auto_increment', 'PRI', 1, 1, 'uint', 'number', 'stu_id', 1, 0, 0, 1, 'EQ', '', '', 'column:student_id;primaryKey;not NULL;autoIncrement');
INSERT INTO sys_gen_field VALUES (186, 23, 'student_name', 'varchar', '姓名', '', '', 0, 0, 'string', 'string', 'stu_name', 1, 1, 1, 1, 'LIKE', 'textarea', '', 'column:student_name;not NULL');
INSERT INTO sys_gen_field VALUES (187, 23, 'age', 'int', '年龄', '', '', 0, 0, 'int', 'number', 'age', 1, 1, 1, 1, 'LIKE', '', '', 'column:age;not NULL;default:18');
INSERT INTO sys_gen_field VALUES (188, 23, 'gender', 'varchar', '性别', '', '', 0, 0, 'string', 'string', 'gender', 1, 1, 1, 1, 'BETWEEN', 'radio', 'gender', 'column:gender;not NULL;default:''''');
INSERT INTO sys_gen_field VALUES (189, 23, 'class_name', 'varchar', '班级名称', '', '', 0, 0, 'string', 'string', 'class_name', 0, 1, 1, 0, '', 'checkbox', 'class', 'column:class_name;not NULL');
INSERT INTO sys_gen_field VALUES (190, 23, 'admission_date', 'datetime', '入学日期', '', '', 0, 0, 'time.Time', 'string', 'admission_date', 0, 0, 1, 0, '', '', '', 'column:admission_date;not NULL');
INSERT INTO sys_gen_field VALUES (191, 23, 'email', 'varchar', ' 邮箱', '', 'UNI', 0, 0, 'string', 'string', 'email', 0, 0, 1, 1, '', 'checkbox', 'status', 'column:email;uniqueIndex');
INSERT INTO sys_gen_field VALUES (192, 23, 'phone', 'varchar', '电话号码', '', '', 0, 0, 'string', 'string', 'phone', 0, 0, 0, 0, '', '', '', 'column:phone');
INSERT INTO sys_gen_field VALUES (193, 23, 'address', 'text', '地址', '', '', 0, 0, 'string', 'string', 'address', 0, 0, 1, 1, '', 'select', 'status', 'column:address');
INSERT INTO sys_gen_field VALUES (194, 23, 'created_at', 'datetime', '创建时间', '', '', 0, 0, 'time.Time', 'string', 'created_at', NULL, NULL, 1, 1, 'BETWEEN', '', '', 'column:created_at');
INSERT INTO sys_gen_field VALUES (195, 23, 'updated_at', 'datetime', '更新时间', '', '', 0, 0, 'time.Time', 'string', 'updated_at', NULL, NULL, 1, NULL, '', '', '', 'column:updated_at');
INSERT INTO sys_gen_field VALUES (196, 23, 'deleted_at', 'datetime', '删除时间', '', '', 0, 0, 'time.Time', 'string', 'deleted_at', NULL, NULL, 1, NULL, '', '', '', 'column:deleted_at');
INSERT INTO sys_gen_field VALUES (197, 23, 'created_by', 'int', '创建人', '', '', 1, 0, 'uint', 'number', 'created_by', NULL, NULL, 1, NULL, '', '', '', 'column:created_by');
INSERT INTO sys_gen_field VALUES (199, 24, 'id', 'int', '主键ID', 'auto_increment', 'PRI', 1, 1, 'uint', 'number', 'tc_id', 1, 1, 1, 1, '', '', '', 'column:id;primaryKey;not NULL;autoIncrement');
INSERT INTO sys_gen_field VALUES (200, 24, 'name', 'varchar', '教师姓名', '', '', 0, 0, 'string', 'string', 'tc_name', 1, 1, 1, 1, 'LIKE', 'input', '', 'column:name;not NULL');
INSERT INTO sys_gen_field VALUES (201, 24, 'employee_id', 'varchar', '工号', '', '', 0, 0, 'string', 'string', 'employee_id', 1, 1, 1, 1, 'BETWEEN', '', '', 'column:employee_id');
INSERT INTO sys_gen_field VALUES (202, 24, 'gender', 'tinyint', '性别', '', '', 0, 0, 'int', 'number', 'gender', 1, 1, 1, 1, 'EQ', 'select', 'gender', 'column:gender;default:0');
INSERT INTO sys_gen_field VALUES (203, 24, 'phone', 'varchar', '手机号', '', '', 0, 0, 'string', 'string', 'phone', 1, 1, 1, 1, 'GT', '', '', 'column:phone');
INSERT INTO sys_gen_field VALUES (204, 24, 'email', 'varchar', '邮箱', '', '', 0, 0, 'string', 'string', 'email', 1, 1, 1, 1, 'NE', '', '', 'column:email');
INSERT INTO sys_gen_field VALUES (205, 24, 'subject', 'varchar', '所教学科', '', '', 0, 0, 'string', 'string', 'subject', 1, 1, 1, 1, '', '', '', 'column:subject');
INSERT INTO sys_gen_field VALUES (206, 24, 'title', 'varchar', '职称', '', '', 0, 0, 'string', 'string', 'title', 1, 1, 1, 1, '', '', '', 'column:title');
INSERT INTO sys_gen_field VALUES (207, 24, 'status', 'tinyint', '状态', '', '', 0, 0, 'int', 'number', 'status', 1, 1, 1, 1, '', 'select', 'status', 'column:status;default:1');
INSERT INTO sys_gen_field VALUES (208, 24, 'hire_date', 'date', '入职日期', '', '', 0, 0, 'time.Time', 'string', 'hire_date', 1, 1, 1, 1, 'BETWEEN', '', '', 'column:hire_date');
INSERT INTO sys_gen_field VALUES (209, 24, 'birth_date', 'date', '出生日期', '', '', 0, 0, 'time.Time', 'string', 'birth_date', 1, 1, 1, 1, '', 'select', 'test_date', 'column:birth_date');
INSERT INTO sys_gen_field VALUES (210, 24, 'created_at', 'datetime', '创建时间', '', '', 0, 0, 'time.Time', 'string', 'created_at', NULL, NULL, NULL, NULL, '', '', '', 'column:created_at');
INSERT INTO sys_gen_field VALUES (211, 24, 'updated_at', 'datetime', '更新时间', '', '', 0, 0, 'time.Time', 'string', 'updated_at', NULL, NULL, NULL, NULL, '', '', '', 'column:updated_at');
INSERT INTO sys_gen_field VALUES (212, 24, 'deleted_at', 'datetime', '删除时间', '', '', 0, 0, 'time.Time', 'string', 'deleted_at', NULL, NULL, NULL, NULL, '', '', '', 'column:deleted_at');
INSERT INTO sys_gen_field VALUES (213, 24, 'created_by', 'int', '创建人', '', '', 1, 0, 'uint', 'number', 'created_by', NULL, NULL, NULL, NULL, '', '', '', 'column:created_by');
-- Table structure for sys_jobs
DROP TABLE IF EXISTS sys_jobs;
CREATE TABLE sys_jobs (
    id VARCHAR(255) NOT NULL,
    group VARCHAR(100) NOT NULL,
    name VARCHAR(200) NOT NULL,
    description TEXT,
    executor_name VARCHAR(100) NOT NULL,
    execution_policy SMALLINT NOT NULL DEFAULT 1,
    status SMALLINT NOT NULL DEFAULT 1,
    cron_expression VARCHAR(100) NOT NULL,
    parameters JSON,
    blocking_policy SMALLINT NOT NULL DEFAULT 0,
    timeout BIGINT NOT NULL DEFAULT 30000000000,
    max_retry INTEGER NOT NULL DEFAULT 0,
    retry_interval BIGINT NOT NULL DEFAULT 10000000000,
    parallel_num INTEGER NOT NULL DEFAULT 1,
    running_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_jobs.description IS '任务描述';
COMMENT ON COLUMN sys_jobs.max_retry IS '最大重试次数';
COMMENT ON COLUMN sys_jobs.parallel_num IS '并行数';
COMMENT ON COLUMN sys_jobs.created_at IS '创建时间';
COMMENT ON COLUMN sys_jobs.id IS '任务ID';
COMMENT ON COLUMN sys_jobs.name IS '任务名称';
COMMENT ON COLUMN sys_jobs.updated_at IS '更新时间';
COMMENT ON COLUMN sys_jobs.executor_name IS '执行器名称';
COMMENT ON COLUMN sys_jobs.parameters IS '任务参数(JSON格式)';
COMMENT ON COLUMN sys_jobs.timeout IS '超时时间(纳秒)';
COMMENT ON COLUMN sys_jobs.running_count IS '当前运行中的任务数';
COMMENT ON COLUMN sys_jobs.group IS '任务分组名称';
COMMENT ON COLUMN sys_jobs.execution_policy IS '执行策略: 0=单次执行, 1=重复执行';
COMMENT ON COLUMN sys_jobs.status IS '任务状态: 0=禁用, 1=启用';
COMMENT ON COLUMN sys_jobs.cron_expression IS 'Cron表达式';
COMMENT ON COLUMN sys_jobs.blocking_policy IS '阻塞策略: 0=丢弃, 1=覆盖, 2=并行';
COMMENT ON COLUMN sys_jobs.retry_interval IS '重试间隔(纳秒)';

-- Records of sys_jobs
-- Table structure for sys_job_results
DROP TABLE IF EXISTS sys_job_results;
CREATE TABLE sys_job_results (
    id BIGSERIAL,
    job_id VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL,
    error TEXT,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP NOT NULL,
    duration BIGINT NOT NULL,
    retry_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    CONSTRAINT TEXT
);

COMMENT ON COLUMN sys_job_results.id IS '自增主键';
COMMENT ON COLUMN sys_job_results.error IS '错误信息';
COMMENT ON COLUMN sys_job_results.duration IS '执行时长(纳秒)';
COMMENT ON COLUMN sys_job_results.retry_count IS '重试次数';
COMMENT ON COLUMN sys_job_results.created_at IS '记录创建时间';
COMMENT ON COLUMN sys_job_results.job_id IS '任务ID';
COMMENT ON COLUMN sys_job_results.status IS '执行状态: SUCCESS, FAILED, PANIC';
COMMENT ON COLUMN sys_job_results.start_time IS '开始时间';
COMMENT ON COLUMN sys_job_results.end_time IS '结束时间';

-- Records of sys_job_results
-- Table structure for sys_menu
DROP TABLE IF EXISTS sys_menu;
CREATE TABLE sys_menu (
    id SERIAL,
    parent_id INTEGER NOT NULL DEFAULT 0,
    path VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    redirect VARCHAR(255),
    component VARCHAR(255),
    title VARCHAR(100),
    is_full BOOLEAN DEFAULT false,
    hide BOOLEAN DEFAULT false,
    disable BOOLEAN DEFAULT false,
    keep_alive BOOLEAN DEFAULT false,
    affix BOOLEAN DEFAULT false,
    link VARCHAR(500) DEFAULT '',
    iframe BOOLEAN DEFAULT false,
    svg_icon VARCHAR(100) DEFAULT '',
    icon VARCHAR(100) DEFAULT '',
    sort INTEGER DEFAULT 0,
    type SMALLINT DEFAULT 2,
    is_link BOOLEAN DEFAULT false,
    permission VARCHAR(255) DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_menu.id IS '路由ID';
COMMENT ON COLUMN sys_menu.path IS '路由路径';
COMMENT ON COLUMN sys_menu.redirect IS '重定向';
COMMENT ON COLUMN sys_menu.created_at IS '创建时间';
COMMENT ON COLUMN sys_menu.parent_id IS '父级路由ID，顶层为0';
COMMENT ON COLUMN sys_menu.iframe IS '是否内嵌：0-否，1-是';
COMMENT ON COLUMN sys_menu.sort IS '排序字段';
COMMENT ON COLUMN sys_menu.updated_at IS '更新时间';
COMMENT ON COLUMN sys_menu.disable IS '是否停用：0-否，1-是';
COMMENT ON COLUMN sys_menu.affix IS '是否固定：0-否，1-是';
COMMENT ON COLUMN sys_menu.name IS '路由名称';
COMMENT ON COLUMN sys_menu.component IS '组件文件路径';
COMMENT ON COLUMN sys_menu.title IS '菜单标题，国际化key';
COMMENT ON COLUMN sys_menu.is_full IS '是否全屏显示：0-否，1-是';
COMMENT ON COLUMN sys_menu.hide IS '是否隐藏：0-否，1-是';
COMMENT ON COLUMN sys_menu.svg_icon IS 'svg图标名称';
COMMENT ON COLUMN sys_menu.keep_alive IS '是否缓存：0-否，1-是';
COMMENT ON COLUMN sys_menu.link IS '外链地址';
COMMENT ON COLUMN sys_menu.icon IS '普通图标名称';
COMMENT ON COLUMN sys_menu.type IS '类型：1-目录，2-菜单，3-按钮';
COMMENT ON COLUMN sys_menu.is_link IS '是否外链';
COMMENT ON COLUMN sys_menu.permission IS '权限标识';

-- Records of sys_menu
INSERT INTO sys_menu VALUES (1, 0, '/home', 'home', NULL, 'home/home', 'home', '0', '0', '0', '0', '1', '', '0', 'home', '', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-08-27 09:09:44', NULL, 1);
INSERT INTO sys_menu VALUES (10, 0, '/system', 'system', NULL, NULL, 'system', '0', '0', '0', '1', '0', '', '0', 'set', '', 0, '1', '0', '', '2025-08-27 09:09:44', '2025-08-27 09:09:44', NULL, 1);
INSERT INTO sys_menu VALUES (1001, 10, '/system/account', 'account', '', 'system/account/account', 'account', '0', '0', '0', '1', '0', '', '0', '', 'IconUser', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-10-11 15:37:41', NULL, 1);
INSERT INTO sys_menu VALUES (1002, 10, '/system/role', 'role', '', 'system/role/role', 'role', '0', '0', '0', '1', '0', '', '0', '', 'IconUserGroup', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-10-11 16:16:08', NULL, 1);
INSERT INTO sys_menu VALUES (1003, 10, '/system/menu', 'menu', NULL, 'system/menu/menu', 'menu', '0', '0', '0', '1', '0', '', '0', '', 'icon-menu', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-08-27 09:09:44', NULL, 1);
INSERT INTO sys_menu VALUES (1004, 10, '/system/division', 'division', '', 'system/division/division', 'division', '0', '0', '0', '1', '0', '', '0', '', 'IconMindMapping', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-10-11 16:23:14', NULL, 1);
INSERT INTO sys_menu VALUES (1005, 10, '/system/dictionary', 'dictionary', '', 'system/dictionary/dictionary', 'dictionary', '0', '0', '0', '1', '0', '', '0', '', 'IconBook', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-10-11 16:23:47', NULL, 1);
INSERT INTO sys_menu VALUES (1006, 10, '/system/log', 'log', '', 'system/log/log', 'log', '0', '0', '0', '1', '0', '', '0', '', 'IconCommon', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-10-20 17:14:19', NULL, 1);
INSERT INTO sys_menu VALUES (1007, 10, '/system/userinfo', 'userinfo', '', 'system/userinfo/userinfo', 'userinfo', '0', '1', '0', '1', '0', '', '0', '', 'icon-menu', 0, '2', '0', '', '2025-08-27 09:09:44', '2025-09-17 11:19:11', NULL, 1);
INSERT INTO sys_menu VALUES (140213, 10, '/system/api', 'SystemApi', '', 'system/sysapi/sysapi', 'api-management', '0', '0', '0', '1', '0', '', '0', '', 'IconFile', 0, '2', '0', '', '2025-09-03 10:53:57', '2025-10-16 08:53:42', NULL, 1);
INSERT INTO sys_menu VALUES (140214, 1001, '', '', '', '', '新增', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:account:add', '2025-09-03 16:11:58', '2025-09-03 16:11:58', NULL, 1);
INSERT INTO sys_menu VALUES (140215, 1001, '', '', '', '', '编辑', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:account:edit', '2025-09-03 17:11:24', '2025-09-03 17:11:24', NULL, 1);
INSERT INTO sys_menu VALUES (140216, 1001, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:account:delete', '2025-09-03 17:12:22', '2025-09-03 17:12:22', NULL, 1);
INSERT INTO sys_menu VALUES (140218, 1002, '', '', '', '', '新增', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:role:add', '2025-09-04 16:43:54', '2025-09-04 16:43:54', NULL, 1);
INSERT INTO sys_menu VALUES (140219, 1002, '', '', '', '', '编辑', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:role:edit', '2025-09-04 16:47:15', '2025-09-04 16:47:15', NULL, 1);
INSERT INTO sys_menu VALUES (140220, 1002, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:role:delete', '2025-09-04 16:50:19', '2025-09-04 16:50:19', NULL, 1);
INSERT INTO sys_menu VALUES (140221, 1002, '', '', '', '', '分配权限', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:role:addRoleMenu', '2025-09-04 16:53:09', '2025-09-04 16:53:09', NULL, 1);
INSERT INTO sys_menu VALUES (140222, 1003, '', '', '', '', '新增', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:menu:add', '2025-09-04 17:07:16', '2025-09-04 17:07:16', NULL, 1);
INSERT INTO sys_menu VALUES (140223, 1003, '', '', '', '', '编辑', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:menu:edit', '2025-09-04 17:11:51', '2025-09-04 17:11:51', NULL, 1);
INSERT INTO sys_menu VALUES (140224, 1003, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:menu:delete', '2025-09-04 17:12:24', '2025-09-04 17:12:24', NULL, 1);
INSERT INTO sys_menu VALUES (140225, 1003, '', '', '', '', '分配权限', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:menu:setMenuApis', '2025-09-04 17:20:09', '2025-09-04 17:20:09', NULL, 1);
INSERT INTO sys_menu VALUES (140226, 140213, '', '', '', '', '新增', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:api:add', '2025-09-04 17:30:56', '2025-09-04 17:30:56', NULL, 1);
INSERT INTO sys_menu VALUES (140227, 140213, '', '', '', '', '编辑', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:api:edit', '2025-09-04 17:31:20', '2025-09-04 17:31:20', NULL, 1);
INSERT INTO sys_menu VALUES (140228, 140213, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:api:delete', '2025-09-04 17:31:38', '2025-09-04 17:31:38', NULL, 1);
INSERT INTO sys_menu VALUES (140229, 1004, '', '', '', '', '新增部门', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:division:add', '2025-09-12 14:50:55', '2025-09-12 14:50:55', NULL, 1);
INSERT INTO sys_menu VALUES (140230, 1004, '', '', '', '', '编辑部门', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:division:edit', '2025-09-12 14:51:17', '2025-09-12 14:51:17', NULL, 1);
INSERT INTO sys_menu VALUES (140231, 1004, '', '', '', '', '删除部门', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:division:delete', '2025-09-12 14:51:51', '2025-09-12 14:51:51', NULL, 1);
INSERT INTO sys_menu VALUES (140232, 1005, '', '', '', '', '新增', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:dict:add', '2025-09-16 16:38:06', '2025-09-16 16:38:06', NULL, 1);
INSERT INTO sys_menu VALUES (140233, 1005, '', '', '', '', '编辑', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:dict:edit', '2025-09-16 16:39:58', '2025-09-16 16:39:58', NULL, 1);
INSERT INTO sys_menu VALUES (140234, 1005, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:dict:delete', '2025-09-16 16:40:19', '2025-09-16 16:40:19', NULL, 1);
INSERT INTO sys_menu VALUES (140235, 1005, '', '', '', '', '字典项管理', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:dictitem:list', '2025-09-16 17:09:58', '2025-09-16 17:31:35', NULL, 1);
INSERT INTO sys_menu VALUES (140236, 1005, '', '', '', '', '新增字典项', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:dictitem:add', '2025-09-16 17:32:06', '2025-09-16 17:32:06', NULL, 1);
INSERT INTO sys_menu VALUES (140237, 1005, '', '', '', '', '编辑字典项', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:dictitem:edit', '2025-09-16 17:33:16', '2025-09-16 17:33:16', NULL, 1);
INSERT INTO sys_menu VALUES (140238, 1005, '', '', '', '', '删除字典项', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:dictitem:delete', '2025-09-16 17:33:41', '2025-09-16 17:33:41', NULL, 1);
INSERT INTO sys_menu VALUES (140239, 10, '/system/affix', 'SystemAffix', '', 'system/affix/affix', 'file-manager', '0', '0', '0', '1', '0', '', '0', '', 'IconFolder', 0, '2', '0', '', '2025-09-25 15:17:00', '2025-10-15 18:14:16', NULL, 1);
INSERT INTO sys_menu VALUES (140240, 140239, '', '', '', '', '文件上传', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:affix:upload', '2025-09-25 15:45:29', '2025-09-25 15:46:29', NULL, 1);
INSERT INTO sys_menu VALUES (140241, 140239, '', '', '', '', '删除文件', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:affix:delete', '2025-09-25 15:46:52', '2025-09-25 15:46:52', NULL, 1);
INSERT INTO sys_menu VALUES (140242, 140239, '', '', '', '', '修改文件名', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:affix:updateName', '2025-09-25 15:47:41', '2025-09-25 15:47:41', NULL, 1);
INSERT INTO sys_menu VALUES (140243, 140239, '', '', '', '', '下载文件', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:affix:download', '2025-09-25 15:48:56', '2025-09-25 15:48:56', NULL, 1);
INSERT INTO sys_menu VALUES (140244, 1002, '', '', '', '', '数据权限', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:role:dataScope', '2025-09-26 17:07:16', '2025-09-26 17:07:16', NULL, 1);
INSERT INTO sys_menu VALUES (140245, 10, '/system/sysconfig', 'SystemSysconfig', '', 'system/sysconfig/sysconfig', 'system-config', '0', '0', '0', '1', '0', '', '0', '', 'IconSettings', 0, '2', '0', '', '2025-10-09 16:15:21', '2025-10-15 18:10:54', NULL, 1);
INSERT INTO sys_menu VALUES (140246, 140245, '', '', '', '', '修改系统配置', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:config:update', '2025-10-09 16:24:33', '2025-10-09 16:24:33', NULL, 1);
INSERT INTO sys_menu VALUES (140247, 0, '/demo', 'Demo', '', '', 'plugin-example', '0', '0', '0', '1', '0', '', '0', 'more', '', 0, '1', '0', '', '2025-10-13 14:38:38', '2025-10-16 08:55:06', NULL, 1);
INSERT INTO sys_menu VALUES (140248, 140247, '/plugins/example', 'PluginsExample', '', 'plugins/example/views/examplelist', 'plugin-example', '0', '0', '0', '1', '0', '', '0', '', 'IconMenu', 0, '2', '0', '', '2025-10-13 15:19:20', '2025-10-16 08:55:19', NULL, 1);
INSERT INTO sys_menu VALUES (140249, 140248, '', '', '', '', '新增', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'plugins:example:add', '2025-10-14 11:02:42', '2025-10-14 11:02:42', NULL, 1);
INSERT INTO sys_menu VALUES (140250, 140248, '', '', '', '', '编辑', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'plugins:example:edit', '2025-10-14 11:03:08', '2025-10-14 11:03:08', NULL, 1);
INSERT INTO sys_menu VALUES (140251, 140248, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'plugins:example:delete', '2025-10-14 11:03:25', '2025-10-14 11:03:25', NULL, 1);
INSERT INTO sys_menu VALUES (140252, 1007, '', '', '', '', '修改密码、手机号等', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:userinfo:updateAccount', '2025-10-17 11:12:56', '2025-10-17 11:12:56', NULL, 1);
INSERT INTO sys_menu VALUES (140254, 140239, '', '', '', '', '复制链接', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:affix:copy', '2025-10-17 11:38:09', '2025-10-17 11:38:09', NULL, 1);
INSERT INTO sys_menu VALUES (140255, 1006, '', '', '', '', '导出', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:log:export', '2025-10-20 10:16:51', '2025-10-20 10:16:51', NULL, 1);
INSERT INTO sys_menu VALUES (140256, 1006, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:log:delete', '2025-10-20 10:17:19', '2025-10-20 10:17:19', NULL, 1);
INSERT INTO sys_menu VALUES (140257, 1003, '', '', '', '', '导出', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:menu:export', '2025-10-20 17:18:01', '2025-10-20 17:18:13', NULL, 1);
INSERT INTO sys_menu VALUES (140258, 1003, '', '', '', '', '导入', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:menu:import', '2025-10-21 11:29:45', '2025-10-21 11:29:45', NULL, 1);
INSERT INTO sys_menu VALUES (140264, 1007, '', '', '', '', '修改用户基本信息', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:userinfo:updateBasicInfo', '2025-10-31 09:26:42', '2025-10-31 09:26:42', NULL, 1);
INSERT INTO sys_menu VALUES (140265, 10, '/system/codegen', 'SystemCodegen', '', 'system/codegen/codegen', 'codegen', '0', '0', '0', '1', '0', '', '0', '', 'IconCode', 0, '2', '0', '', '2025-11-04 11:45:49', '2025-11-04 11:45:49', NULL, 1);
INSERT INTO sys_menu VALUES (140329, 140265, '', '', '', '', '导入表', '0', '0', '0', '1', '0', '', '0', '', '', 1, '3', '0', 'system:codegen:batchInsert', '2025-11-17 15:32:25', '2025-11-17 15:32:25', NULL, 1);
INSERT INTO sys_menu VALUES (140330, 140265, '', '', '', '', '配置', '0', '0', '0', '1', '0', '', '0', '', '', 1, '3', '0', 'system:codegen:update', '2025-11-17 15:33:57', '2025-11-17 15:33:57', NULL, 1);
INSERT INTO sys_menu VALUES (140331, 140265, '', '', '', '', '预览', '0', '0', '0', '1', '0', '', '0', '', '', 1, '3', '0', 'system:codegen:preview', '2025-11-17 15:34:24', '2025-11-17 15:34:24', NULL, 1);
INSERT INTO sys_menu VALUES (140332, 140265, '', '', '', '', '生成代码文件', '0', '0', '0', '1', '0', '', '0', '', '', 1, '3', '0', 'system:codegen:generate', '2025-11-17 15:35:00', '2025-11-17 15:35:00', NULL, 1);
INSERT INTO sys_menu VALUES (140333, 140265, '', '', '', '', '同步数据库', '0', '0', '0', '1', '0', '', '0', '', '', 1, '3', '0', 'system:codegen:refreshFields', '2025-11-17 15:35:51', '2025-11-17 15:35:51', NULL, 1);
INSERT INTO sys_menu VALUES (140334, 140265, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 1, '3', '0', 'system:codegen:delete', '2025-11-17 15:36:50', '2025-11-17 15:36:50', NULL, 1);
INSERT INTO sys_menu VALUES (140335, 140265, '', '', '', '', '生成菜单', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:codegen:insertmenuandapi', '2025-11-26 15:16:32', '2025-11-26 15:16:32', NULL, 1);
INSERT INTO sys_menu VALUES (140336, 10, '/system/pluginsmanager', 'SystemPluginsmanager', '', 'system/pluginsmanager/pluginsmanager', 'plugins-manager', '0', '0', '0', '1', '0', '', '0', '', 'IconApps', 0, '2', '0', '', '2025-12-05 17:59:34', '2025-12-05 17:59:34', NULL, 1);
INSERT INTO sys_menu VALUES (140338, 140336, '', '', '', '', '导出插件', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:pluginsmanager:export', '2025-12-08 16:33:32', '2025-12-08 16:33:32', NULL, 1);
INSERT INTO sys_menu VALUES (140339, 140336, '', '', '', '', '导入插件', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:pluginsmanager:import', '2025-12-08 16:33:51', '2025-12-08 16:33:51', NULL, 1);
INSERT INTO sys_menu VALUES (140340, 140336, '', '', '', '', '插件卸载', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:pluginsmanager:uninstall', '2025-12-08 16:34:53', '2025-12-08 16:34:53', NULL, 1);
INSERT INTO sys_menu VALUES (140341, 0, '/sysjobs', 'Sysjobs', '', '', 'sysjobs', '0', '0', '0', '1', '0', '', '0', 'functions', '', 0, '1', '0', '', '2026-02-11 11:29:40', '2026-02-11 11:38:29', NULL, 1);
INSERT INTO sys_menu VALUES (140342, 140341, '/system/sysjobslist', 'SystemSysjobslist', '', 'system/sysjobs/sysjobslist', 'jobslist', '0', '0', '0', '1', '0', '', '0', '', 'IconList', 0, '2', '0', '', '2026-02-11 11:36:54', '2026-02-11 11:36:54', NULL, 1);
INSERT INTO sys_menu VALUES (140343, 140342, '', '', '', '', '新增', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:sysjobs:add', '2026-02-11 11:43:35', '2026-02-11 11:43:35', NULL, 1);
INSERT INTO sys_menu VALUES (140344, 140342, '', '', '', '', '编辑', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:sysjobs:edit', '2026-02-11 11:44:00', '2026-02-11 11:44:00', NULL, 1);
INSERT INTO sys_menu VALUES (140345, 140342, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:sysjobs:delete', '2026-02-11 11:44:22', '2026-02-11 11:44:22', NULL, 1);
INSERT INTO sys_menu VALUES (140346, 140342, '', '', '', '', '执行一次', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:sysjobs:executeNow', '2026-02-12 17:59:02', '2026-02-12 17:59:02', NULL, 1);
INSERT INTO sys_menu VALUES (140347, 140341, '/system/joblog', 'SystemJoblog', '', 'system/sysjobresults/sysjobresultslist', 'joblog', '0', '0', '0', '1', '0', '', '0', '', 'IconHistory', 0, '2', '0', '', '2026-02-11 11:41:27', '2026-02-11 11:41:27', NULL, 1);
INSERT INTO sys_menu VALUES (140348, 140347, '', '', '', '', '删除', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:sysjobresults:delete', '2026-02-11 11:45:18', '2026-02-11 11:45:18', NULL, 1);
INSERT INTO sys_menu VALUES (140349, 140239, '', '', '', '', '大文件上传', '0', '0', '0', '1', '0', '', '0', '', '', 0, '3', '0', 'system:affix:bigupload', '2026-04-09 15:47:39', '2026-04-09 15:47:39', NULL, 1);
-- Table structure for sys_menu_api
DROP TABLE IF EXISTS sys_menu_api;
CREATE TABLE sys_menu_api (
    menu_id INTEGER NOT NULL,
    api_id INTEGER NOT NULL,
    PRIMARY KEY (menu_id,api_id)
);


-- Records of sys_menu_api
INSERT INTO sys_menu_api VALUES (10, 5);
INSERT INTO sys_menu_api VALUES (10, 6);
INSERT INTO sys_menu_api VALUES (10, 7);
INSERT INTO sys_menu_api VALUES (10, 12);
INSERT INTO sys_menu_api VALUES (10, 27);
INSERT INTO sys_menu_api VALUES (10, 54);
INSERT INTO sys_menu_api VALUES (10, 202);
INSERT INTO sys_menu_api VALUES (1001, 7);
INSERT INTO sys_menu_api VALUES (1001, 8);
INSERT INTO sys_menu_api VALUES (1001, 18);
INSERT INTO sys_menu_api VALUES (1001, 19);
INSERT INTO sys_menu_api VALUES (1002, 19);
INSERT INTO sys_menu_api VALUES (1003, 13);
INSERT INTO sys_menu_api VALUES (1004, 18);
INSERT INTO sys_menu_api VALUES (1004, 37);
INSERT INTO sys_menu_api VALUES (1005, 41);
INSERT INTO sys_menu_api VALUES (1006, 70);
INSERT INTO sys_menu_api VALUES (1007, 6);
INSERT INTO sys_menu_api VALUES (140213, 29);
INSERT INTO sys_menu_api VALUES (140214, 9);
INSERT INTO sys_menu_api VALUES (140215, 10);
INSERT INTO sys_menu_api VALUES (140216, 11);
INSERT INTO sys_menu_api VALUES (140218, 24);
INSERT INTO sys_menu_api VALUES (140219, 25);
INSERT INTO sys_menu_api VALUES (140220, 26);
INSERT INTO sys_menu_api VALUES (140221, 13);
INSERT INTO sys_menu_api VALUES (140221, 20);
INSERT INTO sys_menu_api VALUES (140221, 21);
INSERT INTO sys_menu_api VALUES (140222, 15);
INSERT INTO sys_menu_api VALUES (140223, 16);
INSERT INTO sys_menu_api VALUES (140224, 17);
INSERT INTO sys_menu_api VALUES (140224, 197);
INSERT INTO sys_menu_api VALUES (140225, 29);
INSERT INTO sys_menu_api VALUES (140225, 35);
INSERT INTO sys_menu_api VALUES (140225, 36);
INSERT INTO sys_menu_api VALUES (140226, 31);
INSERT INTO sys_menu_api VALUES (140227, 30);
INSERT INTO sys_menu_api VALUES (140227, 32);
INSERT INTO sys_menu_api VALUES (140228, 33);
INSERT INTO sys_menu_api VALUES (140229, 38);
INSERT INTO sys_menu_api VALUES (140230, 39);
INSERT INTO sys_menu_api VALUES (140231, 40);
INSERT INTO sys_menu_api VALUES (140232, 43);
INSERT INTO sys_menu_api VALUES (140233, 44);
INSERT INTO sys_menu_api VALUES (140234, 45);
INSERT INTO sys_menu_api VALUES (140235, 48);
INSERT INTO sys_menu_api VALUES (140236, 50);
INSERT INTO sys_menu_api VALUES (140237, 51);
INSERT INTO sys_menu_api VALUES (140238, 52);
INSERT INTO sys_menu_api VALUES (140239, 58);
INSERT INTO sys_menu_api VALUES (140240, 55);
INSERT INTO sys_menu_api VALUES (140241, 56);
INSERT INTO sys_menu_api VALUES (140242, 57);
INSERT INTO sys_menu_api VALUES (140243, 60);
INSERT INTO sys_menu_api VALUES (140244, 61);
INSERT INTO sys_menu_api VALUES (140245, 62);
INSERT INTO sys_menu_api VALUES (140245, 64);
INSERT INTO sys_menu_api VALUES (140246, 63);
INSERT INTO sys_menu_api VALUES (140248, 65);
INSERT INTO sys_menu_api VALUES (140248, 69);
INSERT INTO sys_menu_api VALUES (140249, 66);
INSERT INTO sys_menu_api VALUES (140250, 67);
INSERT INTO sys_menu_api VALUES (140251, 68);
INSERT INTO sys_menu_api VALUES (140252, 53);
INSERT INTO sys_menu_api VALUES (140254, 60);
INSERT INTO sys_menu_api VALUES (140255, 73);
INSERT INTO sys_menu_api VALUES (140256, 72);
INSERT INTO sys_menu_api VALUES (140257, 74);
INSERT INTO sys_menu_api VALUES (140258, 75);
INSERT INTO sys_menu_api VALUES (140264, 89);
INSERT INTO sys_menu_api VALUES (140265, 190);
INSERT INTO sys_menu_api VALUES (140329, 188);
INSERT INTO sys_menu_api VALUES (140329, 191);
INSERT INTO sys_menu_api VALUES (140330, 192);
INSERT INTO sys_menu_api VALUES (140330, 193);
INSERT INTO sys_menu_api VALUES (140331, 189);
INSERT INTO sys_menu_api VALUES (140332, 105);
INSERT INTO sys_menu_api VALUES (140333, 195);
INSERT INTO sys_menu_api VALUES (140334, 194);
INSERT INTO sys_menu_api VALUES (140335, 196);
INSERT INTO sys_menu_api VALUES (140336, 198);
INSERT INTO sys_menu_api VALUES (140338, 199);
INSERT INTO sys_menu_api VALUES (140339, 200);
INSERT INTO sys_menu_api VALUES (140340, 201);
INSERT INTO sys_menu_api VALUES (140342, 203);
INSERT INTO sys_menu_api VALUES (140342, 204);
INSERT INTO sys_menu_api VALUES (140343, 205);
INSERT INTO sys_menu_api VALUES (140344, 206);
INSERT INTO sys_menu_api VALUES (140344, 207);
INSERT INTO sys_menu_api VALUES (140344, 208);
INSERT INTO sys_menu_api VALUES (140345, 209);
INSERT INTO sys_menu_api VALUES (140346, 210);
INSERT INTO sys_menu_api VALUES (140347, 211);
INSERT INTO sys_menu_api VALUES (140348, 212);
INSERT INTO sys_menu_api VALUES (140349, 55);
INSERT INTO sys_menu_api VALUES (140349, 213);
INSERT INTO sys_menu_api VALUES (140349, 214);
INSERT INTO sys_menu_api VALUES (140349, 215);
INSERT INTO sys_menu_api VALUES (140349, 216);
-- Table structure for sys_operation_logs
DROP TABLE IF EXISTS sys_operation_logs;
CREATE TABLE sys_operation_logs (
    id BIGSERIAL,
    created_at TIMESTAMP(3),
    updated_at TIMESTAMP(3),
    deleted_at TIMESTAMP(3),
    user_id BIGINT,
    username VARCHAR(50),
    module VARCHAR(100),
    operation VARCHAR(100),
    method VARCHAR(10),
    path VARCHAR(500),
    ip VARCHAR(50),
    user_agent VARCHAR(500),
    request_data TEXT,
    response_data TEXT,
    status_code INTEGER,
    duration BIGINT,
    error_msg TEXT,
    location VARCHAR(100),
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_operation_logs.user_agent IS '用户代理';
COMMENT ON COLUMN sys_operation_logs.error_msg IS '错误信息';
COMMENT ON COLUMN sys_operation_logs.user_id IS '操作用户ID';
COMMENT ON COLUMN sys_operation_logs.module IS '操作模块';
COMMENT ON COLUMN sys_operation_logs.method IS '请求方法';
COMMENT ON COLUMN sys_operation_logs.path IS '请求路径';
COMMENT ON COLUMN sys_operation_logs.response_data IS '响应数据';
COMMENT ON COLUMN sys_operation_logs.ip IS '客户端IP';
COMMENT ON COLUMN sys_operation_logs.status_code IS '响应状态码';
COMMENT ON COLUMN sys_operation_logs.location IS '操作地点';
COMMENT ON COLUMN sys_operation_logs.username IS '操作用户名';
COMMENT ON COLUMN sys_operation_logs.request_data IS '请求参数';
COMMENT ON COLUMN sys_operation_logs.duration IS '操作耗时(毫秒)';
COMMENT ON COLUMN sys_operation_logs.operation IS '操作类型';

-- Records of sys_operation_logs
-- Table structure for sys_role
DROP TABLE IF EXISTS sys_role;
CREATE TABLE sys_role (
    id SERIAL,
    name VARCHAR(255) DEFAULT '',
    sort INTEGER DEFAULT 0,
    status SMALLINT DEFAULT 0,
    description VARCHAR(255),
    parent_id INTEGER DEFAULT 0,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER,
    data_scope INTEGER DEFAULT 0,
    checked_depts VARCHAR(1000),
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_role.name IS '角色名称';
COMMENT ON COLUMN sys_role.sort IS '排序';
COMMENT ON COLUMN sys_role.status IS '状态';
COMMENT ON COLUMN sys_role.description IS '描述';
COMMENT ON COLUMN sys_role.data_scope IS '数据权限';
COMMENT ON COLUMN sys_role.checked_depts IS '数据权限关联的部门';

-- Records of sys_role
INSERT INTO sys_role VALUES (1, '系统管理员', 0, 1, '最高权限管理员角色', 0, '2025-09-01 17:32:12', '2025-09-30 15:53:24', NULL, 1, 1, '');
INSERT INTO sys_role VALUES (2, '演示', 0, 1, '', 0, '2025-10-14 15:12:09', '2025-10-17 15:34:47', NULL, 1, 0, '');
-- Table structure for sys_role_menu
DROP TABLE IF EXISTS sys_role_menu;
CREATE TABLE sys_role_menu (
    role_id INTEGER NOT NULL,
    menu_id INTEGER NOT NULL,
    PRIMARY KEY (role_id,menu_id)
);


-- Records of sys_role_menu
INSERT INTO sys_role_menu VALUES (1, 1);
INSERT INTO sys_role_menu VALUES (1, 10);
INSERT INTO sys_role_menu VALUES (1, 1001);
INSERT INTO sys_role_menu VALUES (1, 1002);
INSERT INTO sys_role_menu VALUES (1, 1003);
INSERT INTO sys_role_menu VALUES (1, 1004);
INSERT INTO sys_role_menu VALUES (1, 1005);
INSERT INTO sys_role_menu VALUES (1, 1006);
INSERT INTO sys_role_menu VALUES (1, 1007);
INSERT INTO sys_role_menu VALUES (1, 140213);
INSERT INTO sys_role_menu VALUES (1, 140214);
INSERT INTO sys_role_menu VALUES (1, 140215);
INSERT INTO sys_role_menu VALUES (1, 140216);
INSERT INTO sys_role_menu VALUES (1, 140218);
INSERT INTO sys_role_menu VALUES (1, 140219);
INSERT INTO sys_role_menu VALUES (1, 140220);
INSERT INTO sys_role_menu VALUES (1, 140221);
INSERT INTO sys_role_menu VALUES (1, 140222);
INSERT INTO sys_role_menu VALUES (1, 140223);
INSERT INTO sys_role_menu VALUES (1, 140224);
INSERT INTO sys_role_menu VALUES (1, 140225);
INSERT INTO sys_role_menu VALUES (1, 140226);
INSERT INTO sys_role_menu VALUES (1, 140227);
INSERT INTO sys_role_menu VALUES (1, 140228);
INSERT INTO sys_role_menu VALUES (1, 140229);
INSERT INTO sys_role_menu VALUES (1, 140230);
INSERT INTO sys_role_menu VALUES (1, 140231);
INSERT INTO sys_role_menu VALUES (1, 140232);
INSERT INTO sys_role_menu VALUES (1, 140233);
INSERT INTO sys_role_menu VALUES (1, 140234);
INSERT INTO sys_role_menu VALUES (1, 140235);
INSERT INTO sys_role_menu VALUES (1, 140236);
INSERT INTO sys_role_menu VALUES (1, 140237);
INSERT INTO sys_role_menu VALUES (1, 140238);
INSERT INTO sys_role_menu VALUES (1, 140239);
INSERT INTO sys_role_menu VALUES (1, 140240);
INSERT INTO sys_role_menu VALUES (1, 140241);
INSERT INTO sys_role_menu VALUES (1, 140242);
INSERT INTO sys_role_menu VALUES (1, 140243);
INSERT INTO sys_role_menu VALUES (1, 140244);
INSERT INTO sys_role_menu VALUES (1, 140245);
INSERT INTO sys_role_menu VALUES (1, 140246);
INSERT INTO sys_role_menu VALUES (1, 140247);
INSERT INTO sys_role_menu VALUES (1, 140248);
INSERT INTO sys_role_menu VALUES (1, 140249);
INSERT INTO sys_role_menu VALUES (1, 140250);
INSERT INTO sys_role_menu VALUES (1, 140251);
INSERT INTO sys_role_menu VALUES (1, 140252);
INSERT INTO sys_role_menu VALUES (1, 140254);
INSERT INTO sys_role_menu VALUES (1, 140255);
INSERT INTO sys_role_menu VALUES (1, 140256);
INSERT INTO sys_role_menu VALUES (1, 140257);
INSERT INTO sys_role_menu VALUES (1, 140258);
INSERT INTO sys_role_menu VALUES (1, 140264);
INSERT INTO sys_role_menu VALUES (1, 140265);
INSERT INTO sys_role_menu VALUES (1, 140329);
INSERT INTO sys_role_menu VALUES (1, 140330);
INSERT INTO sys_role_menu VALUES (1, 140331);
INSERT INTO sys_role_menu VALUES (1, 140332);
INSERT INTO sys_role_menu VALUES (1, 140333);
INSERT INTO sys_role_menu VALUES (1, 140334);
INSERT INTO sys_role_menu VALUES (1, 140335);
INSERT INTO sys_role_menu VALUES (1, 140336);
INSERT INTO sys_role_menu VALUES (1, 140338);
INSERT INTO sys_role_menu VALUES (1, 140339);
INSERT INTO sys_role_menu VALUES (1, 140340);
INSERT INTO sys_role_menu VALUES (2, 1);
INSERT INTO sys_role_menu VALUES (2, 10);
INSERT INTO sys_role_menu VALUES (2, 1001);
INSERT INTO sys_role_menu VALUES (2, 1002);
INSERT INTO sys_role_menu VALUES (2, 1003);
INSERT INTO sys_role_menu VALUES (2, 1004);
INSERT INTO sys_role_menu VALUES (2, 1005);
INSERT INTO sys_role_menu VALUES (2, 1006);
INSERT INTO sys_role_menu VALUES (2, 1007);
INSERT INTO sys_role_menu VALUES (2, 140213);
INSERT INTO sys_role_menu VALUES (2, 140214);
INSERT INTO sys_role_menu VALUES (2, 140215);
INSERT INTO sys_role_menu VALUES (2, 140216);
INSERT INTO sys_role_menu VALUES (2, 140218);
INSERT INTO sys_role_menu VALUES (2, 140219);
INSERT INTO sys_role_menu VALUES (2, 140220);
INSERT INTO sys_role_menu VALUES (2, 140221);
INSERT INTO sys_role_menu VALUES (2, 140222);
INSERT INTO sys_role_menu VALUES (2, 140223);
INSERT INTO sys_role_menu VALUES (2, 140224);
INSERT INTO sys_role_menu VALUES (2, 140225);
INSERT INTO sys_role_menu VALUES (2, 140226);
INSERT INTO sys_role_menu VALUES (2, 140227);
INSERT INTO sys_role_menu VALUES (2, 140228);
INSERT INTO sys_role_menu VALUES (2, 140229);
INSERT INTO sys_role_menu VALUES (2, 140230);
INSERT INTO sys_role_menu VALUES (2, 140231);
INSERT INTO sys_role_menu VALUES (2, 140232);
INSERT INTO sys_role_menu VALUES (2, 140233);
INSERT INTO sys_role_menu VALUES (2, 140234);
INSERT INTO sys_role_menu VALUES (2, 140235);
INSERT INTO sys_role_menu VALUES (2, 140236);
INSERT INTO sys_role_menu VALUES (2, 140237);
INSERT INTO sys_role_menu VALUES (2, 140238);
INSERT INTO sys_role_menu VALUES (2, 140239);
INSERT INTO sys_role_menu VALUES (2, 140240);
INSERT INTO sys_role_menu VALUES (2, 140241);
INSERT INTO sys_role_menu VALUES (2, 140242);
INSERT INTO sys_role_menu VALUES (2, 140243);
INSERT INTO sys_role_menu VALUES (2, 140244);
INSERT INTO sys_role_menu VALUES (2, 140245);
INSERT INTO sys_role_menu VALUES (2, 140246);
INSERT INTO sys_role_menu VALUES (2, 140247);
INSERT INTO sys_role_menu VALUES (2, 140248);
INSERT INTO sys_role_menu VALUES (2, 140249);
INSERT INTO sys_role_menu VALUES (2, 140250);
INSERT INTO sys_role_menu VALUES (2, 140251);
INSERT INTO sys_role_menu VALUES (2, 140252);
INSERT INTO sys_role_menu VALUES (2, 140254);
INSERT INTO sys_role_menu VALUES (2, 140255);
INSERT INTO sys_role_menu VALUES (2, 140256);
INSERT INTO sys_role_menu VALUES (2, 140257);
INSERT INTO sys_role_menu VALUES (2, 140258);
INSERT INTO sys_role_menu VALUES (2, 140264);
INSERT INTO sys_role_menu VALUES (2, 140265);
INSERT INTO sys_role_menu VALUES (2, 140329);
INSERT INTO sys_role_menu VALUES (2, 140330);
INSERT INTO sys_role_menu VALUES (2, 140331);
INSERT INTO sys_role_menu VALUES (2, 140332);
INSERT INTO sys_role_menu VALUES (2, 140333);
INSERT INTO sys_role_menu VALUES (2, 140334);
INSERT INTO sys_role_menu VALUES (2, 140335);
INSERT INTO sys_role_menu VALUES (2, 140336);
INSERT INTO sys_role_menu VALUES (2, 140338);
INSERT INTO sys_role_menu VALUES (2, 140339);
INSERT INTO sys_role_menu VALUES (2, 140340);
    id SERIAL,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER NOT NULL DEFAULT 0,
    name VARCHAR(100) NOT NULL,
    code VARCHAR(50) NOT NULL,
    description VARCHAR(500),
    status SMALLINT NOT NULL DEFAULT 1,
    domain VARCHAR(255),
    platform_domain VARCHAR(255),
    menu_permission VARCHAR(1000),
    PRIMARY KEY (id)
);


-- Table structure for sys_users
DROP TABLE IF EXISTS sys_users;
CREATE TABLE sys_users (
    id SERIAL,
    username VARCHAR(50) NOT NULL DEFAULT '',
    password VARCHAR(255) NOT NULL DEFAULT '',
    email VARCHAR(100) DEFAULT '',
    status BOOLEAN DEFAULT true,
    dept_id INTEGER DEFAULT 0,
    phone VARCHAR(64) DEFAULT '',
    sex VARCHAR(64) DEFAULT '',
    nick_name VARCHAR(100) DEFAULT '',
    avatar VARCHAR(255) DEFAULT '',
    description VARCHAR(500),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER DEFAULT 0,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_users.phone IS '电话';
COMMENT ON COLUMN sys_users.sex IS '性别';
COMMENT ON COLUMN sys_users.description IS '描述';
COMMENT ON COLUMN sys_users.created_by IS '创建人';
COMMENT ON COLUMN sys_users.username IS '用户名';
COMMENT ON COLUMN sys_users.email IS '邮箱';
COMMENT ON COLUMN sys_users.nick_name IS '昵称';
COMMENT ON COLUMN sys_users.avatar IS '头像';
COMMENT ON COLUMN sys_users.password IS '密码';
COMMENT ON COLUMN sys_users.status IS '是否启用 0停用 1启用';
COMMENT ON COLUMN sys_users.dept_id IS '部门ID';

-- Records of sys_users
INSERT INTO sys_users VALUES (1, 'admin', '$2a$10$0aS9FxWlOz/PXiqzsBr7huy.Dqdwucyb795qiWcA6fsn0Lu.GLA.C', 'admin@example.com', '1', 1, '18800000006', '1', '超级管理员', '/public/uploads/2025-11-04/20251104_0945787a-8536-45fc-ba75-e94c8daaec06.jpeg', '超级管理员', '2025-08-18 14:55:05', '2025-11-17 17:38:01', NULL, 0);
INSERT INTO sys_users VALUES (4, 'demo', '$2a$10$yxq80jnZCRPn/hhQYUffheRnDopYjiq1AKGdgrg1oatLha7tc/.Qe', '', '1', 1, '', '1', '演示账号', '', '演示账号', '2025-10-17 15:38:37', '2025-10-31 16:32:34', NULL, 1);
-- Table structure for sys_user_role
DROP TABLE IF EXISTS sys_user_role;
CREATE TABLE sys_user_role (
    user_id INTEGER NOT NULL DEFAULT 0,
    role_id INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id,role_id)
);

COMMENT ON COLUMN sys_user_role.user_id IS '用户ID';
COMMENT ON COLUMN sys_user_role.role_id IS '角色ID';

-- Records of sys_user_role
INSERT INTO sys_user_role VALUES (1, 1);
INSERT INTO sys_user_role VALUES (4, 2);
-- Table structure for sys_param
DROP TABLE IF EXISTS sys_param;
CREATE TABLE sys_param (
    id BIGSERIAL,
    name VARCHAR(255),
    code VARCHAR(255) NOT NULL,
    value TEXT,
    status SMALLINT DEFAULT 1,
    description VARCHAR(500),
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by INTEGER DEFAULT 0,
    PRIMARY KEY (id)
);

COMMENT ON COLUMN sys_param.id IS 'ID';
COMMENT ON COLUMN sys_param.name IS '参数名称';
COMMENT ON COLUMN sys_param.code IS '参数唯一标识';
COMMENT ON COLUMN sys_param.value IS '参数值';
COMMENT ON COLUMN sys_param.status IS '状态(0禁用/1启用)';
COMMENT ON COLUMN sys_param.description IS '描述';
COMMENT ON COLUMN sys_param.created_by IS '创建人';

CREATE UNIQUE INDEX idx_sys_param_code ON sys_param (code);
CREATE INDEX idx_sys_param_deleted_at ON sys_param (deleted_at);

-- SIP 首次部署配置 (2026-07-20 起 gb_sip_config 是引导判据的唯一权威源)
DROP TABLE IF EXISTS gb_sip_config;

CREATE TABLE gb_sip_config (
    id SMALLINT NOT NULL,
    deployment_mode VARCHAR(8) NOT NULL,
    listen_ip VARCHAR(45) NOT NULL,
    advertise_ip VARCHAR(45) NOT NULL,
    advertise_ip_inferred BOOLEAN NOT NULL DEFAULT FALSE,
    port INTEGER NOT NULL,
    domain VARCHAR(10) NOT NULL,
    server_id VARCHAR(20) NOT NULL,
    password VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    CONSTRAINT chk_gb_sip_config_singleton CHECK (id = 1),
    CONSTRAINT chk_gb_sip_config_deployment_mode CHECK (deployment_mode IN ('lan', 'public')),
    CONSTRAINT chk_gb_sip_config_port CHECK (port BETWEEN 1 AND 65535)
);

COMMENT ON TABLE gb_sip_config IS 'GB28181 SIP 运行时配置';
COMMENT ON COLUMN gb_sip_config.advertise_ip_inferred IS '宣告地址是否由系统推断';
COMMENT ON COLUMN gb_sip_config.password IS 'SIP Digest 原始凭据';

-- 首装用户不 seed gb_sip_config,DB 为空触发引导页.
-- 老 stack 升级由 setup.MigrateYAMLToDB 一次性从 config.yml 搬迁到本表.

-- 创建索引
CREATE INDEX sys_jobs_idx_group ON sys_jobs ("group");
CREATE INDEX sys_jobs_idx_status ON sys_jobs (status);
CREATE INDEX sys_jobs_idx_executor_name ON sys_jobs (executor_name);
CREATE INDEX sys_jobs_idx_created_at ON sys_jobs (created_at);
CREATE INDEX sys_job_results_idx_job_id ON sys_job_results (job_id);
CREATE INDEX sys_job_results_idx_status ON sys_job_results (status);
CREATE INDEX sys_job_results_idx_start_time ON sys_job_results (start_time);
CREATE INDEX sys_job_results_idx_created_at ON sys_job_results (created_at);
CREATE INDEX sys_menu_idx_parent_id ON sys_menu (parent_id);
CREATE INDEX sys_menu_idx_sort ON sys_menu (sort);
CREATE INDEX sys_menu_idx_type ON sys_menu (type);
CREATE UNIQUE INDEX sys_users_username ON sys_users (username);
CREATE INDEX sys_affix_idx_sys_affix_file_md5 ON sys_affix (file_md5);
CREATE UNIQUE INDEX sys_casbin_rule_idx_casbin_rule ON sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5);
CREATE UNIQUE INDEX sys_casbin_rule_idx_sys_casbin_rule ON sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5);
CREATE INDEX sys_affix_chunk_idx_upload_id ON sys_affix_chunk (upload_id);
CREATE INDEX sys_affix_chunk_idx_file_md5 ON sys_affix_chunk (file_md5);
CREATE INDEX sys_operation_logs_idx_sys_operation_logs_deleted_at ON sys_operation_logs (deleted_at);
CREATE INDEX sys_operation_logs_idx_user_id ON sys_operation_logs (user_id);

-- 设置序列值
SELECT setval('sys_department_id_seq', 1, true);
SELECT setval('sys_gen_id_seq', 24, true);
-- 表 demo_teacher 的列 id 没有数据，序列 demo_teacher_id_seq 将保持默认起始值
-- 表 sys_affix_chunk 的列 id 没有数据，序列 sys_affix_chunk_id_seq 将保持默认起始值
SELECT setval('sys_dict_id_seq', 4, true);
SELECT setval('sys_dict_item_id_seq', 42, true);
-- 表 sys_operation_logs 的列 id 没有数据，序列 sys_operation_logs_id_seq 将保持默认起始值
-- 表 sys_job_results 的列 id 没有数据，序列 sys_job_results_id_seq 将保持默认起始值
SELECT setval('sys_gen_field_id_seq', 213, true);
SELECT setval('sys_menu_id_seq', 140349, true);
SELECT setval('sys_role_id_seq', 2, true);
SELECT setval('sys_users_id_seq', 4, true);
-- 表 demo_students 的列 student_id 没有数据，序列 demo_students_student_id_seq 将保持默认起始值
SELECT setval('example_id_seq', 15, true);
-- 表 sys_affix 的列 id 没有数据，序列 sys_affix_id_seq 将保持默认起始值
SELECT setval('sys_api_id_seq', 216, true);
SELECT setval('sys_casbin_rule_id_seq', 7560, true);
SELECT setval('sys_param_id_seq', 4, true);

-- UVP UI language: use Lucide icons for menu entries.
UPDATE sys_menu
SET
    svg_icon = '',
    icon = CASE id
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
        ELSE icon
    END
WHERE id IN (
    1, 10, 1001, 1002, 1003, 1004, 1005, 1006, 1007,
    140213, 140239, 140245, 140247, 140248, 140265,
    140336, 140341, 140342, 140347, 140350, 140351, 140352,
    140353, 140354, 140355, 140357, 140358
);


SET session_replication_role = DEFAULT;
SET client_min_messages TO NOTICE;

-- SIP setup API, UI permissions and administrator policies.
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(217,'读取 SIP 配置状态','/api/gb28181/sip/setup/status','GET','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(218,'读取本机网络接口','/api/gb28181/sip/setup/network-interfaces','GET','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(219,'读取 SIP 平台信息','/api/gb28181/sip/platform','GET','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(220,'保存 SIP 配置','/api/gb28181/sip/setup/config','PUT','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(221,'暂缓 SIP 配置','/api/gb28181/sip/setup/skip','POST','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by) VALUES
(140359,140355,'','','','查看 SIP 配置',1,3,'gb28181:sip:config:view',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),
(140360,140355,'','','','修改 SIP 配置',1,3,'gb28181:sip:config:update',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,140359),(1,140360);
INSERT INTO sys_menu_api (menu_id,api_id) VALUES
(140359,217),(140359,218),(140359,219),(140360,220),(140360,221);
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5) VALUES
(7561,'p','role_1','/api/gb28181/sip/setup/status','GET','*','',''),
(7562,'p','role_1','/api/gb28181/sip/setup/network-interfaces','GET','*','',''),
(7563,'p','role_1','/api/gb28181/sip/platform','GET','*','',''),
(7564,'p','role_1','/api/gb28181/sip/setup/config','PUT','*','',''),
(7565,'p','role_1','/api/gb28181/sip/setup/skip','POST','*','','');

-- Custom device groups reuse the device management page and expose one hidden permission.
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(222,'创建自定义分组','/api/gb28181/device-mgmt/custom-groups','POST','GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(223,'修改自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','PATCH','GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(224,'移动自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/move','POST','GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(225,'删除自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','DELETE','GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(226,'添加分组设备','/api/gb28181/device-mgmt/custom-groups/:id/devices','POST','GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(227,'移除分组设备','/api/gb28181/device-mgmt/custom-groups/:id/devices/remove','POST','GB28181 设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by) VALUES
(140361,140355,'','','','管理自定义分组',1,3,'gb28181:device-group:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,140361);
INSERT INTO sys_menu_api (menu_id,api_id) VALUES
(140361,222),(140361,223),(140361,224),(140361,225),(140361,226),(140361,227);
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5) VALUES
(7566,'p','role_1','/api/gb28181/device-mgmt/custom-groups','POST','*','',''),
(7567,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id','PATCH','*','',''),
(7568,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id/move','POST','*','',''),
(7569,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id','DELETE','*','',''),
(7570,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id/devices','POST','*','',''),
(7571,'p','role_1','/api/gb28181/device-mgmt/custom-groups/:id/devices/remove','POST','*','','');
SELECT setval('sys_api_id_seq',227,true);
SELECT setval('sys_menu_id_seq',140361,true);
SELECT setval('sys_casbin_rule_id_seq',7571,true);

-- Playback schemes reuse the multi-screen page and expose one hidden permission.
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(228,'查询播放方案','/api/gb28181/playback-schemes','GET','GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(229,'查看播放方案','/api/gb28181/playback-schemes/:id','GET','GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(230,'创建播放方案','/api/gb28181/playback-schemes','POST','GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(231,'重命名播放方案','/api/gb28181/playback-schemes/:id','PATCH','GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(232,'覆盖播放方案','/api/gb28181/playback-schemes/:id/layout','PUT','GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(233,'删除播放方案','/api/gb28181/playback-schemes/:id','DELETE','GB28181 多屏播放',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by) VALUES
(140362,140355,'','','','管理播放方案',1,3,'gb28181:playback-scheme:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,140362);
INSERT INTO sys_menu_api (menu_id,api_id) VALUES
(140362,228),(140362,229),(140362,230),(140362,231),(140362,232),(140362,233);
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5) VALUES
(7572,'p','role_1','/api/gb28181/playback-schemes','GET','*','',''),
(7573,'p','role_1','/api/gb28181/playback-schemes/:id','GET','*','',''),
(7574,'p','role_1','/api/gb28181/playback-schemes','POST','*','',''),
(7575,'p','role_1','/api/gb28181/playback-schemes/:id','PATCH','*','',''),
(7576,'p','role_1','/api/gb28181/playback-schemes/:id/layout','PUT','*','',''),
(7577,'p','role_1','/api/gb28181/playback-schemes/:id','DELETE','*','','');
SELECT setval('sys_api_id_seq',233,true);
SELECT setval('sys_menu_id_seq',140370,true);
SELECT setval('sys_casbin_rule_id_seq',7577,true);

-- GB28181 cascade API/menu/Casbin seed for fresh PostgreSQL installs.
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(234,'查看级联平台列表','/api/gb28181/cascade/platforms','GET','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(235,'创建级联平台','/api/gb28181/cascade/platforms','POST','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(236,'查看级联平台','/api/gb28181/cascade/platforms/:id','GET','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(237,'修改级联平台','/api/gb28181/cascade/platforms/:id','PUT','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(238,'删除级联平台','/api/gb28181/cascade/platforms/:id','DELETE','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(239,'启停级联平台','/api/gb28181/cascade/platforms/:id/enabled','PUT','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(240,'启用级联平台','/api/gb28181/cascade/platforms/:id/enable','POST','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(241,'停用级联平台','/api/gb28181/cascade/platforms/:id/disable','POST','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(242,'重连级联平台','/api/gb28181/cascade/platforms/:id/reconnect','POST','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(243,'查看级联共享','/api/gb28181/cascade/platforms/:id/shares','GET','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(244,'更新级联共享','/api/gb28181/cascade/platforms/:id/shares','PUT','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(245,'共享级联通道','/api/gb28181/cascade/platforms/:id/channels/share','POST','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(246,'取消级联通道共享','/api/gb28181/cascade/platforms/:id/channels/unshare','POST','GB28181 国标级联',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) VALUES
(140370,0,'/gb28181/cascade','gb28181-cascade','gb28181/cascade/index','国标级联',FALSE,FALSE,13,2,'','lucide:GitBranch',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by) VALUES
(140363,140370,'','','','查看国标级联',TRUE,3,'gb28181:cascade:view',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140364,140370,'','','','管理国标级联',TRUE,3,'gb28181:cascade:manage',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140365,140370,'','','','启停国标级联',TRUE,3,'gb28181:cascade:enable',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140366,140370,'','','','共享国标级联资源',TRUE,3,'gb28181:cascade:share',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140367,140370,'','','','重连国标级联',TRUE,3,'gb28181:cascade:reconnect',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,140370),(1,140363),(1,140364),(1,140365),(1,140366),(1,140367);
INSERT INTO sys_menu_api (menu_id,api_id) VALUES (140363,234),(140363,236),(140363,243),(140364,235),(140364,237),(140364,238),(140365,239),(140365,240),(140365,241),(140366,244),(140366,245),(140366,246),(140367,242);
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5) VALUES
(7578,'p','role_1','/api/gb28181/cascade/platforms','GET','*','',''),(7579,'p','role_1','/api/gb28181/cascade/platforms','POST','*','',''),(7580,'p','role_1','/api/gb28181/cascade/platforms/:id','GET','*','',''),(7581,'p','role_1','/api/gb28181/cascade/platforms/:id','PUT','*','',''),(7582,'p','role_1','/api/gb28181/cascade/platforms/:id','DELETE','*','',''),(7583,'p','role_1','/api/gb28181/cascade/platforms/:id/enabled','PUT','*','',''),(7584,'p','role_1','/api/gb28181/cascade/platforms/:id/enable','POST','*','',''),(7585,'p','role_1','/api/gb28181/cascade/platforms/:id/disable','POST','*','',''),(7586,'p','role_1','/api/gb28181/cascade/platforms/:id/reconnect','POST','*','',''),(7587,'p','role_1','/api/gb28181/cascade/platforms/:id/shares','GET','*','',''),(7588,'p','role_1','/api/gb28181/cascade/platforms/:id/shares','PUT','*','',''),(7589,'p','role_1','/api/gb28181/cascade/platforms/:id/channels/share','POST','*','',''),(7590,'p','role_1','/api/gb28181/cascade/platforms/:id/channels/unshare','POST','*','','');

-- Playback authorization settings for fresh PostgreSQL installs.
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(247,'读取播放鉴权配置','/api/gb28181/sip/service-config/play-auth','GET','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(248,'修改播放鉴权配置','/api/gb28181/sip/service-config/play-auth','PUT','GB28181 SIP 配置',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(249,'发起实时点播','/api/gb28181/play/:deviceId/:channelId','POST','GB28181 播放鉴权',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(250,'申请固定播放地址授权','/api/gb28181/play/:deviceId/:channelId/authorization','POST','GB28181 播放鉴权',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,type,permission,created_at,updated_at,created_by) VALUES
(140371,140355,'','','','发起实时点播',TRUE,3,'gb28181:play:start',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,140371);
INSERT INTO sys_menu_api (menu_id,api_id) VALUES (140359,247),(140360,248),(140371,249),(140371,250);
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5) VALUES
(7591,'p','role_1','/api/gb28181/sip/service-config/play-auth','GET','*','',''),
(7592,'p','role_1','/api/gb28181/sip/service-config/play-auth','PUT','*','',''),
(7593,'p','role_1','/api/gb28181/play/:deviceId/:channelId','POST','*','',''),
(7594,'p','role_1','/api/gb28181/play/:deviceId/:channelId/authorization','POST','*','','');
SELECT setval('sys_api_id_seq',250,true);
SELECT setval('sys_menu_id_seq',140371,true);
SELECT setval('sys_casbin_rule_id_seq',7594,true);

-- Cloud recording download task control APIs for fresh PostgreSQL installs.
-- The content route is authorized by a one-time HttpOnly cookie and is not seeded here.
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(251,'创建云端录像下载','/api/gb28181/cloud-recordings/files/:id/downloads','POST','GB28181 云端录像下载',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(252,'查询云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','GET','GB28181 云端录像下载',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(253,'取消云端录像下载','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE','GB28181 云端录像下载',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a
WHERE m.path='/gb28181/cloud-recordings' AND m.deleted_at IS NULL
  AND a.id IN (251,252,253);
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5)
VALUES
(7595,'p','role_1','/api/gb28181/cloud-recordings/files/:id/downloads','POST','*','',''),
(7596,'p','role_1','/api/gb28181/cloud-recordings/downloads/:taskId','GET','*','',''),
(7597,'p','role_1','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE','*','','');
SELECT setval('sys_api_id_seq',253,true);
SELECT setval('sys_casbin_rule_id_seq',7597,true);

-- ZLM media-node registry and durable endpoint-recovery gate.
DROP TABLE IF EXISTS meta_node;
CREATE TABLE meta_node (
    id BIGSERIAL,
    revision BIGINT NOT NULL DEFAULT 1,
    name VARCHAR(64) NOT NULL DEFAULT '',
    host VARCHAR(64) NOT NULL DEFAULT '',
    receive_host VARCHAR(255) NOT NULL DEFAULT '',
    playback_host VARCHAR(255) NOT NULL DEFAULT '',
    api_port INTEGER NOT NULL DEFAULT 18080,
    api_secret VARCHAR(128) NOT NULL DEFAULT '',
    media_server_uuid VARCHAR(64) NOT NULL DEFAULT '',
    weight INTEGER NOT NULL DEFAULT 50,
    tags_json TEXT,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    recovery_required BOOLEAN NOT NULL DEFAULT FALSE,
    recovery_reason VARCHAR(255) NOT NULL DEFAULT '',
    recovery_fingerprint CHAR(64) NOT NULL DEFAULT '',
    rtp_port_start INTEGER NOT NULL DEFAULT 30000,
    rtp_port_end INTEGER NOT NULL DEFAULT 35000,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    PRIMARY KEY (id),
    CONSTRAINT uk_media_server_uuid UNIQUE (media_server_uuid)
);
CREATE INDEX idx_state ON meta_node (state);
CREATE INDEX idx_recovery_required ON meta_node (recovery_required);

-- GB28181 device registry and dual-version profile archive.
DROP TABLE IF EXISTS gb_device;
CREATE TABLE gb_device (
    id BIGSERIAL,
    device_id VARCHAR(20) NOT NULL DEFAULT '',
    name VARCHAR(255) NOT NULL DEFAULT '',
    password VARCHAR(255) NOT NULL DEFAULT '',
    transport VARCHAR(8) NOT NULL DEFAULT '',
    manufacturer VARCHAR(255) NOT NULL DEFAULT '',
    model VARCHAR(255) NOT NULL DEFAULT '',
    firmware VARCHAR(255) NOT NULL DEFAULT '',
    ip VARCHAR(64) NOT NULL DEFAULT '',
    port INTEGER DEFAULT 0,
    register_time TIMESTAMP,
    register_expire_at TIMESTAMP,
    keepalive_time TIMESTAMP,
    keepalive_interval INTEGER DEFAULT 60,
    expires INTEGER DEFAULT 0,
    status SMALLINT DEFAULT 0,
    offline_at TIMESTAMP,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    deleted_at TIMESTAMP,
    created_by BIGINT DEFAULT 0,
    owner_dept_id BIGINT NOT NULL DEFAULT 0,
    reported_version VARCHAR(8) NOT NULL DEFAULT '',
    reported_version_at TIMESTAMP(3),
    protocol_override VARCHAR(8) NOT NULL DEFAULT 'auto',
    effective_version VARCHAR(8) NOT NULL DEFAULT '2016',
    effective_version_source VARCHAR(16) NOT NULL DEFAULT 'default',
    effective_version_at TIMESTAMP(3),
    zlm_node_id BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (id),
    CONSTRAINT uk_gb_device_id UNIQUE (device_id)
);
CREATE INDEX idx_gb_device_deleted_at ON gb_device (deleted_at);
CREATE INDEX idx_gb_device_owner_dept_deleted ON gb_device (owner_dept_id, deleted_at);
CREATE INDEX idx_gb_device_status_keepalive ON gb_device (status, keepalive_time);
CREATE INDEX idx_gb_device_zlm_node ON gb_device (zlm_node_id);

-- GB28181 PTZ / home-position tables (2026-07-24).
DROP TABLE IF EXISTS gb_ptz_home_position;
DROP TABLE IF EXISTS gb_ptz_operation_attempt;
DROP TABLE IF EXISTS gb_ptz_cruise_track;
DROP TABLE IF EXISTS gb_ptz_preset;
DROP TABLE IF EXISTS gb_ptz_state;
DROP TABLE IF EXISTS gb_ptz_operation;

CREATE TABLE gb_ptz_operation (
    id BIGSERIAL,
    operation_id VARCHAR(64) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    device_id BIGINT NOT NULL,
    device_code VARCHAR(20) NOT NULL,
    channel_id BIGINT NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    cmd_type VARCHAR(64) NOT NULL,
    action VARCHAR(64),
    payload_json TEXT,
    sn INTEGER NOT NULL,
    call_id VARCHAR(255),
    cseq VARCHAR(64),
    sip_status INTEGER NOT NULL DEFAULT 0,
    device_result VARCHAR(32),
    device_error TEXT,
    status VARCHAR(16) NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    response_required BOOLEAN NOT NULL DEFAULT FALSE,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    error_code VARCHAR(64),
    error_message TEXT,
    actor_id BIGINT NOT NULL DEFAULT 0,
    actor_dept_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP(3) NOT NULL,
    sent_at TIMESTAMP(3),
    completed_at TIMESTAMP(3),
    queue_deadline_at TIMESTAMP(3),
    dispatch_started_at TIMESTAMP(3),
    transport_deadline_at TIMESTAMP(3),
    deadline_at TIMESTAMP(3),
    next_attempt_at TIMESTAMP(3),
    response_call_id VARCHAR(255),
    response_cseq VARCHAR(64),
    response_at TIMESTAMP(3),
    response_has_data BOOLEAN,
    trigger_operation_id VARCHAR(64),
    reconcile_operation_id VARCHAR(64),
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_operation_id UNIQUE (operation_id),
    CONSTRAINT uk_ptz_operation_idempotency UNIQUE (channel_id, idempotency_key)
);

CREATE TABLE gb_ptz_state (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    device_code VARCHAR(20) NOT NULL,
    channel_id BIGINT NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    pan NUMERIC(18,6),
    tilt NUMERIC(18,6),
    zoom NUMERIC(18,6),
    focus NUMERIC(18,6),
    iris NUMERIC(18,6),
    device_time TIMESTAMP(3),
    received_at TIMESTAMP(3) NOT NULL,
    source_sn INTEGER NOT NULL DEFAULT 0,
    freshness VARCHAR(16) NOT NULL DEFAULT 'unknown',
    dedupe_key VARCHAR(128),
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_state_channel UNIQUE (channel_id)
);

CREATE TABLE gb_ptz_preset (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    preset_id INTEGER NOT NULL,
    name VARCHAR(255),
    status VARCHAR(16) NOT NULL DEFAULT 'unknown',
    last_operation_id VARCHAR(64),
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_preset_channel_number UNIQUE (channel_id, preset_id)
);

CREATE TABLE gb_ptz_cruise_track (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    track_id INTEGER NOT NULL,
    name VARCHAR(255),
    enabled BOOLEAN,
    detail_json TEXT,
    last_operation_id VARCHAR(64),
    raw_summary TEXT,
    device_time TIMESTAMP(3),
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_cruise_channel_track UNIQUE (channel_id, track_id)
);

CREATE TABLE gb_ptz_operation_attempt (
    id BIGSERIAL,
    operation_id BIGINT NOT NULL,
    attempt_no INTEGER NOT NULL,
    sn INTEGER NOT NULL,
    status VARCHAR(16) NOT NULL,
    call_id VARCHAR(255),
    cseq VARCHAR(64),
    sip_status INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMP(3) NOT NULL,
    lease_until TIMESTAMP(3) NOT NULL,
    sent_at TIMESTAMP(3),
    completed_at TIMESTAMP(3),
    error_code VARCHAR(64),
    error_message TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_operation_attempt UNIQUE (operation_id, attempt_no)
);

CREATE TABLE gb_ptz_home_position (
    id BIGSERIAL,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    enabled BOOLEAN NOT NULL,
    reset_time INTEGER,
    preset_id INTEGER,
    enabled_encoding VARCHAR(32) NOT NULL DEFAULT 'numeric',
    confirmed_at TIMESTAMP(3) NOT NULL,
    source VARCHAR(32) NOT NULL,
    verification VARCHAR(16) NOT NULL,
    source_sn INTEGER NOT NULL DEFAULT 0,
    source_operation_id VARCHAR(64),
    source_operation_seq BIGINT NOT NULL DEFAULT 0,
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT uk_ptz_home_position_channel UNIQUE (channel_id)
);

CREATE INDEX idx_ptz_operation_channel_time ON gb_ptz_operation (channel_id, created_at);
CREATE INDEX idx_ptz_operation_channel_cmd_id ON gb_ptz_operation (channel_id, cmd_type, id);
CREATE INDEX idx_ptz_operation_device_sn ON gb_ptz_operation (device_id, sn);
CREATE INDEX idx_ptz_operation_status_time ON gb_ptz_operation (status, created_at);
CREATE INDEX idx_ptz_operation_status_next_attempt ON gb_ptz_operation (status, next_attempt_at);
CREATE INDEX idx_ptz_operation_status_queue_deadline ON gb_ptz_operation (status, queue_deadline_at);
CREATE INDEX idx_ptz_operation_status_transport_deadline ON gb_ptz_operation (status, transport_deadline_at);
CREATE INDEX idx_ptz_operation_status_deadline ON gb_ptz_operation (status, deadline_at);
CREATE INDEX idx_ptz_operation_call_id ON gb_ptz_operation (call_id);
CREATE INDEX idx_ptz_state_device ON gb_ptz_state (device_id);
CREATE INDEX idx_ptz_state_received ON gb_ptz_state (received_at);
CREATE INDEX idx_ptz_preset_device ON gb_ptz_preset (device_id);
CREATE INDEX idx_ptz_cruise_device ON gb_ptz_cruise_track (device_id);
CREATE INDEX idx_ptz_attempt_status_lease ON gb_ptz_operation_attempt (status, lease_until);
CREATE INDEX idx_ptz_home_position_device ON gb_ptz_home_position (device_id);

ALTER TABLE gb_ptz_operation
    ADD COLUMN profile_version VARCHAR(8),
    ADD COLUMN profile_charset VARCHAR(16),
    ADD COLUMN target_scope VARCHAR(16),
    ADD COLUMN target_code VARCHAR(20),
    ADD COLUMN scope_key VARCHAR(64);
CREATE TABLE gb_device_control_state (
    id BIGSERIAL PRIMARY KEY,
    device_id BIGINT NOT NULL,
    channel_id BIGINT NOT NULL DEFAULT 0,
    target_scope VARCHAR(16) NOT NULL,
    target_code VARCHAR(20) NOT NULL,
    record_state VARCHAR(8) NOT NULL DEFAULT 'unknown',
    guard_state VARCHAR(8) NOT NULL DEFAULT 'unknown',
    freshness VARCHAR(8) NOT NULL DEFAULT 'unknown',
    observed_at TIMESTAMP(3) NOT NULL,
    source VARCHAR(32) NOT NULL DEFAULT 'device_status',
    source_sn INTEGER NOT NULL DEFAULT 0,
    source_operation_id VARCHAR(64),
    source_operation_seq BIGINT NOT NULL DEFAULT 0,
    raw_summary TEXT,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_control_state_target UNIQUE (device_id, target_scope, target_code)
);
CREATE INDEX idx_control_state_device_target ON gb_device_control_state (device_id, target_scope, target_code);
CREATE INDEX idx_control_state_channel ON gb_device_control_state (channel_id);
CREATE INDEX idx_ptz_operation_target ON gb_ptz_operation (device_code, target_scope, target_code, status);
CREATE INDEX idx_ptz_operation_device_scope_time ON gb_ptz_operation (device_id, scope_key, created_at);

CREATE TABLE gb_alarm_resource (
    id BIGSERIAL PRIMARY KEY,
    owner_dept_id BIGINT NOT NULL,
    device_id BIGINT NOT NULL DEFAULT 0,
    device_code VARCHAR(20) NOT NULL,
    alarm_code VARCHAR(20) NOT NULL,
    resource_type VARCHAR(16) NOT NULL,
    type_code VARCHAR(3) NOT NULL,
    name VARCHAR(255) NOT NULL,
    raw_parent_ids VARCHAR(512) NOT NULL DEFAULT '',
    status SMALLINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    deleted_at TIMESTAMP(3),
    CONSTRAINT uk_alarm_resource_code UNIQUE (owner_dept_id, device_code, alarm_code)
);

CREATE TABLE gb_alarm_resource_parent (
    id BIGSERIAL PRIMARY KEY,
    alarm_resource_id BIGINT NOT NULL,
    parent_code VARCHAR(20) NOT NULL,
    created_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_alarm_resource_parent UNIQUE (alarm_resource_id, parent_code)
);

CREATE TABLE gb_alarm_binding (
    id BIGSERIAL PRIMARY KEY,
    device_id BIGINT NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    alarm_resource_id BIGINT NOT NULL,
    source VARCHAR(16) NOT NULL DEFAULT 'manual',
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_alarm_binding_channel UNIQUE (device_id, channel_code)
);

CREATE INDEX idx_alarm_resource_device ON gb_alarm_resource (owner_dept_id, device_code);
CREATE INDEX idx_alarm_resource_device_id ON gb_alarm_resource (device_id);
CREATE INDEX idx_alarm_resource_alarm_code ON gb_alarm_resource (alarm_code);
CREATE INDEX idx_alarm_resource_type ON gb_alarm_resource (resource_type);
CREATE INDEX idx_alarm_resource_deleted_at ON gb_alarm_resource (deleted_at);
CREATE INDEX idx_alarm_parent_resource ON gb_alarm_resource_parent (alarm_resource_id);
CREATE INDEX idx_alarm_parent_code ON gb_alarm_resource_parent (parent_code);
CREATE INDEX idx_alarm_binding_device ON gb_alarm_binding (device_id);
CREATE INDEX idx_alarm_binding_resource ON gb_alarm_binding (alarm_resource_id);

CREATE TABLE gb_playback_scheme (
    id BIGSERIAL PRIMARY KEY,
    owner_user_id BIGINT NOT NULL,
    owner_dept_id BIGINT NOT NULL,
    name VARCHAR(64) NOT NULL,
    layout_size SMALLINT NOT NULL,
    slot_count INTEGER NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL,
    updated_by BIGINT NOT NULL,
    created_at TIMESTAMP(3) NOT NULL,
    updated_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_playback_scheme_owner_name UNIQUE (owner_user_id, name)
);
CREATE INDEX idx_playback_scheme_owner_updated ON gb_playback_scheme (owner_user_id, updated_at);
CREATE INDEX idx_playback_scheme_dept ON gb_playback_scheme (owner_dept_id);

CREATE TABLE gb_playback_scheme_slot (
    id BIGSERIAL PRIMARY KEY,
    scheme_id BIGINT NOT NULL,
    slot_index INTEGER NOT NULL,
    device_code VARCHAR(20) NOT NULL,
    channel_code VARCHAR(20) NOT NULL,
    device_name_snapshot VARCHAR(255) NOT NULL,
    channel_name_snapshot VARCHAR(255) NOT NULL,
    created_at TIMESTAMP(3) NOT NULL,
    CONSTRAINT uk_playback_scheme_slot UNIQUE (scheme_id, slot_index)
);
CREATE INDEX idx_playback_scheme_slot_scheme ON gb_playback_scheme_slot (scheme_id);

CREATE TABLE gb_sip_trace_capture (
    id CHAR(36) PRIMARY KEY, device_id BIGINT NOT NULL, device_code VARCHAR(20) NOT NULL, created_by BIGINT NOT NULL,
    started_at TIMESTAMP(3) WITH TIME ZONE NOT NULL, planned_end_at TIMESTAMP(3) WITH TIME ZONE NOT NULL,
    ended_at TIMESTAMP(3) WITH TIME ZONE NULL, end_reason VARCHAR(16) NOT NULL DEFAULT '', active_key VARCHAR(64) NULL,
    created_at TIMESTAMP(3) WITH TIME ZONE NOT NULL, updated_at TIMESTAMP(3) WITH TIME ZONE NOT NULL
);
CREATE UNIQUE INDEX uk_sip_trace_capture_active ON gb_sip_trace_capture (active_key);
CREATE INDEX idx_sip_trace_capture_device_started ON gb_sip_trace_capture (device_id, started_at);
CREATE INDEX idx_sip_trace_capture_device_code ON gb_sip_trace_capture (device_code);
CREATE INDEX idx_sip_trace_capture_created_by ON gb_sip_trace_capture (created_by);
CREATE INDEX idx_sip_trace_capture_planned_end ON gb_sip_trace_capture (planned_end_at);

CREATE TABLE gb_sip_trace_message (
    event_id VARCHAR(36) PRIMARY KEY, occurred_at TIMESTAMP(6) WITH TIME ZONE NOT NULL, direction VARCHAR(16) NOT NULL,
    transport VARCHAR(16) NOT NULL, local_addr VARCHAR(255) NOT NULL, remote_addr VARCHAR(255) NOT NULL,
    device_id VARCHAR(64) NOT NULL, method VARCHAR(32) NOT NULL, status_code SMALLINT NOT NULL,
    call_id VARCHAR(255) NOT NULL, cseq INTEGER NOT NULL, cseq_method VARCHAR(32) NOT NULL,
    from_uri VARCHAR(512) NOT NULL, to_uri VARCHAR(512) NOT NULL,
    from_id VARCHAR(64) NOT NULL DEFAULT '', to_id VARCHAR(64) NOT NULL DEFAULT '',
    business_code VARCHAR(64) NOT NULL DEFAULT 'unknown', business_type VARCHAR(64) NOT NULL DEFAULT '未知业务',
    business_confidence VARCHAR(16) NOT NULL DEFAULT 'none', user_agent VARCHAR(512) NOT NULL,
    malformed BOOLEAN NOT NULL DEFAULT FALSE, parse_error VARCHAR(1024) NOT NULL,
    payload_nonce BYTEA NOT NULL, payload_ciphertext BYTEA NOT NULL, payload_algorithm VARCHAR(32) NOT NULL,
    payload_key_version VARCHAR(64) NOT NULL, payload_digest_sha256 CHAR(64) NOT NULL
);
CREATE INDEX idx_gb_sip_trace_occurred_event ON gb_sip_trace_message (occurred_at, event_id);
CREATE INDEX idx_gb_sip_trace_device_occurred ON gb_sip_trace_message (device_id, occurred_at, event_id);
CREATE INDEX idx_gb_sip_trace_call_occurred ON gb_sip_trace_message (call_id, occurred_at, event_id);
CREATE INDEX idx_gb_sip_trace_business_occurred ON gb_sip_trace_message (business_code, occurred_at);

CREATE TABLE gb_sip_trace_session_diagnosis (
    id BIGSERIAL PRIMARY KEY,
    session_day DATE NOT NULL,
    observed_at TIMESTAMP(6) WITH TIME ZONE NOT NULL,
    correlation_key VARCHAR(128) NOT NULL,
    state VARCHAR(16) NOT NULL DEFAULT 'active',
    category VARCHAR(32) NOT NULL,
    code VARCHAR(64) NOT NULL,
    stage VARCHAR(32) NOT NULL,
    source VARCHAR(32) NOT NULL,
    device_id VARCHAR(64) NOT NULL DEFAULT '',
    channel_id VARCHAR(64) NOT NULL DEFAULT '',
    call_id VARCHAR(255) NOT NULL DEFAULT '',
    cseq INTEGER NOT NULL DEFAULT 0,
    method VARCHAR(32) NOT NULL DEFAULT '',
    status_code SMALLINT NOT NULL DEFAULT 0,
    stream_id VARCHAR(255) NOT NULL DEFAULT '',
    resolved_at TIMESTAMP(6) WITH TIME ZONE NULL,
    CONSTRAINT uk_sip_trace_diagnosis_session UNIQUE (session_day, category, correlation_key)
);
CREATE INDEX idx_sip_trace_diagnosis_category_state_observed
    ON gb_sip_trace_session_diagnosis (session_day, category, state, observed_at);
CREATE INDEX idx_sip_trace_diagnosis_device_observed
    ON gb_sip_trace_session_diagnosis (device_id, observed_at);
CREATE INDEX idx_sip_trace_diagnosis_call_cseq
    ON gb_sip_trace_session_diagnosis (call_id, cseq);

-- 在线用户会话与权限 seed。
CREATE TABLE sys_user_sessions (
    sid VARCHAR(36) PRIMARY KEY,
    user_id BIGINT NOT NULL,
    refresh_token_hash CHAR(64) NULL,
    refresh_jti VARCHAR(36) NULL,
    client_ip VARCHAR(50) NOT NULL DEFAULT '',
    login_location VARCHAR(100) NOT NULL DEFAULT '未知',
    user_agent VARCHAR(500) NOT NULL DEFAULT '',
    browser VARCHAR(100) NOT NULL DEFAULT '未知',
    os VARCHAR(100) NOT NULL DEFAULT '未知',
    login_at TIMESTAMP NOT NULL,
    last_active_at TIMESTAMP NOT NULL,
    session_expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP NULL,
    revoke_reason VARCHAR(32) NULL,
    revoked_by BIGINT NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX idx_user_id ON sys_user_sessions (user_id);
CREATE INDEX idx_session_valid ON sys_user_sessions (revoked_at,session_expires_at,login_at);
CREATE INDEX idx_client_ip ON sys_user_sessions (client_ip);

INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(339,'查询在线用户','/api/sysOnlineUser/list','GET','系统管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),
(340,'强制下线会话','/api/sysOnlineUser/forceLogout','POST','系统管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) VALUES
(140382,10,'/system/online-user','SystemOnlineUser','system/online-user/index','在线用户',FALSE,FALSE,8,2,'system:online-user:list','lucide:UsersRound',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),
(140383,140382,'','SystemOnlineUserForceLogout','','强制下线',TRUE,FALSE,1,3,'system:online-user:force-logout','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_role_menu (role_id,menu_id) VALUES (1,140382),(1,140383);
INSERT INTO sys_menu_api (menu_id,api_id) VALUES (140382,339),(140383,340);
INSERT INTO sys_casbin_rule (id,ptype,v0,v1,v2,v3,v4,v5) VALUES
(7806,'p','role_1','/api/sysOnlineUser/list','GET','*','',''),
(7807,'p','role_1','/api/sysOnlineUser/forceLogout','POST','*','','');
SELECT setval('sys_api_id_seq',340,true);
SELECT setval('sys_menu_id_seq',140383,true);
SELECT setval('sys_casbin_rule_id_seq',7807,true);

-- 登录审计事件与操作菜单 seed。
CREATE TABLE IF NOT EXISTS sys_login_logs (
  id BIGSERIAL PRIMARY KEY, user_id BIGINT NULL, username VARCHAR(100) NOT NULL, result VARCHAR(16) NOT NULL,
  failure_reason VARCHAR(48) NULL, ip VARCHAR(50) NOT NULL DEFAULT '', location VARCHAR(100) NOT NULL DEFAULT '未知',
  user_agent VARCHAR(500) NOT NULL DEFAULT '', browser VARCHAR(100) NOT NULL DEFAULT '未知', os VARCHAR(100) NOT NULL DEFAULT '未知',
  created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NULL, deleted_at TIMESTAMP NULL
);
CREATE INDEX idx_login_logs_user_id ON sys_login_logs(user_id); CREATE INDEX idx_login_logs_username ON sys_login_logs(username);
CREATE INDEX idx_login_logs_result ON sys_login_logs(result); CREATE INDEX idx_login_logs_failure_reason ON sys_login_logs(failure_reason);
CREATE INDEX idx_login_logs_ip ON sys_login_logs(ip); CREATE INDEX idx_login_logs_created_at ON sys_login_logs(created_at);
INSERT INTO sys_api (id,title,path,method,api_group,created_at,updated_at,deleted_at,created_by) VALUES
(341,'登录日志列表','/api/sysLoginLog/list','GET','日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(342,'登录日志详情','/api/sysLoginLog/:id','GET','日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(343,'删除登录日志','/api/sysLoginLog/delete','DELETE','日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(344,'清空登录日志','/api/sysLoginLog/clear','POST','日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1),(345,'解锁登录账号','/api/sysLoginLog/unlock','POST','日志管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,NULL,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) VALUES
(140384,10,'/system/login-log','SystemLoginLog','system/login-log/index','登录日志',FALSE,FALSE,1,2,'system:login-log:list','lucide:FileClock',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_menu (id,parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) VALUES (140385,140384,'','SystemLoginLogDelete','','删除登录日志',TRUE,FALSE,1,3,'system:login-log:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140386,140384,'','SystemLoginLogClear','','清空登录日志',TRUE,FALSE,2,3,'system:login-log:clear','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(140387,140384,'','SystemLoginLogUnlock','','解锁登录账号',TRUE,FALSE,3,3,'system:login-log:unlock','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1);
INSERT INTO sys_role_menu(role_id,menu_id) VALUES (1,140384),(1,140385),(1,140386),(1,140387); INSERT INTO sys_menu_api(menu_id,api_id) VALUES (140384,341),(140384,342),(140385,343),(140386,344),(140387,345);
INSERT INTO sys_casbin_rule(id,ptype,v0,v1,v2,v3,v4,v5) VALUES (7808,'p','role_1','/api/sysLoginLog/list','GET','*','',''),(7809,'p','role_1','/api/sysLoginLog/:id','GET','*','',''),(7810,'p','role_1','/api/sysLoginLog/delete','DELETE','*','',''),(7811,'p','role_1','/api/sysLoginLog/clear','POST','*','',''),(7812,'p','role_1','/api/sysLoginLog/unlock','POST','*','','');
SELECT setval('sys_api_id_seq',345,true); SELECT setval('sys_menu_id_seq',140387,true); SELECT setval('sys_casbin_rule_id_seq',7812,true);

-- Device permission workbench baseline seed (PostgreSQL).

-- 设备分配菜单 + 按钮权限 + 角色绑定(PostgreSQL,幂等)
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,is_full,hide,disable,keep_alive,affix,is_link,link,iframe,svg_icon,icon,sort,type,permission,created_by,created_at,updated_at)
SELECT 0,'/gb28181/device-assignment','device-assignment','','gb28181/device-assignment/index','设备分配',0,0,0,0,0,0,'',0,'','lucide:KeyRound',9,2,'',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE name='device-assignment' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,is_full,hide,disable,keep_alive,affix,is_link,link,iframe,svg_icon,icon,sort,type,permission,created_by,created_at,updated_at)
SELECT m.id,'','device-assignment-assign','','','分配设备归属',0,0,0,0,0,0,'',0,'','',1,3,'gb28181:device:assign',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
FROM sys_menu m WHERE m.name='device-assignment' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu x WHERE x.name='device-assignment-assign' AND x.deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,is_full,hide,disable,keep_alive,affix,is_link,link,iframe,svg_icon,icon,sort,type,permission,created_by,created_at,updated_at)
SELECT m.id,'','device-assignment-share','','','共享设备',0,0,0,0,0,0,'',0,'','',2,3,'gb28181:device:share',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP
FROM sys_menu m WHERE m.name='device-assignment' AND m.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu x WHERE x.name='device-assignment-share' AND x.deleted_at IS NULL);

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT rm.role_id, m.id
FROM sys_role_menu rm
JOIN sys_menu src ON src.id=rm.menu_id AND src.name='device-mgmt-list' AND src.deleted_at IS NULL
JOIN sys_menu m ON m.name IN ('device-assignment','device-assignment-assign','device-assignment-share') AND m.deleted_at IS NULL
WHERE NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=rm.role_id AND x.menu_id=m.id);

-- 设备权限工作台 API 权限迁移(PostgreSQL,幂等)。

UPDATE sys_menu SET title='设备权限工作台', updated_at=CURRENT_TIMESTAMP
WHERE name='device-assignment' AND deleted_at IS NULL;

UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND path IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM sys_menu_api ma USING sys_api a
WHERE a.id=ma.api_id AND a.path IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');
DELETE FROM sys_casbin_rule WHERE v1 IN ('/api/gb28181/device-mgmt/assign','/api/gb28181/device-mgmt/assign-dept','/api/gb28181/device-mgmt/device/:id/grants','/api/gb28181/device-mgmt/device/:id/grants/:grantId');

UPDATE sys_api SET deleted_at=NULL, title='查询设备权限汇总', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/summary' AND method='GET';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询设备权限汇总','/api/gb28181/device-mgmt/permission-workbench/summary','GET','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/summary' AND method='GET' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='解析工作台设备', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '解析工作台设备','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='查询设备共享授权', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询设备共享授权','/api/gb28181/device-mgmt/permission-workbench/grants/query','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='查询共享目标', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND method='GET';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '查询共享目标','/api/gb28181/device-mgmt/permission-workbench/grant-targets','GET','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND method='GET' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='调整设备归属', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '调整设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='整部门调整设备归属', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '整部门调整设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND method='POST' AND deleted_at IS NULL);
UPDATE sys_api SET deleted_at=NULL, title='应用设备共享授权', api_group='GB28181设备管理', updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND method='POST';
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT '应用设备共享授权','/api/gb28181/device-mgmt/permission-workbench/grants/apply','POST','GB28181设备管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON TRUE
WHERE m.name='device-assignment' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND ((a.path='/api/gb28181/device-mgmt/permission-workbench/summary' AND a.method='GET') OR (a.path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND a.method='POST') OR (a.path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND a.method='POST') OR (a.path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND a.method='GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON TRUE
WHERE m.permission='gb28181:device:assign' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path IN ('/api/gb28181/device-mgmt/permission-workbench/assignments','/api/gb28181/device-mgmt/permission-workbench/assignments/departments') AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON TRUE
WHERE m.permission='gb28181:device:share' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND a.method='POST'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p',CONCAT('role_',rm.role_id),a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.name IN ('device-assignment','device-assignment-assign','device-assignment-share') AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND a.path LIKE '/api/gb28181/device-mgmt/permission-workbench/%'
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0=CONCAT('role_',rm.role_id) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- Cloud recording physical deletion for fresh PostgreSQL installs.
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT m.id,'','','','删除录像文件',3,'gb28181:recording:delete',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu m WHERE m.path='/gb28181/cloud-recordings' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:delete' AND deleted_at IS NULL);
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 云端录像删除',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES ('删除单个云端录像','/api/gb28181/cloud-recordings/files/:id','DELETE'),('批量删除云端录像','/api/gb28181/cloud-recordings/files/batch-delete','POST')) v(title,path,method) WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);
INSERT INTO sys_role_menu (role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:recording:delete' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:delete' AND m.deleted_at IS NULL AND ((a.path='/api/gb28181/cloud-recordings/files/:id' AND a.method='DELETE') OR (a.path='/api/gb28181/cloud-recordings/files/batch-delete' AND a.method='POST')) AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a WHERE ((a.path='/api/gb28181/cloud-recordings/files/:id' AND a.method='DELETE') OR (a.path='/api/gb28181/cloud-recordings/files/batch-delete' AND a.method='POST')) AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- Cloud recording stop control for fresh PostgreSQL installs.
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT m.id,'','','','停止录像',3,'gb28181:recording:stop',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu m WHERE m.path='/gb28181/cloud-recordings' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:stop' AND deleted_at IS NULL);
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT '停止云端录像','/api/gb28181/cloud-recordings/active/:id/stop','POST','GB28181 云端录像控制',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/active/:id/stop' AND method='POST' AND deleted_at IS NULL);
INSERT INTO sys_role_menu (role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:recording:stop' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);
INSERT INTO sys_menu_api (menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:stop' AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/active/:id/stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_1',a.path,a.method,'*','','' FROM sys_api a WHERE a.path='/api/gb28181/cloud-recordings/active/:id/stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- Recording plan menus and API permissions for fresh PostgreSQL installs.
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,icon,sort,created_at,updated_at,created_by)
SELECT 0,'/gb28181/recording-schedules','gb28181-recording-schedules','gb28181/recording-schedules/index','录像计划',2,'gb28181:recording-plan:view','lucide:CalendarClock',34,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording-plan:view' AND deleted_at IS NULL);
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','维护录像计划',3,'gb28181:recording-plan:maintain',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p WHERE p.permission='gb28181:recording-plan:view' AND p.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording-plan:maintain' AND deleted_at IS NULL);
INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','分配录像计划',3,'gb28181:recording-plan:assign',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p WHERE p.permission='gb28181:recording-plan:view' AND p.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording-plan:assign' AND deleted_at IS NULL);
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m WHERE m.permission IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT s.title,s.path,s.method,'GB28181 录像计划',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
 ('查询录像计划','/api/gb28181/recording-plans','GET'),('新建录像计划','/api/gb28181/recording-plans','POST'),('查看录像计划','/api/gb28181/recording-plans/:id','GET'),
 ('编辑录像计划','/api/gb28181/recording-plans/:id','PUT'),('删除录像计划','/api/gb28181/recording-plans/:id','DELETE'),('启停录像计划','/api/gb28181/recording-plans/:id/status','PATCH'),
 ('搜索分配设备','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),('搜索分配通道','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
 ('分配录像计划','/api/gb28181/recording-plans/:id/assignments','POST'),('切换通道录像模式','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
 ('查询计划通道状态','/api/gb28181/recording-plans/:id/channels','GET'),('诊断通道录像','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
 ('查询通道执行时间线','/api/gb28181/recording-plans/channels/:channelId/timeline','GET')
) AS s(title,path,method) WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=s.path AND a.method=s.method AND a.deleted_at IS NULL);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.method='GET' AND a.path IN ('/api/gb28181/recording-plans','/api/gb28181/recording-plans/:id','/api/gb28181/recording-plans/:id/channels','/api/gb28181/recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/timeline') AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND ((a.path='/api/gb28181/recording-plans' AND a.method='POST') OR (a.path='/api/gb28181/recording-plans/:id' AND a.method IN ('PUT','DELETE')) OR (a.path='/api/gb28181/recording-plans/:id/status' AND a.method='PATCH')) AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path IN ('/api/gb28181/recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignments','/api/gb28181/recording-plans/channels/:channelId/recording-mode') AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||rm.role_id,a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission IN ('gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign') AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- media-management-baseline:start
-- Media management menus and exact backend permission bindings (postgresql, idempotent).
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,keep_alive,created_at,updated_at,created_by)
SELECT 0,'/media','Media','/gb28181/zlm/overview','','流媒体管理','lucide:Clapperboard',9,1,'',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media' AND deleted_at IS NULL);
UPDATE sys_menu SET redirect='/gb28181/zlm/overview',title='流媒体管理',icon='lucide:Clapperboard',sort=9,updated_at=CURRENT_TIMESTAMP
WHERE path='/media' AND deleted_at IS NULL;

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='集群概览',icon='lucide:LayoutDashboard',sort=10,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/overview','gb28181-zlm-overview','','gb28181/zlm/ClusterOverview','集群概览','lucide:LayoutDashboard',10,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='节点管理',icon='lucide:Server',sort=11,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/nodes' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/nodes','gb28181-zlm-nodes','','gb28181/zlm/NodeList','节点管理','lucide:Server',11,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/nodes' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='调度策略',icon='lucide:Workflow',sort=12,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/scheduler','gb28181-zlm-scheduler-strategy','','gb28181/zlm/SchedulerStrategy','调度策略','lucide:Workflow',12,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/scheduler' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='调度日志',icon='lucide:History',sort=13,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler/logs' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/scheduler/logs','gb28181-zlm-scheduler-log','','gb28181/zlm/SchedulerLog','调度日志','lucide:History',13,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/scheduler/logs' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='运行监控',icon='lucide:Activity',sort=20,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/runtime','gb28181-zlm-runtime','','gb28181/zlm/RuntimeOverview','运行监控','lucide:Activity',20,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='流媒体',icon='lucide:RadioTower',sort=21,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/streams' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/streams','gb28181-zlm-streams','','gb28181/zlm/StreamManagement','流媒体','lucide:RadioTower',21,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/streams' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='会话管理',icon='lucide:Users',sort=22,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/sessions' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/sessions','gb28181-zlm-sessions','','gb28181/zlm/SessionManagement','会话管理','lucide:Users',22,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/sessions' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='拉流代理',icon='lucide:Network',sort=30,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/proxies' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/proxies','gb28181-zlm-proxies','','gb28181/zlm/ProxyManagement','拉流代理','lucide:Network',30,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/proxies' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='FFmpeg 源',icon='lucide:Clapperboard',sort=31,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/ffmpeg-sources' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/ffmpeg-sources','gb28181-zlm-ffmpeg-sources','','gb28181/zlm/FFmpegSources','FFmpeg 源','lucide:Clapperboard',31,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/ffmpeg-sources' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='RTP 服务',icon='lucide:Waypoints',sort=32,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/rtp-servers' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/rtp-servers','gb28181-zlm-rtp-servers','','gb28181/zlm/RTPServices','RTP 服务','lucide:Waypoints',32,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/rtp-servers' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='录制管理',icon='lucide:Cloud',sort=40,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/cloud-recordings','gb28181-cloud-recordings','','gb28181/cloud-recordings/index','录制管理','lucide:Cloud',40,2,'gb28181:recording:view',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='录像计划',icon='lucide:CalendarClock',sort=41,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/recording-schedules' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/recording-schedules','gb28181-recording-schedules','','gb28181/recording-schedules/index','录像计划','lucide:CalendarClock',41,2,'gb28181:recording-plan:view',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/recording-schedules' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),redirect='',title='服务配置',icon='lucide:Settings2',sort=42,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/config' AND deleted_at IS NULL;
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,created_at,updated_at,created_by)
SELECT (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),'/gb28181/zlm/config','gb28181-zlm-config','','gb28181/zlm/ServerConfig','服务配置','lucide:Settings2',42,2,'',0,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE (SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/zlm/config' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理节点',3,'gb28181:zlm:node:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/nodes' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','踢除节点会话',3,'gb28181:zlm:node:kick',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/nodes' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:kick' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','切换调度策略',3,'gb28181:zlm:scheduler:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/scheduler' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:scheduler:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','预览与截图',3,'gb28181:zlm:stream:preview',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/streams' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:preview' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','关闭流',3,'gb28181:zlm:stream:close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/streams' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:close' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','强制关闭流',3,'gb28181:zlm:stream:force-close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/streams' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:force-close' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','踢除会话',3,'gb28181:zlm:session:kick',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/sessions' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:session:kick' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理代理',3,'gb28181:zlm:proxy:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/proxies' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:proxy:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理 FFmpeg 源',3,'gb28181:zlm:ffmpeg:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/ffmpeg-sources' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:ffmpeg:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','管理 RTP 服务',3,'gb28181:zlm:rtp:manage',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/rtp-servers' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:manage' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','强制关闭 RTP 服务',3,'gb28181:zlm:rtp:force-close',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/rtp-servers' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:force-close' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','手工录制控制',3,'gb28181:recording:control',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/cloud-recordings' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:control' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','强制停止录制',3,'gb28181:recording:force-stop',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/cloud-recordings' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:force-stop' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','更新服务配置',3,'gb28181:zlm:config:update',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/config' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:config:update' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,type,permission,hide,created_at,updated_at,created_by)
SELECT p.id,'','','','重启媒体服务',3,'gb28181:zlm:restart',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/gb28181/zlm/config' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:restart' AND deleted_at IS NULL);

INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.deleted_at IS NULL AND (m.path IN ('/media','/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config')
OR m.permission IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT v.title,v.path,v.method,'GB28181 媒体管理',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (VALUES
  ('媒体管理 GET zlm/overview','/api/gb28181/zlm/overview','GET'),
  ('媒体管理 GET zlm/nodes','/api/gb28181/zlm/nodes','GET'),
  ('媒体管理 GET zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','GET'),
  ('媒体管理 GET zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','GET'),
  ('媒体管理 GET zlm/scheduler','/api/gb28181/zlm/scheduler','GET'),
  ('媒体管理 GET zlm/scheduler/logs','/api/gb28181/zlm/scheduler/logs','GET'),
  ('媒体管理 GET zlm/nodes/:id/runtime','/api/gb28181/zlm/nodes/:id/runtime','GET'),
  ('媒体管理 GET zlm/streams','/api/gb28181/zlm/streams','GET'),
  ('媒体管理 GET zlm/nodes/:id/streams','/api/gb28181/zlm/nodes/:id/streams','GET'),
  ('媒体管理 GET zlm/nodes/:id/streams/detail','/api/gb28181/zlm/nodes/:id/streams/detail','GET'),
  ('媒体管理 GET zlm/nodes/:id/streams/viewers','/api/gb28181/zlm/nodes/:id/streams/viewers','GET'),
  ('媒体管理 GET zlm/nodes/:id/sessions/network','/api/gb28181/zlm/nodes/:id/sessions/network','GET'),
  ('媒体管理 GET zlm/nodes/:id/sessions/viewers','/api/gb28181/zlm/nodes/:id/sessions/viewers','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','GET'),
  ('媒体管理 GET zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','GET'),
  ('媒体管理 GET zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','GET'),
  ('媒体管理 GET zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','GET'),
  ('媒体管理 GET cloud-recordings/files','/api/gb28181/cloud-recordings/files','GET'),
  ('媒体管理 GET cloud-recordings/files/options','/api/gb28181/cloud-recordings/files/options','GET'),
  ('媒体管理 GET cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','GET'),
  ('媒体管理 POST cloud-recordings/files/:id/access','/api/gb28181/cloud-recordings/files/:id/access','POST'),
  ('媒体管理 POST cloud-recordings/files/:id/downloads','/api/gb28181/cloud-recordings/files/:id/downloads','POST'),
  ('媒体管理 GET cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','GET'),
  ('媒体管理 DELETE cloud-recordings/downloads/:taskId','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE'),
  ('媒体管理 GET cloud-recordings/active','/api/gb28181/cloud-recordings/active','GET'),
  ('媒体管理 GET zlm/nodes/:id/recordings/runtime/status','/api/gb28181/zlm/nodes/:id/recordings/runtime/status','GET'),
  ('媒体管理 GET cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','GET'),
  ('媒体管理 POST cloud-recordings/reconciliations','/api/gb28181/cloud-recordings/reconciliations','POST'),
  ('媒体管理 POST cloud-recordings/files/batch-delete','/api/gb28181/cloud-recordings/files/batch-delete','POST'),
  ('媒体管理 DELETE cloud-recordings/files/:id','/api/gb28181/cloud-recordings/files/:id','DELETE'),
  ('媒体管理 POST cloud-recordings/active/:id/stop','/api/gb28181/cloud-recordings/active/:id/stop','POST'),
  ('媒体管理 GET recording-plans','/api/gb28181/recording-plans','GET'),
  ('媒体管理 GET recording-plans/:id','/api/gb28181/recording-plans/:id','GET'),
  ('媒体管理 GET recording-plans/:id/channels','/api/gb28181/recording-plans/:id/channels','GET'),
  ('媒体管理 GET recording-plans/channels/:channelId/diagnosis','/api/gb28181/recording-plans/channels/:channelId/diagnosis','GET'),
  ('媒体管理 GET recording-plans/channels/:channelId/timeline','/api/gb28181/recording-plans/channels/:channelId/timeline','GET'),
  ('媒体管理 POST recording-plans','/api/gb28181/recording-plans','POST'),
  ('媒体管理 PUT recording-plans/:id','/api/gb28181/recording-plans/:id','PUT'),
  ('媒体管理 DELETE recording-plans/:id','/api/gb28181/recording-plans/:id','DELETE'),
  ('媒体管理 PATCH recording-plans/:id/status','/api/gb28181/recording-plans/:id/status','PATCH'),
  ('媒体管理 PATCH recording-plans/channels/:channelId/recording-mode','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH'),
  ('媒体管理 GET recording-plans/:id/assignment-options/devices','/api/gb28181/recording-plans/:id/assignment-options/devices','GET'),
  ('媒体管理 GET recording-plans/:id/assignment-options/channels','/api/gb28181/recording-plans/:id/assignment-options/channels','GET'),
  ('媒体管理 POST recording-plans/:id/assignments','/api/gb28181/recording-plans/:id/assignments','POST'),
  ('媒体管理 POST zlm/nodes','/api/gb28181/zlm/nodes','POST'),
  ('媒体管理 PUT zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','PUT'),
  ('媒体管理 DELETE zlm/nodes/:id','/api/gb28181/zlm/nodes/:id','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/maintenance','/api/gb28181/zlm/nodes/:id/maintenance','POST'),
  ('媒体管理 POST zlm/nodes/:id/activate','/api/gb28181/zlm/nodes/:id/activate','POST'),
  ('媒体管理 POST zlm/nodes/:id/kick','/api/gb28181/zlm/nodes/:id/kick','POST'),
  ('媒体管理 PUT zlm/scheduler','/api/gb28181/zlm/scheduler','PUT'),
  ('媒体管理 POST zlm/nodes/:id/streams/playback-grant','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST'),
  ('媒体管理 GET zlm/nodes/:id/streams/snapshot','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET'),
  ('媒体管理 POST zlm/nodes/:id/streams/close/preflight','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/close','/api/gb28181/zlm/nodes/:id/streams/close','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/close/batch/preflight','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/close/batch','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST'),
  ('媒体管理 POST zlm/nodes/:id/streams/force-close','/api/gb28181/zlm/nodes/:id/streams/force-close','POST'),
  ('媒体管理 POST zlm/nodes/:id/sessions/kick','/api/gb28181/zlm/nodes/:id/sessions/kick','POST'),
  ('媒体管理 POST zlm/nodes/:id/proxies/pull','/api/gb28181/zlm/nodes/:id/proxies/pull','POST'),
  ('媒体管理 POST zlm/nodes/:id/proxies/pull/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST'),
  ('媒体管理 DELETE zlm/nodes/:id/proxies/pull/:key','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/proxies/push','/api/gb28181/zlm/nodes/:id/proxies/push','POST'),
  ('媒体管理 POST zlm/nodes/:id/proxies/push/:key/preflight','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST'),
  ('媒体管理 DELETE zlm/nodes/:id/proxies/push/:key','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/ffmpeg-sources','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST'),
  ('媒体管理 POST zlm/nodes/:id/ffmpeg-sources/:key/preflight','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST'),
  ('媒体管理 DELETE zlm/nodes/:id/ffmpeg-sources/:key','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers','/api/gb28181/zlm/nodes/:id/rtp-servers','POST'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers/close/preflight','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers/close','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST'),
  ('媒体管理 POST zlm/nodes/:id/rtp-servers/force-close','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/start/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/start','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop/preflight','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST'),
  ('媒体管理 POST zlm/nodes/:id/recordings/runtime/force-stop','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST'),
  ('媒体管理 PUT zlm/nodes/:id/config','/api/gb28181/zlm/nodes/:id/config','PUT'),
  ('媒体管理 POST zlm/nodes/:id/config/test-connection','/api/gb28181/zlm/nodes/:id/config/test-connection','POST'),
  ('媒体管理 GET zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','GET'),
  ('媒体管理 POST zlm/nodes/:id/restart','/api/gb28181/zlm/nodes/:id/restart','POST')
) AS v(title,path,method)
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=v.path AND a.method=v.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT DISTINCT m.id,a.id FROM (VALUES
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
) AS b(selector_type,selector,api_path,method)
JOIN sys_menu m ON ((b.selector_type='path' AND m.path=b.selector) OR (b.selector_type='permission' AND m.permission=b.selector)) AND m.deleted_at IS NULL
JOIN sys_api a ON a.path=b.api_path AND a.method=b.method AND a.deleted_at IS NULL
WHERE NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||CAST(rm.role_id AS varchar(20)),a.path,a.method,'*','','' FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.deleted_at IS NULL AND a.deleted_at IS NULL
AND (m.path IN ('/gb28181/zlm/overview','/gb28181/zlm/nodes','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/config') OR m.permission IN ('gb28181:zlm:node:manage','gb28181:zlm:node:kick','gb28181:zlm:scheduler:manage','gb28181:zlm:stream:preview','gb28181:zlm:stream:close','gb28181:zlm:stream:force-close','gb28181:zlm:session:kick','gb28181:zlm:proxy:manage','gb28181:zlm:ffmpeg:manage','gb28181:zlm:rtp:manage','gb28181:zlm:rtp:force-close','gb28181:recording:control','gb28181:recording:force-stop','gb28181:zlm:config:update','gb28181:zlm:restart','gb28181:recording:view','gb28181:recording:reconcile','gb28181:recording:delete','gb28181:recording:stop','gb28181:recording-plan:view','gb28181:recording-plan:maintain','gb28181:recording-plan:assign'))
AND (a.path LIKE '/api/gb28181/zlm/%' OR a.path LIKE '/api/gb28181/cloud-recordings/%' OR a.path LIKE '/api/gb28181/recording-plans%')
AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||CAST(rm.role_id AS varchar(20)) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
-- media-management-baseline:end

-- media-workbench-v2:start
-- Flatten media management into six visible workspaces while preserving legacy permission anchors (PostgreSQL).
INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT 0,'/media','Media','','gb28181/zlm/workbench/MediaEntry','流媒体管理','lucide:Clapperboard',9,1,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=0,name='Media',redirect='',component='gb28181/zlm/workbench/MediaEntry',title='流媒体管理',icon='lucide:Clapperboard',sort=9,type=1,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/overview','media-overview','','gb28181/zlm/workbench/MediaOverview','媒体总览','lucide:LayoutDashboard',10,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/overview' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-overview',redirect='',component='gb28181/zlm/workbench/MediaOverview',title='媒体总览',icon='lucide:LayoutDashboard',sort=10,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/overview' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/monitoring','media-monitoring','','gb28181/zlm/workbench/MediaMonitoring','媒体监控','lucide:Activity',20,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/monitoring' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-monitoring',redirect='',component='gb28181/zlm/workbench/MediaMonitoring',title='媒体监控',icon='lucide:Activity',sort=20,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/monitoring' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/ingress','media-ingress','','gb28181/zlm/workbench/IngressManagement','接入管理','lucide:RadioTower',30,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/ingress' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-ingress',redirect='',component='gb28181/zlm/workbench/IngressManagement',title='接入管理',icon='lucide:RadioTower',sort=30,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/ingress' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/recordings','media-recordings','','gb28181/zlm/workbench/RecordingCenter','录制中心','lucide:Cloud',40,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/recordings' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-recordings',redirect='',component='gb28181/zlm/workbench/RecordingCenter',title='录制中心',icon='lucide:Cloud',sort=40,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/recordings' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/nodes','media-nodes','','gb28181/zlm/workbench/NodeManagement','节点管理','lucide:Server',50,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/nodes' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-nodes',redirect='',component='gb28181/zlm/workbench/NodeManagement',title='节点管理',icon='lucide:Server',sort=50,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/nodes' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/scheduling','media-scheduling','','gb28181/zlm/workbench/SchedulingManagement','调度管理','lucide:Workflow',60,2,'',false,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/scheduling' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),name='media-scheduling',redirect='',component='gb28181/zlm/workbench/SchedulingManagement',title='调度管理',icon='lucide:Workflow',sort=60,type=2,permission='',hide=false,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/scheduling' AND deleted_at IS NULL;

INSERT INTO sys_menu (parent_id,path,name,redirect,component,title,icon,sort,type,permission,hide,keep_alive,created_at,updated_at,created_by)
SELECT p.id,'/media/nodes/:id','media-node-detail','','gb28181/zlm/workbench/nodes/NodeDetail','节点详情','lucide:Server',99,2,'',true,true,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu p
WHERE p.path='/media/nodes' AND p.deleted_at IS NULL
AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/media/nodes/:id' AND deleted_at IS NULL);
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media/nodes' AND deleted_at IS NULL),name='media-node-detail',redirect='',component='gb28181/zlm/workbench/nodes/NodeDetail',title='节点详情',icon='lucide:Server',sort=99,type=2,permission='',hide=true,keep_alive=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/media/nodes/:id' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/streams' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/sessions' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/proxies' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/ffmpeg-sources' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/rtp-servers' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/recording-schedules' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/nodes' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/nodes/:id' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/config' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler' AND deleted_at IS NULL;

UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP
WHERE path='/gb28181/zlm/scheduler/logs' AND deleted_at IS NULL;

-- workspace-role-union:/media
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/cloud-recordings','/gb28181/recording-schedules','/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/overview
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/overview' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/overview')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/monitoring
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/monitoring' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/ingress
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/ingress' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/recordings
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/recordings' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/cloud-recordings','/gb28181/recording-schedules')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/nodes
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/nodes' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/nodes/:id
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/nodes/:id' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- workspace-role-union:/media/scheduling
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT DISTINCT rm.role_id,target.id
FROM sys_role_menu rm
JOIN sys_menu source ON source.id=rm.menu_id AND source.deleted_at IS NULL
JOIN sys_menu target ON target.path='/media/scheduling' AND target.deleted_at IS NULL
WHERE source.path IN ('/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs')
AND NOT EXISTS (SELECT 1 FROM sys_role_menu existing WHERE existing.role_id=rm.role_id AND existing.menu_id=target.id);

-- media-workbench-v2:end
-- zlm-admin-parity-v3:start
-- Restore zlm-admin-style direct pages and keep GB28181 recordings independent (PostgreSQL).
UPDATE sys_menu SET redirect='/gb28181/zlm/overview',component='',title='流媒体管理',icon='lucide:Clapperboard',hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/media' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/ClusterOverview',title='集群总览',icon='lucide:LayoutDashboard',sort=10,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/NodeList',title='节点管理',icon='lucide:Server',sort=20,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/nodes' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/RuntimeOverview',title='总览',icon='lucide:Gauge',sort=30,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/StreamManagement',title='流管理',icon='lucide:RadioTower',sort=40,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/streams' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/SessionManagement',title='会话管理',icon='lucide:Users',sort=50,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/sessions' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/ProxyManagement',title='拉流/推流代理',icon='lucide:Network',sort=60,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/proxies' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/FFmpegSources',title='FFmpeg 源',icon='lucide:Clapperboard',sort=70,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/ffmpeg-sources' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/RTPServices',title='RTP 服务',icon='lucide:Waypoints',sort=80,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/rtp-servers' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/ServerConfig',title='服务器配置',icon='lucide:Settings2',sort=90,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/config' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/SchedulerStrategy',title='调度策略',icon='lucide:Workflow',sort=100,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/scheduler' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/SchedulerLog',title='调度日志',icon='lucide:History',sort=110,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/scheduler/logs' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=0,component='gb28181/zlm/NodeDetail',title='节点详情',hide=true,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/nodes/:id' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT dm.parent_id FROM sys_menu dm WHERE dm.path IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND dm.deleted_at IS NULL ORDER BY CASE WHEN dm.path='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,dm.id LIMIT 1),component='gb28181/cloud-recordings/index',title='云端录像',icon='lucide:Cloud',sort=35,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/cloud-recordings' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT dm.parent_id FROM sys_menu dm WHERE dm.path IN ('/gb28181/device-mgmt/index','/gb28181/device-mgmt') AND dm.deleted_at IS NULL ORDER BY CASE WHEN dm.path='/gb28181/device-mgmt/index' THEN 0 ELSE 1 END,dm.id LIMIT 1),component='gb28181/recording-schedules/index',title='录像计划',icon='lucide:CalendarClock',sort=36,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/recording-schedules' AND deleted_at IS NULL;
UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=true,updated_at=CURRENT_TIMESTAMP WHERE path IN ('/media/overview','/media/monitoring','/media/ingress','/media/recordings','/media/nodes','/media/scheduling','/media/nodes/:id') AND deleted_at IS NULL;
-- zlm-admin-parity-v3:end

-- zlm-overview-merge:start
UPDATE sys_menu SET component='gb28181/zlm/ClusterOverview',title='总览',icon='lucide:LayoutDashboard',sort=10,hide=false,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/overview' AND deleted_at IS NULL;
UPDATE sys_menu SET component='gb28181/zlm/RuntimeOverview',hide=true,updated_at=CURRENT_TIMESTAMP WHERE path='/gb28181/zlm/runtime' AND deleted_at IS NULL;
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT rm.role_id,o.id FROM sys_role_menu rm JOIN sys_menu r ON r.id=rm.menu_id AND r.path='/gb28181/zlm/runtime' AND r.deleted_at IS NULL CROSS JOIN sys_menu o
WHERE o.path='/gb28181/zlm/overview' AND o.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=rm.role_id AND x.menu_id=o.id);
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT o.id,a.id FROM sys_menu o CROSS JOIN sys_api a WHERE o.path='/gb28181/zlm/overview' AND o.deleted_at IS NULL AND a.deleted_at IS NULL AND a.method='GET'
AND a.path IN ('/api/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/runtime') AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=o.id AND ma.api_id=a.id);
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||CAST(rm.role_id AS varchar(20)),a.path,a.method,'*','','' FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id
WHERE m.path='/gb28181/zlm/overview' AND m.deleted_at IS NULL AND a.deleted_at IS NULL
AND a.path IN ('/api/gb28181/zlm/overview','/api/gb28181/zlm/nodes','/api/gb28181/zlm/nodes/:id/runtime')
AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||CAST(rm.role_id AS varchar(20)) AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
-- zlm-overview-merge:end
-- zlm-single-menu-workbench:start
UPDATE sys_menu SET redirect='/media/overview',component='',title='流媒体管理',icon='lucide:Clapperboard',sort=9,type=1,hide=0,keep_alive=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/workbench/MediaOverview',title='运行总览',sort=10,type=2,hide=1,keep_alive=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media/overview' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/workbench/MediaMonitoring',title='流与会话',sort=20,type=2,hide=1,keep_alive=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media/monitoring' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/workbench/IngressManagement',title='接入管理',sort=30,type=2,hide=1,keep_alive=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media/ingress' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/workbench/NodeManagement',title='节点管理',sort=40,type=2,hide=1,keep_alive=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media/nodes' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media' AND deleted_at IS NULL),component='gb28181/zlm/workbench/SchedulingManagement',title='调度管理',sort=50,type=2,hide=1,keep_alive=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media/scheduling' AND deleted_at IS NULL;
UPDATE sys_menu SET parent_id=(SELECT MIN(id) FROM sys_menu WHERE path='/media/nodes' AND deleted_at IS NULL),component='gb28181/zlm/workbench/nodes/NodeDetail',title='节点详情',hide=1,keep_alive=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media/nodes/:id' AND deleted_at IS NULL;
UPDATE sys_menu SET component='gb28181/zlm/workbench/LegacyMediaRoute',hide=1,updated_at=CURRENT_TIMESTAMP WHERE path IN ('/gb28181/zlm/overview','/gb28181/zlm/runtime','/gb28181/zlm/streams','/gb28181/zlm/sessions','/gb28181/zlm/proxies','/gb28181/zlm/ffmpeg-sources','/gb28181/zlm/rtp-servers','/gb28181/zlm/nodes','/gb28181/zlm/nodes/:id','/gb28181/zlm/config','/gb28181/zlm/scheduler','/gb28181/zlm/scheduler/logs') AND deleted_at IS NULL;
-- zlm-single-menu-workbench:end
-- zlm-global-sidebar-menu:start
UPDATE sys_menu SET hide=0,updated_at=CURRENT_TIMESTAMP WHERE path IN ('/media/overview','/media/monitoring','/media/ingress','/media/nodes','/media/scheduling') AND deleted_at IS NULL;
UPDATE sys_menu SET hide=1,updated_at=CURRENT_TIMESTAMP WHERE path='/media/nodes/:id' AND deleted_at IS NULL;
-- zlm-global-sidebar-menu:end

-- device-maintenance:start
-- Separate viewing maintenance records from executing a whole-device reboot.

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备维护','/api/gb28181/device-mgmt/device/:id/maintenance-operations','GET','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceMaintenanceView','','查看设备维护',1,0,1,3,'gb28181:device:maintenance:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_' || rm.role_id,a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启设备','/api/gb28181/device-mgmt/device/:id/reboot','POST','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/reboot' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceReboot','','重启设备',1,0,1,3,'gb28181:device:reboot','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:reboot' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_' || rm.role_id,a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:reboot' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- device-maintenance:end

-- device-firmware-upgrade:start
-- Firmware upgrade audit records. Session and idempotency keys are case-sensitive.
CREATE TABLE IF NOT EXISTS gb_device_firmware_upgrade (
  id BIGSERIAL PRIMARY KEY,
  operation_id VARCHAR(64) NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  device_id BIGINT NOT NULL,
  device_code VARCHAR(20) NOT NULL,
  firmware VARCHAR(255) NOT NULL,
  file_url VARCHAR(2048) NOT NULL,
  manufacturer VARCHAR(255) NOT NULL,
  session_id VARCHAR(128) NOT NULL,
  sn BIGINT NOT NULL,
  profile_version VARCHAR(8) NOT NULL,
  profile_charset VARCHAR(16) NOT NULL,
  sip_status INT DEFAULT 0 NOT NULL,
  sip_call_id VARCHAR(255) NULL,
  sip_cseq VARCHAR(64) NULL,
  device_result VARCHAR(16) NULL,
  device_error TEXT NULL,
  status VARCHAR(16) NOT NULL,
  error_code VARCHAR(64) NULL,
  error_message TEXT NULL,
  failed_reason VARCHAR(8) NULL,
  current_firmware VARCHAR(255) NULL,
  actor_id BIGINT DEFAULT 0 NOT NULL,
  actor_dept_id BIGINT DEFAULT 0 NOT NULL,
  created_at TIMESTAMP(6) NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL,
  sent_at TIMESTAMP(6) NULL,
  accepted_at TIMESTAMP(6) NULL,
  completed_at TIMESTAMP(6) NULL,
  deadline_at TIMESTAMP(6) NULL,
  response_at TIMESTAMP(6) NULL,
  response_call_id VARCHAR(255) NULL,
  response_cseq VARCHAR(64) NULL,
  CONSTRAINT uk_firmware_upgrade_operation UNIQUE (operation_id),
  CONSTRAINT uk_firmware_upgrade_device_idempotency UNIQUE (device_id,idempotency_key),
  CONSTRAINT uk_firmware_upgrade_device_session UNIQUE (device_id,session_id),
  CONSTRAINT uk_firmware_upgrade_sn UNIQUE (sn)
);
CREATE INDEX IF NOT EXISTS idx_firmware_upgrade_device_sn ON gb_device_firmware_upgrade (device_code,sn);
CREATE INDEX IF NOT EXISTS idx_firmware_upgrade_device_session ON gb_device_firmware_upgrade (device_code,session_id);
CREATE INDEX IF NOT EXISTS idx_firmware_upgrade_device_status ON gb_device_firmware_upgrade (device_id,status);
CREATE INDEX IF NOT EXISTS idx_firmware_upgrade_device_time ON gb_device_firmware_upgrade (device_id,created_at);
CREATE INDEX IF NOT EXISTS idx_firmware_upgrade_deadline ON gb_device_firmware_upgrade (deadline_at);

-- device-firmware-upgrade-permissions:start
INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备升级','/api/gb28181/device-mgmt/device/:id/firmware-upgrades','GET','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceMaintenanceView','','查看设备维护',1,0,1,3,'gb28181:device:maintenance:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_' || rm.role_id,a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:maintenance:view' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '升级设备','/api/gb28181/device-mgmt/device/:id/firmware-upgrade','POST','GB28181 设备维护',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE name='device-mgmt-list' AND deleted_at IS NULL),0),'','GbDeviceUpgrade','','升级设备',1,0,1,3,'gb28181:device:upgrade','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:upgrade' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id) SELECT 1,m.id FROM sys_menu m WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_role_menu r WHERE r.role_id=1 AND r.menu_id=m.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT 'p','role_' || rm.role_id,a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:device:upgrade' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_' || rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- device-firmware-upgrade:end

-- Current GB28181 security schema; keep column parity with the MySQL snapshot.

CREATE TABLE IF NOT EXISTS gb_sip_security_event (
  "id" BIGSERIAL NOT NULL,
  "bucket_at" TIMESTAMP NOT NULL,
  "source_ip" varchar(64) NOT NULL,
  "device_id" varchar(64) DEFAULT NULL,
  "risk_scope" varchar(16) DEFAULT NULL,
  "address_family" varchar(8) NOT NULL,
  "transport" varchar(8) NOT NULL,
  "method" varchar(16) NOT NULL,
  "user_agent" varchar(255) NOT NULL DEFAULT '',
  "reason" varchar(32) NOT NULL,
  "action" varchar(16) NOT NULL,
  "count" bigint NOT NULL DEFAULT 0,
  "score_delta" bigint NOT NULL DEFAULT 0,
  "first_seen_at" TIMESTAMP NOT NULL,
  "last_seen_at" TIMESTAMP NOT NULL,
  "sample_event_id" varchar(64) NOT NULL DEFAULT '',
  PRIMARY KEY ("id"),
  CONSTRAINT "uk_gb_sip_security_event" UNIQUE ("bucket_at", "source_ip", "device_id", "risk_scope", "transport", "method", "reason", "action")
);

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_event_source_time" ON gb_sip_security_event ("source_ip", "last_seen_at");

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_event_attribution_time" ON gb_sip_security_event ("risk_scope", "device_id", "last_seen_at");

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_event_reason_time" ON gb_sip_security_event ("reason", "last_seen_at");

CREATE TABLE IF NOT EXISTS gb_sip_security_ban (
  "id" BIGSERIAL NOT NULL,
  "source_ip" varchar(64) NOT NULL,
  "device_id" varchar(64) DEFAULT NULL,
  "risk_scope" varchar(16) DEFAULT NULL,
  "address_family" varchar(8) NOT NULL,
  "status" varchar(16) NOT NULL,
  "reason" varchar(32) NOT NULL,
  "rule_id" varchar(64) NOT NULL,
  "score" int NOT NULL DEFAULT 0,
  "created_at" TIMESTAMP NOT NULL,
  "expires_at" TIMESTAMP DEFAULT NULL,
  "unbanned_at" TIMESTAMP DEFAULT NULL,
  "unbanned_by" varchar(64) NOT NULL DEFAULT '',
  "origin" varchar(16) NOT NULL,
  "agent_state" varchar(16) NOT NULL,
  "decision_id" varchar(64) NOT NULL,
  "last_error" varchar(512) NOT NULL DEFAULT '',
  "trigger_method" varchar(16) NOT NULL DEFAULT '',
  "trigger_count" int NOT NULL DEFAULT 0,
  "trigger_threshold" int NOT NULL DEFAULT 0,
  "window_seconds" int NOT NULL DEFAULT 0,
  "policy_mode" varchar(16) NOT NULL DEFAULT '',
  "firewall_applied_at" TIMESTAMP DEFAULT NULL,
  "blocked_count_after_ban" bigint NOT NULL DEFAULT 0,
  "last_blocked_at" TIMESTAMP DEFAULT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uk_gb_sip_security_ban_decision" UNIQUE ("decision_id")
);

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_ban_source_status" ON gb_sip_security_ban ("source_ip", "status");

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_ban_attribution_status" ON gb_sip_security_ban ("risk_scope", "device_id", "status");

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_ban_expiry" ON gb_sip_security_ban ("expires_at");

CREATE TABLE IF NOT EXISTS gb_sip_security_policy (
  "id" BIGSERIAL NOT NULL,
  "scope_key" varchar(32) NOT NULL,
  "mode" varchar(16) NOT NULL,
  "window_seconds" int NOT NULL,
  "ban_score" int NOT NULL,
  "max_packet_bytes" int NOT NULL,
  "max_udp_per_window" int NOT NULL,
  "max_tcp_connections" int NOT NULL,
  "sample_per_source" int NOT NULL,
  "nonce_ttl_seconds" int NOT NULL,
  "ban_ttl_steps" varchar(1024) NOT NULL,
  "allowlist_text" varchar(4096) NOT NULL,
  "updated_by" bigint NOT NULL DEFAULT 0,
  "updated_at" TIMESTAMP NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uk_gb_sip_security_policy_scope" UNIQUE ("scope_key")
);

CREATE TABLE IF NOT EXISTS gb_sip_security_audit (
  "id" BIGSERIAL NOT NULL,
  "actor" varchar(64) NOT NULL,
  "action" varchar(32) NOT NULL,
  "target" varchar(128) NOT NULL,
  "reason" varchar(255) NOT NULL,
  "decision_id" varchar(64) NOT NULL DEFAULT '',
  "created_at" TIMESTAMP NOT NULL,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_audit_time" ON gb_sip_security_audit ("created_at");

CREATE TABLE IF NOT EXISTS gb_sip_security_access_rule (
  "id" BIGSERIAL NOT NULL,
  "list_type" varchar(16) NOT NULL,
  "match_type" varchar(16) NOT NULL,
  "match_value" varchar(255) NOT NULL,
  "scope" varchar(32) NOT NULL DEFAULT 'all_sip',
  "status" varchar(16) NOT NULL DEFAULT 'enabled',
  "expires_at" TIMESTAMP DEFAULT NULL,
  "note" varchar(255) NOT NULL DEFAULT '',
  "created_by" varchar(64) NOT NULL DEFAULT '',
  "created_at" TIMESTAMP NOT NULL,
  "updated_at" TIMESTAMP NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "uk_gb_sip_security_access_rule_match" UNIQUE ("list_type", "match_type", "match_value")
);

CREATE INDEX IF NOT EXISTS "idx_gb_sip_security_access_rule_list_status" ON gb_sip_security_access_rule ("list_type", "status");

-- GB28181 security policy seed.
INSERT INTO gb_sip_security_policy (scope_key,mode,window_seconds,ban_score,max_packet_bytes,max_udp_per_window,max_tcp_connections,sample_per_source,nonce_ttl_seconds,ban_ttl_steps,allowlist_text,updated_by,updated_at)
VALUES ('global','protect',10,100,65536,120,32,3,60,'100:0',E'127.0.0.0/8\n10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16',0,CURRENT_TIMESTAMP)
ON CONFLICT (scope_key) DO NOTHING;

-- button-permission-catalog:start

-- Generated by scripts/button-catalog.py. Add catalog metadata only; do not grant roles or rebuild policies.

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'/system/sysparam','SystemSysparam','system/sysparam/sysparam','参数管理',1,1,100,2,'','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/system/sysparam' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '物理删除告警','/api/gb28181/alarms/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/alarms/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups/:id/channels','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups/:id/channels' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '下载云端录像','/api/gb28181/cloud-recordings/downloads/:taskId','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/downloads/:taskId' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除录像文件','/api/gb28181/cloud-recordings/files/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/files/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除通道（单个/批量）','/api/gb28181/device-mgmt/channel/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除预置位','/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '语音对讲与广播会话','/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除设备（单个/批量）','/api/gb28181/device-mgmt/device/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重置仪表盘布局','/api/gb28181/home/layout','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/layout' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制停止当前直播/共享停播','/api/gb28181/play/:streamId','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:streamId' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除安全访问规则','/api/gb28181/security/access-rules/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/access-rules/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 FFmpeg 源','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/pull/:key','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/push/:key','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/push/:key' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/plugins/example/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '插件卸载','/api/pluginsmanager/uninstall','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/pluginsmanager/uninstall' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/cancel','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/cancel' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除文件','/api/sysAffix/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysApi/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除部门','/api/sysDepartment/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysDict/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDict/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除字典项','/api/sysDictItem/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysGen/:id','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/:id' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysJobResults/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobResults/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysJobs/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除登录日志','/api/sysLoginLog/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysMenu/batchDelete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/batchDelete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysMenu/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysOperationLog/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysOperationLog/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除参数','/api/sysParam/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysParam/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/sysRole/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除','/api/users/delete','DELETE','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/delete' AND method='DELETE' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '预览','/api/codegen/preview','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/preview' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入表','/api/codegen/tables','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/tables' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看国标级联','/api/gb28181/cascade/platforms','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享国标级联资源','/api/gb28181/cascade/platforms/:id/shares','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/shares' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '下载云端录像','/api/gb28181/cloud-recordings/downloads/:taskId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/downloads/:taskId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '执行录像对账','/api/gb28181/cloud-recordings/reconciliations','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/reconciliations' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/catalog/tree','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/catalog/tree' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/catalog/tree/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/catalog/tree/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/catalog/tree/:id/children','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/catalog/tree/:id/children' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channel/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/control-capabilities','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/control-capabilities' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/device-status','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/device-status' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channel/:id/mounts','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/mounts' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks/:trackId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks/:trackId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/home-position','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/precise-status','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/precise-status' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台能力、状态与预置/巡航资源读取','/api/gb28181/device-mgmt/channel/:id/ptz/presets','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查询设备录像','/api/gb28181/device-mgmt/channel/:id/record-query/options','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/record-query/options' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备图像抓拍会话','/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '语音对讲与广播会话','/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channel/:id/timeline','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/timeline' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/channels','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channels' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/device/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备维护','/api/gb28181/device-mgmt/device/:id/firmware-upgrades','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看设备维护','/api/gb28181/device-mgmt/device/:id/maintenance-operations','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/device/:id/status-events','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/status-events' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备订阅读取、更新与续订','/api/gb28181/device-mgmt/device/:id/subscriptions','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/subscriptions' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/devices','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/devices' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/directory/tree','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/directory/tree' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/map/clusters','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/map/clusters' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备、通道、目录与地图只读查询','/api/gb28181/device-mgmt/map/markers','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/map/markers' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享设备','/api/gb28181/device-mgmt/permission-workbench/grant-targets','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/coverage','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/coverage' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/realtime','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/realtime' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/sessions','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/sessions' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/summary','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/summary' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/trend','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/trend' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看运行监控','/api/gb28181/device-traffic/viewers','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/viewers' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/drilldown/play','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/drilldown/play' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/drilldown/sip','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/drilldown/sip' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/drilldown/traffic','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/drilldown/traffic' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/layout','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/layout' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看仪表盘','/api/gb28181/home/summary','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/summary' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '播放流监控读取','/api/gb28181/play/:streamId/monitor','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:streamId/monitor' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/:id/assignment-options/channels','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/assignment-options/channels' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/:id/assignment-options/devices','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/assignment-options/devices' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP Trace 报文及会话详情','/api/gb28181/sip-traces/messages/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip-traces/messages/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP Trace 报文及会话详情','/api/gb28181/sip-traces/sessions/:callId/messages','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip-traces/sessions/:callId/messages' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/platform','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/platform' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/default-channel-audio','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-channel-audio' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/default-playback-protocol','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-playback-protocol' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/fixed-address-playback','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/fixed-address-playback' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/global-subscriptions','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/global-subscriptions' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/ignore-channel-offline-status-notify','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/online-on-heartbeat','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/online-on-heartbeat' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/play-auth','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/play-auth' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/preallocation-mode','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/preallocation-mode' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/ptz-default-speed','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ptz-default-speed' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/save-alarm-messages','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/save-alarm-messages' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/sip-command-timeout','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-command-timeout' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/sip-log','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-log' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/service-config/sync-channels-on-online','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sync-channels-on-online' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/setup/network-interfaces','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/network-interfaces' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/setup/status','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/status' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 ZLM 节点详情','/api/gb28181/zlm/nodes/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启媒体服务','/api/gb28181/zlm/nodes/:id/restart','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/restart' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看媒体流详情','/api/gb28181/zlm/nodes/:id/streams/detail','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/detail' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '预览与截图','/api/gb28181/zlm/nodes/:id/streams/snapshot','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/snapshot' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看媒体流详情','/api/gb28181/zlm/nodes/:id/streams/viewers','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/viewers' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/plugins/example/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '复制链接','/api/sysAffix/download/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/download/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysApi/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysApi/list','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/list' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '数据权限','/api/sysDepartment/getDivision','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/getDivision' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/sysDictItem/getByDictCode/:dictCode','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/getByDictCode/:dictCode' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '字典项管理','/api/sysDictItem/getByDictId/:dictId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/getByDictId/:dictId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '配置','/api/sysGen/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看定时任务日志','/api/sysJobResults/list','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobResults/list' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysJobs/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看登录日志详情','/api/sysLoginLog/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysMenu/apis/:id','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/apis/:id' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导出','/api/sysMenu/export','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/export' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysMenu/getMenuList','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/getMenuList' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导出','/api/sysOperationLog/export','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysOperationLog/export' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysRole/getUserPermission/:roleId','GET','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/getUserPermission/:roleId' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑通道参数','/api/gb28181/device-mgmt/channel/:id','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '切换通道云端录制','/api/gb28181/device-mgmt/channel/:id/cloud-recording','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/cloud-recording' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台归位点更新','/api/gb28181/device-mgmt/channel/:id/ptz/home-position','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改通道传输模式','/api/gb28181/device-mgmt/channel/:id/stream-transport','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/stream-transport' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备订阅读取、更新与续订','/api/gb28181/device-mgmt/device/:id/subscriptions/:kind','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑设备','/api/gb28181/device/:deviceId','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device/:deviceId' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id/status','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/status' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/channels/:channelId/recording-mode','PATCH','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/channels/:channelId/recording-mode' AND method='PATCH' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '生成代码文件','/api/codegen/generate','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/generate' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '生成菜单','/api/codegen/insertmenuandapi','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/codegen/insertmenuandapi' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '物理删除告警','/api/gb28181/alarms/batch-delete','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/alarms/batch-delete' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '清空全部告警','/api/gb28181/alarms/clear-all','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/alarms/clear-all' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '启停国标级联','/api/gb28181/cascade/platforms/:id/enable','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/enable' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重连国标级联','/api/gb28181/cascade/platforms/:id/reconnect','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/reconnect' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '通道收藏管理','/api/gb28181/channel-favorite-groups/:id/channels','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/channel-favorite-groups/:id/channels' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '停止录像','/api/gb28181/cloud-recordings/active/:id/stop','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/active/:id/stop' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '下载云端录像','/api/gb28181/cloud-recordings/files/:id/downloads','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/files/:id/downloads' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除录像文件','/api/gb28181/cloud-recordings/files/batch-delete','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/files/batch-delete' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '执行录像对账','/api/gb28181/cloud-recordings/reconciliations','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cloud-recordings/reconciliations' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备复合控制（关键帧、录制、守望、告警、拖拽变焦等）','/api/gb28181/device-mgmt/channel/:id/device-control','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/device-control' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像下载','/api/gb28181/device-mgmt/channel/:id/download-sessions','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/download-sessions' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备录像回放会话（播放、暂停/续播、倍速、停止）','/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台移动、变倍、聚焦、光圈（基础/精准/扩展）','/api/gb28181/device-mgmt/channel/:id/ptz','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台巡航轨迹创建、启动、停止与删除','/api/gb28181/device-mgmt/channel/:id/ptz/cruise','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台巡航轨迹创建、启动、停止与删除','/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台移动、变倍、聚焦、光圈（基础/精准/扩展）','/api/gb28181/device-mgmt/channel/:id/ptz/extended','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/extended' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '云台移动、变倍、聚焦、光圈（基础/精准/扩展）','/api/gb28181/device-mgmt/channel/:id/ptz/precise','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/precise' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '保存预置位','/api/gb28181/device-mgmt/channel/:id/ptz/presets','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '调用预置位','/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查询设备录像','/api/gb28181/device-mgmt/channel/:id/record-query','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/record-query' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备图像抓拍会话','/api/gb28181/device-mgmt/channel/:id/snapshot-sessions','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '语音对讲与广播会话','/api/gb28181/device-mgmt/channel/:id/talk-sessions','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/:id/talk-sessions' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除通道（单个/批量）','/api/gb28181/device-mgmt/channel/batch-delete','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/channel/batch-delete' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/devices','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id/devices' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/devices/remove','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id/devices/remove' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理自定义分组','/api/gb28181/device-mgmt/custom-groups/:id/move','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/custom-groups/:id/move' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新建设备','/api/gb28181/device-mgmt/device','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '刷新设备目录（下发 SIP 目录查询）','/api/gb28181/device-mgmt/device/:id/catalog/refresh','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/catalog/refresh' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '升级设备','/api/gb28181/device-mgmt/device/:id/firmware-upgrade','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启设备','/api/gb28181/device-mgmt/device/:id/reboot','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/reboot' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '设备订阅读取、更新与续订','/api/gb28181/device-mgmt/device/:id/subscriptions/:kind/renew','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind/renew' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '删除设备（单个/批量）','/api/gb28181/device-mgmt/device/batch-delete','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/device/batch-delete' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配设备归属','/api/gb28181/device-mgmt/permission-workbench/assignments/departments','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配设备归属','/api/gb28181/device-mgmt/permission-workbench/devices/resolve','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享设备','/api/gb28181/device-mgmt/permission-workbench/grants/apply','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享设备','/api/gb28181/device-mgmt/permission-workbench/grants/query','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强退观看连接','/api/gb28181/device-traffic/viewers/kick','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/device-traffic/viewers/kick' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '发起实时点播','/api/gb28181/play/:deviceId/:channelId','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:deviceId/:channelId' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '发起实时点播','/api/gb28181/play/:deviceId/:channelId/authorization','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/play/:deviceId/:channelId/authorization' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配录像计划','/api/gb28181/recording-plans/:id/assignments','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id/assignments' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增访问规则（黑白名单）','/api/gb28181/security/access-rules','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/access-rules' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '解除自动封禁','/api/gb28181/security/bans/:id/unban','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/bans/:id/unban' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看 SIP 配置','/api/gb28181/sip/qr/token','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL OR (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/qr/token' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/setup/skip','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/skip' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '视频探针诊断','/api/gb28181/stream-probes/:streamId','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/stream-probes/:streamId' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id/activate','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/activate' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '更新服务配置','/api/gb28181/zlm/nodes/:id/config/test-connection','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/config/test-connection' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 FFmpeg 源','/api/gb28181/zlm/nodes/:id/ffmpeg-sources','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 FFmpeg 源','/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '踢除节点会话','/api/gb28181/zlm/nodes/:id/kick','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/kick' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id/maintenance','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/maintenance' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/pull','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/pull' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/push','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/push' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理代理','/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制停止录制','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制停止录制','/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/start','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '手工录制控制','/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '重启媒体服务','/api/gb28181/zlm/nodes/:id/restart','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/restart' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers/close','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers/close' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制关闭 RTP 服务','/api/gb28181/zlm/nodes/:id/rtp-servers/force-close','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/rtp-servers/force-close' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '踢除会话','/api/gb28181/zlm/nodes/:id/sessions/kick','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/sessions/kick' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close/batch','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close/batch' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '关闭流','/api/gb28181/zlm/nodes/:id/streams/close/preflight','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/close/preflight' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制关闭流','/api/gb28181/zlm/nodes/:id/streams/force-close','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/force-close' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '预览与截图','/api/gb28181/zlm/nodes/:id/streams/playback-grant','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/streams/playback-grant' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/probe','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/probe' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/plugins/example/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导出插件','/api/pluginsmanager/export','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/pluginsmanager/export' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入插件','/api/pluginsmanager/import','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/pluginsmanager/import' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/init','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/init' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/merge','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/merge' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/chunk/upload','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/chunk/upload' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '大文件上传','/api/sysAffix/upload','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/upload' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysApi/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增部门','/api/sysDepartment/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysDict/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDict/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增字典项','/api/sysDictItem/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入表','/api/sysGen/batchInsert','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/batchInsert' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysJobs/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '执行一次','/api/sysJobs/executeNow','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/executeNow' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '清空登录日志','/api/sysLoginLog/clear','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/clear' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '解锁登录账号','/api/sysLoginLog/unlock','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysLoginLog/unlock' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysMenu/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '导入','/api/sysMenu/import','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/import' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysMenu/setApis','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/setApis' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '强制下线','/api/sysOnlineUser/forceLogout','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysOnlineUser/forceLogout' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增参数','/api/sysParam/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysParam/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/sysRole/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '分配权限','/api/sysRole/addRoleMenu','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/addRoleMenu' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '新增','/api/users/add','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/add' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '上传个人头像','/api/users/uploadAvatar','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/uploadAvatar' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改系统配置','/api/config/update','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/config/update' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理国标级联','/api/gb28181/cascade/platforms/:id','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '启停国标级联','/api/gb28181/cascade/platforms/:id/enabled','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/enabled' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '共享国标级联资源','/api/gb28181/cascade/platforms/:id/shares','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/cascade/platforms/:id/shares' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '保存仪表盘布局','/api/gb28181/home/layout','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/home/layout' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理播放方案','/api/gb28181/playback-schemes/:id/layout','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/playback-schemes/:id/layout' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '维护录像计划','/api/gb28181/recording-plans/:id','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/recording-plans/:id' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '启用或停用安全访问规则','/api/gb28181/security/access-rules/:id','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/access-rules/:id' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '保存安全防护策略','/api/gb28181/security/policy','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/security/policy' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/default-channel-audio','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-channel-audio' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/default-channel-stream-transport','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-channel-stream-transport' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/default-playback-protocol','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-playback-protocol' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/fixed-address-playback','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/fixed-address-playback' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/global-subscriptions','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/global-subscriptions' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/ignore-channel-offline-status-notify','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/online-on-heartbeat','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/online-on-heartbeat' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/play-auth','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/play-auth' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/playback-settings','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/playback-settings' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/position-history','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/position-history' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/preallocation-mode','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/preallocation-mode' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/ptz-default-speed','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/ptz-default-speed' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/save-alarm-messages','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/save-alarm-messages' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sdp-extension','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sdp-extension' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sip-command-timeout','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-command-timeout' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sip-log','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sip-log' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/service-config/sync-channels-on-online','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/sync-channels-on-online' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改 SIP 配置','/api/gb28181/sip/setup/config','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/setup/config' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '管理节点','/api/gb28181/zlm/nodes/:id','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '更新服务配置','/api/gb28181/zlm/nodes/:id/config','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/nodes/:id/config' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '切换调度策略','/api/gb28181/zlm/scheduler','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/scheduler' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/plugins/example/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/plugins/example/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改文件名','/api/sysAffix/updateName','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysAffix/updateName' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysApi/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysApi/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑部门','/api/sysDepartment/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDepartment/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysDict/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDict/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑字典项','/api/sysDictItem/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysDictItem/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '同步数据库','/api/sysGen/refreshFields','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/refreshFields' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '配置','/api/sysGen/update','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysGen/update' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysJobs/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysJobs/setStatus','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysJobs/setStatus' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysMenu/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysMenu/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑参数','/api/sysParam/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysParam/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '数据权限','/api/sysRole/dataScope','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/dataScope' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/sysRole/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/sysRole/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '编辑','/api/users/edit','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/edit' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改密码、手机号等','/api/users/updateAccount','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/updateAccount' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '修改用户基本信息','/api/users/updateBasicInfo','PUT','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE ((SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL) AND NOT EXISTS (SELECT 1 FROM sys_api WHERE path='/api/users/updateBasicInfo' AND method='PUT' AND deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_alarm_clear','','清空全部告警',1,0,100,3,'gb28181:alarm:clear','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:alarm:clear' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:alarm:clear' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:alarm:clear' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/alarms/clear-all' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_alarm_delete','','物理删除告警',1,0,100,3,'gb28181:alarm:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:alarm:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:alarm:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/alarm-management' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:alarm:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/alarms/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:alarm:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/alarms/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_enable','','启停国标级联',1,0,100,3,'gb28181:cascade:enable','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:enable' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:enable' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:enable' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/enable' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:enable' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/enabled' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_manage','','管理国标级联',1,0,100,3,'gb28181:cascade:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_reconnect','','重连国标级联',1,0,100,3,'gb28181:cascade:reconnect','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:reconnect' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:reconnect' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:reconnect' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/reconnect' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_share','','共享国标级联资源',1,0,100,3,'gb28181:cascade:share','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:share' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:share' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/shares' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/shares' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_cascade_view','','查看国标级联',1,0,100,3,'gb28181:cascade:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:cascade:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:cascade:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cascade' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:cascade:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cascade/platforms/:id/shares' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_favorite_manage','','通道收藏管理',1,0,100,3,'gb28181:channel-favorite:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel-favorite:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel-favorite:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups/:id/channels' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel-favorite:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/channel-favorite-groups/:id/channels' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_delete','','删除通道（单个/批量）',1,0,100,3,'gb28181:channel:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_edit','','编辑通道参数',1,0,100,3,'gb28181:channel:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_recording_update','','切换通道云端录制',1,0,100,3,'gb28181:channel:recording:update','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:recording:update' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:recording:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:recording:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/cloud-recording' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_channel_stream_transport_update','','修改通道传输模式',1,0,100,3,'gb28181:channel:stream-transport:update','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:channel:stream-transport:update' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:channel:stream-transport:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:channel:stream-transport:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/stream-transport' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_group_manage','','管理自定义分组',1,0,100,3,'gb28181:device-group:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-group:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-group:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id/devices' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id/devices/remove' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-group:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/custom-groups/:id/move' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_record_download','','设备录像下载',1,0,100,3,'gb28181:device-record:download','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-record:download' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-record:download' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/download-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_record_play','','设备录像回放会话（播放、暂停/续播、倍速、停止）',1,0,100,3,'gb28181:device-record:play','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-record:play' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-record:play' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-record-playback/:channelId' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:play' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_record_query','','查询设备录像',1,0,100,3,'gb28181:device-record:query','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device-record:query' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device-record:query' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:query' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/record-query' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device-record:query' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/record-query/options' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_add','','新建设备',1,0,100,3,'gb28181:device:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_assign','','分配设备归属',1,0,100,3,'gb28181:device:assign','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:assign' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:assign' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/assignments' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/assignments/departments' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/devices/resolve' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_catalog_refresh','','刷新设备目录（下发 SIP 目录查询）',1,0,100,3,'gb28181:device:catalog:refresh','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:catalog:refresh' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:catalog:refresh' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:catalog:refresh' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/catalog/refresh' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_control','','设备复合控制（关键帧、录制、守望、告警、拖拽变焦等）',1,0,100,3,'gb28181:device:control','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:control' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/device-control' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_delete','','删除设备（单个/批量）',1,0,100,3,'gb28181:device:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_edit','','编辑设备',1,0,100,3,'gb28181:device:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device/:deviceId' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_maintenance_view','','查看设备维护',1,0,100,3,'gb28181:device:maintenance:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:maintenance:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:maintenance:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrades' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:maintenance:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/maintenance-operations' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_reboot','','重启设备',1,0,100,3,'gb28181:device:reboot','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:reboot' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:reboot' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:reboot' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/reboot' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_share','','共享设备',1,0,100,3,'gb28181:device:share','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:share' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:share' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-assignment' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grant-targets' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grants/apply' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:share' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/permission-workbench/grants/query' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_snapshot','','设备图像抓拍会话',1,0,100,3,'gb28181:device:snapshot','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:snapshot' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:snapshot' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:snapshot' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_subscription_manage','','设备订阅读取、更新与续订',1,0,100,3,'gb28181:device:subscription:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:subscription:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:subscription:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:subscription:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/subscriptions' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:subscription:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:subscription:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/subscriptions/:kind/renew' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_upgrade','','升级设备',1,0,100,3,'gb28181:device:upgrade','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:upgrade' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:upgrade' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:upgrade' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/firmware-upgrade' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_device_view','','设备、通道、目录与地图只读查询',1,0,100,3,'gb28181:device:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:device:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:device:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/catalog/tree' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/catalog/tree/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/catalog/tree/:id/children' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/mounts' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/timeline' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channels' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/device/:id/status-events' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/devices' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/directory/tree' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/map/clusters' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:device:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/map/markers' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_home_layout_reset','','重置仪表盘布局',1,0,100,3,'gb28181:home:layout:reset','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:home:layout:reset' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:home:layout:reset' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:layout:reset' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/layout' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_home_layout_save','','保存仪表盘布局',1,0,100,3,'gb28181:home:layout:save','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:home:layout:save' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:home:layout:save' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:layout:save' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/layout' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_home_view','','查看仪表盘',1,0,100,3,'gb28181:home:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:home:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:home:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/home' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/drilldown/play' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/drilldown/sip' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/drilldown/traffic' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/layout' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/home/summary' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_diagnose','','视频探针诊断',1,0,100,3,'gb28181:play:diagnose','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:diagnose' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:diagnose' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:diagnose' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/stream-probes/:streamId' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_monitor','','播放流监控读取',1,0,100,3,'gb28181:play:monitor','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:monitor' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:monitor' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:monitor' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:streamId/monitor' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_start','','发起实时点播',1,0,100,3,'gb28181:play:start','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:start' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:start' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:deviceId/:channelId' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:deviceId/:channelId/authorization' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_stop','','强制停止当前直播/共享停播',1,0,100,3,'gb28181:play:stop','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:stop' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:stop' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/play/:streamId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_playback_scheme_manage','','管理播放方案',1,0,100,3,'gb28181:playback-scheme:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:playback-scheme:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:playback-scheme:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:playback-scheme:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/playback-schemes/:id/layout' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_control','','云台移动、变倍、聚焦、光圈（基础/精准/扩展）',1,0,100,3,'gb28181:ptz:control','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:control' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/extended' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/precise' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_cruise','','云台巡航轨迹创建、启动、停止与删除',1,0,100,3,'gb28181:ptz:cruise','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:cruise' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:cruise' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:cruise' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:cruise' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise/tracks' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_home','','云台归位点更新',1,0,100,3,'gb28181:ptz:home','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:home' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:home' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:home' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_preset_call','','调用预置位',1,0,100,3,'gb28181:ptz:preset:call','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:preset:call' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:preset:call' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:preset:call' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId/call' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_preset_delete','','删除预置位',1,0,100,3,'gb28181:ptz:preset:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:preset:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:preset:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:preset:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_preset_save','','保存预置位',1,0,100,3,'gb28181:ptz:preset:save','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:preset:save' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:preset:save' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:preset:save' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_ptz_view','','云台能力、状态与预置/巡航资源读取',1,0,100,3,'gb28181:ptz:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:ptz:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:ptz:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/control-capabilities' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/device-status' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/cruise-tracks/:trackId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/home-position' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/operations/:operationId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/precise-status' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:ptz:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/ptz/presets' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_plan_assign','','分配录像计划',1,0,100,3,'gb28181:recording-plan:assign','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording-plan:assign' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording-plan:assign' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/assignment-options/channels' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/assignment-options/devices' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/assignments' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:assign' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/channels/:channelId/recording-mode' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_plan_maintain','','维护录像计划',1,0,100,3,'gb28181:recording-plan:maintain','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording-plan:maintain' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording-plan:maintain' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/recording-schedules' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording-plan:maintain' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/recording-plans/:id/status' AND a.method='PATCH' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_control','','手工录制控制',1,0,100,3,'gb28181:recording:control','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:control' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_delete','','删除录像文件',1,0,100,3,'gb28181:recording:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/files/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/files/batch-delete' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_download','','下载云端录像',1,0,100,3,'gb28181:recording:download','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:download' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:download' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/downloads/:taskId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/files/:id/downloads' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_force_stop','','强制停止录制',1,0,100,3,'gb28181:recording:force-stop','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:force-stop' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:force-stop' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:force-stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:force-stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_reconcile','','执行录像对账',1,0,100,3,'gb28181:recording:reconcile','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:reconcile' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:reconcile' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:reconcile' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/reconciliations' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:reconcile' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/reconciliations' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_recording_stop','','停止录像',1,0,100,3,'gb28181:recording:stop','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:recording:stop' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:recording:stop' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/cloud-recordings' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:recording:stop' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/cloud-recordings/active/:id/stop' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_ban_unban','','解除自动封禁',1,0,100,3,'gb28181:security:ban:unban','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:ban:unban' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:ban:unban' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:ban:unban' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/bans/:id/unban' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_policy_update','','保存安全防护策略',1,0,100,3,'gb28181:security:policy:update','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:policy:update' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:policy:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:policy:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/policy' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_rule_add','','新增访问规则（黑白名单）',1,0,100,3,'gb28181:security:rule:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:rule:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:rule:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:rule:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/access-rules' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_rule_delete','','删除安全访问规则',1,0,100,3,'gb28181:security:rule:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:rule:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:rule:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:rule:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/access-rules/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_security_rule_edit','','启用或停用安全访问规则',1,0,100,3,'gb28181:security:rule:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:security:rule:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:security:rule:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/security-preview' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:security:rule:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/security/access-rules/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_config_update','','修改 SIP 配置',1,0,100,3,'gb28181:sip:config:update','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:config:update' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:config:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-channel-audio' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-channel-stream-transport' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-playback-protocol' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/fixed-address-playback' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/global-subscriptions' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/online-on-heartbeat' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/playback-settings' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/position-history' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/preallocation-mode' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ptz-default-speed' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/save-alarm-messages' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sdp-extension' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-command-timeout' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-log' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sync-channels-on-online' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/config' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/skip' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_config_view','','查看 SIP 配置',1,0,100,3,'gb28181:sip:config:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:config:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:config:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/config' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/media')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/platform' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/qr/token' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-channel-audio' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-playback-protocol' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/fixed-address-playback' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/global-subscriptions' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ignore-channel-offline-status-notify' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/online-on-heartbeat' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/play-auth' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/preallocation-mode' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/ptz-default-speed' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/save-alarm-messages' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-command-timeout' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sip-log' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/sync-channels-on-online' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/network-interfaces' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/setup/status' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:config:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/getByDictCode/:dictCode' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_qr_create','','生成设备接入二维码',1,0,100,3,'gb28181:sip:qr:create','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:qr:create' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:qr:create' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip/platform' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:qr:create' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/qr/token' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_trace_export','','下载SIP日志',1,0,100,3,'gb28181:sip:trace:export','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:trace:export' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:trace:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_sip_trace_view','','查看 SIP Trace 报文及会话详情',1,0,100,3,'gb28181:sip:trace:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:sip:trace:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:sip:trace:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/sip-traces' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:trace:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip-traces/messages/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:sip:trace:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip-traces/sessions/:callId/messages' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_talk_control','','语音对讲与广播会话',1,0,100,3,'gb28181:talk:control','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:talk:control' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:talk:control' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:talk:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/talk-sessions' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:talk:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:talk:control' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_traffic_view','','查看运行监控',1,0,100,3,'gb28181:traffic:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:traffic:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:traffic:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/coverage' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/realtime' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/sessions' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/summary' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/trend' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/viewers' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_traffic_viewer_kick','','强退观看连接',1,0,100,3,'gb28181:traffic:viewer:kick','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:traffic:viewer:kick' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:traffic:viewer:kick' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/device-mgmt/index' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:traffic:viewer:kick' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/device-traffic/viewers/kick' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_config_update','','更新服务配置',1,0,100,3,'gb28181:zlm:config:update','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:config:update' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:config:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/config')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/config' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/config/test-connection' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_ffmpeg_manage','','管理 FFmpeg 源',1,0,100,3,'gb28181:zlm:ffmpeg:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:ffmpeg:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:ffmpeg:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/ffmpeg-sources')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:ffmpeg:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:ffmpeg:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:ffmpeg:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_node_kick','','踢除节点会话',1,0,100,3,'gb28181:zlm:node:kick','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:kick' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:node:kick' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/nodes')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:kick' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/kick' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_node_manage','','管理节点',1,0,100,3,'gb28181:zlm:node:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:node:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/nodes')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/activate' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/config/test-connection' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/maintenance' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/probe' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_node_view','','查看 ZLM 节点详情',1,0,100,3,'gb28181:zlm:node:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:node:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:node:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:node:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_proxy_manage','','管理代理',1,0,100,3,'gb28181:zlm:proxy:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:proxy:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:proxy:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/proxies')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/pull' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/push' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/push/:key' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:proxy:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_restart','','重启媒体服务',1,0,100,3,'gb28181:zlm:restart','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:restart' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:restart' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/nodes' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/config')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:restart' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/restart' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:restart' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/restart' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_rtp_force_close','','强制关闭 RTP 服务',1,0,100,3,'gb28181:zlm:rtp:force-close','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:force-close' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:rtp:force-close' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/rtp-servers')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:force-close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers/force-close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_rtp_manage','','管理 RTP 服务',1,0,100,3,'gb28181:zlm:rtp:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:rtp:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:rtp:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/ingress' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/rtp-servers')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers/close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:rtp:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_scheduler_manage','','切换调度策略',1,0,100,3,'gb28181:zlm:scheduler:manage','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:scheduler:manage' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:scheduler:manage' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/scheduling' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/scheduler')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:scheduler:manage' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/scheduler' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_session_kick','','踢除会话',1,0,100,3,'gb28181:zlm:session:kick','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:session:kick' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:session:kick' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/sessions')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:session:kick' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/sessions/kick' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_close','','关闭流',1,0,100,3,'gb28181:zlm:stream:close','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:close' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:close' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/streams')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close/batch' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/close/preflight' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_force_close','','强制关闭流',1,0,100,3,'gb28181:zlm:stream:force-close','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:force-close' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:force-close' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/streams')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:force-close' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/force-close' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_preview','','预览与截图',1,0,100,3,'gb28181:zlm:stream:preview','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:preview' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:preview' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent) OR parent_id IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE path IN ('/gb28181/zlm/streams')) AS catalog_legacy_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:preview' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/playback-grant' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:preview' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/snapshot' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_zlm_stream_view','','查看媒体流详情',1,0,100,3,'gb28181:zlm:stream:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:zlm:stream:view' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:zlm:stream:view' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/media/monitoring' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/detail' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:zlm:stream:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/nodes/:id/streams/viewers' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_plugins_example_add','','新增',1,0,100,3,'plugins:example:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='plugins:example:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='plugins:example:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_plugins_example_delete','','删除',1,0,100,3,'plugins:example:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='plugins:example:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='plugins:example:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_plugins_example_edit','','编辑',1,0,100,3,'plugins:example:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='plugins:example:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='plugins:example:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/plugins/example' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='plugins:example:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/plugins/example/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_account_add','','新增',1,0,100,3,'system:account:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:account:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:account:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:account:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_account_delete','','删除',1,0,100,3,'system:account:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:account:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:account:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:account:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_account_edit','','编辑',1,0,100,3,'system:account:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:account:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:account:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/account' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:account:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_bigupload','','大文件上传',1,0,100,3,'system:affix:bigupload','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:bigupload' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:bigupload' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/cancel' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/init' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/merge' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/chunk/upload' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:bigupload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/upload' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_copy','','复制链接',1,0,100,3,'system:affix:copy','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:copy' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:copy' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:copy' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/download/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_delete','','删除文件',1,0,100,3,'system:affix:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_download','','下载文件',1,0,100,3,'system:affix:download','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:download' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:download' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:download' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/download/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_updateName','','修改文件名',1,0,100,3,'system:affix:updateName','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:updateName' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:updateName' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:updateName' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/updateName' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_affix_upload','','文件上传',1,0,100,3,'system:affix:upload','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:affix:upload' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:affix:upload' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/affix' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:affix:upload' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysAffix/upload' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_api_add','','新增',1,0,100,3,'system:api:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:api:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:api:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_api_delete','','删除',1,0,100,3,'system:api:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:api:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:api:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_api_edit','','编辑',1,0,100,3,'system:api:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:api:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:api:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/api' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:api:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_batchInsert','','导入表',1,0,100,3,'system:codegen:batchInsert','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:batchInsert' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:batchInsert' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:batchInsert' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/tables' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:batchInsert' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/batchInsert' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_delete','','删除',1,0,100,3,'system:codegen:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/:id' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_generate','','生成代码文件',1,0,100,3,'system:codegen:generate','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:generate' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:generate' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:generate' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/generate' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_insertmenuandapi','','生成菜单',1,0,100,3,'system:codegen:insertmenuandapi','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:insertmenuandapi' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:insertmenuandapi' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:insertmenuandapi' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/insertmenuandapi' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_preview','','预览',1,0,100,3,'system:codegen:preview','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:preview' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:preview' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:preview' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/codegen/preview' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_refreshFields','','同步数据库',1,0,100,3,'system:codegen:refreshFields','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:refreshFields' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:refreshFields' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:refreshFields' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/refreshFields' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_codegen_update','','配置',1,0,100,3,'system:codegen:update','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:codegen:update' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:codegen:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/codegen' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:codegen:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysGen/update' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_config_update','','修改系统配置',1,0,100,3,'system:config:update','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:config:update' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:config:update' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysconfig' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:config:update' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/config/update' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dict_add','','新增',1,0,100,3,'system:dict:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dict:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dict:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dict:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDict/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dict_delete','','删除',1,0,100,3,'system:dict:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dict:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dict:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dict:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDict/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dict_edit','','编辑',1,0,100,3,'system:dict:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dict:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dict:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dict:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDict/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_add','','新增字典项',1,0,100,3,'system:dictitem:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_delete','','删除字典项',1,0,100,3,'system:dictitem:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_edit','','编辑字典项',1,0,100,3,'system:dictitem:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_dictitem_list','','字典项管理',1,0,100,3,'system:dictitem:list','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:dictitem:list' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:dictitem:list' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/dictionary' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:dictitem:list' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDictItem/getByDictId/:dictId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_division_add','','新增部门',1,0,100,3,'system:division:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:division:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:division:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:division:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_division_delete','','删除部门',1,0,100,3,'system:division:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:division:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:division:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:division:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_division_edit','','编辑部门',1,0,100,3,'system:division:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:division:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:division:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/division' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:division:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_log_delete','','删除',1,0,100,3,'system:log:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:log:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:log:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:log:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysOperationLog/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_log_export','','导出',1,0,100,3,'system:log:export','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:log:export' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:log:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:log:export' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysOperationLog/export' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_clear','','清空登录日志',1,0,100,3,'system:login-log:clear','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:clear' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:clear' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:clear' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/clear' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_delete','','删除登录日志',1,0,100,3,'system:login-log:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_detail','','查看登录日志详情',1,0,100,3,'system:login-log:detail','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:detail' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:detail' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:detail' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_login_log_unlock','','解锁登录账号',1,0,100,3,'system:login-log:unlock','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:login-log:unlock' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:login-log:unlock' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/login-log' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:login-log:unlock' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysLoginLog/unlock' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_add','','新增',1,0,100,3,'system:menu:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_delete','','删除',1,0,100,3,'system:menu:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/batchDelete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_edit','','编辑',1,0,100,3,'system:menu:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_export','','导出',1,0,100,3,'system:menu:export','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:export' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:export' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/export' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_import','','导入',1,0,100,3,'system:menu:import','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:import' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:import' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:import' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/import' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_menu_setMenuApis','','分配权限',1,0,100,3,'system:menu:setMenuApis','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:menu:setMenuApis' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:menu:setMenuApis' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/menu' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:setMenuApis' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysApi/list' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:setMenuApis' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/apis/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:menu:setMenuApis' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/setApis' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_online_user_force_logout','','强制下线',1,0,100,3,'system:online-user:force-logout','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:online-user:force-logout' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:online-user:force-logout' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/online-user' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:online-user:force-logout' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysOnlineUser/forceLogout' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_param_add','','新增参数',1,0,100,3,'system:param:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:param:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:param:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:param:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysParam/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_param_delete','','删除参数',1,0,100,3,'system:param:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:param:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:param:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:param:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysParam/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_param_edit','','编辑参数',1,0,100,3,'system:param:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:param:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:param:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysparam' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:param:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysParam/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_pluginsmanager_export','','导出插件',1,0,100,3,'system:pluginsmanager:export','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:pluginsmanager:export' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:pluginsmanager:export' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:pluginsmanager:export' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/pluginsmanager/export' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_pluginsmanager_import','','导入插件',1,0,100,3,'system:pluginsmanager:import','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:pluginsmanager:import' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:pluginsmanager:import' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:pluginsmanager:import' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/pluginsmanager/import' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_pluginsmanager_uninstall','','插件卸载',1,0,100,3,'system:pluginsmanager:uninstall','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:pluginsmanager:uninstall' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:pluginsmanager:uninstall' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/pluginsmanager' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:pluginsmanager:uninstall' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/pluginsmanager/uninstall' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_add','','新增',1,0,100,3,'system:role:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_addRoleMenu','','分配权限',1,0,100,3,'system:role:addRoleMenu','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:addRoleMenu' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:addRoleMenu' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:addRoleMenu' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/getMenuList' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:addRoleMenu' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/addRoleMenu' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:addRoleMenu' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/getUserPermission/:roleId' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_dataScope','','数据权限',1,0,100,3,'system:role:dataScope','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:dataScope' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:dataScope' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:dataScope' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysDepartment/getDivision' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:dataScope' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/dataScope' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_delete','','删除',1,0,100,3,'system:role:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_role_edit','','编辑',1,0,100,3,'system:role:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:role:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:role:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/role' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:role:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysRole/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobresults_delete','','删除',1,0,100,3,'system:sysjobresults:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobresults:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobresults:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/joblog' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobresults:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobResults/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobresults_list','','查看定时任务日志',1,0,100,3,'system:sysjobresults:list','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobresults:list' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobresults:list' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobresults:list' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobResults/list' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_add','','新增',1,0,100,3,'system:sysjobs:add','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:add' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:add' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:add' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/add' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_delete','','删除',1,0,100,3,'system:sysjobs:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:delete' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:delete' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:delete' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/delete' AND a.method='DELETE' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_edit','','编辑',1,0,100,3,'system:sysjobs:edit','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:edit' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:edit' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/:id' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/edit' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:edit' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/setStatus' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_executeNow','','执行一次',1,0,100,3,'system:sysjobs:executeNow','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:executeNow' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:executeNow' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:executeNow' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/executeNow' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_sysjobs_setStatus','','切换定时任务状态',1,0,100,3,'system:sysjobs:setStatus','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:sysjobs:setStatus' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:sysjobs:setStatus' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/sysjobslist' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:sysjobs:setStatus' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/sysJobs/setStatus' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_userinfo_updateAccount','','修改密码、手机号等',1,0,100,3,'system:userinfo:updateAccount','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:userinfo:updateAccount' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:userinfo:updateAccount' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:userinfo:updateAccount' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/updateAccount' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_userinfo_updateBasicInfo','','修改用户基本信息',1,0,100,3,'system:userinfo:updateBasicInfo','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:userinfo:updateBasicInfo' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:userinfo:updateBasicInfo' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:userinfo:updateBasicInfo' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/updateBasicInfo' AND a.method='PUT' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_system_userinfo_uploadAvatar','','上传个人头像',1,0,100,3,'system:userinfo:uploadAvatar','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='system:userinfo:uploadAvatar' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='system:userinfo:uploadAvatar' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/system/userinfo' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='system:userinfo:uploadAvatar' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/users/uploadAvatar' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM sys_menu_api ma WHERE ma.menu_id=m.id AND ma.api_id=a.id);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_share','','复制或分享播放地址',1,0,100,3,'gb28181:play:share','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:share' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:share' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent),'','Permission_gb28181_play_snapshot','','播放器本地图像截图',1,0,100,3,'gb28181:play:snapshot','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:play:snapshot' AND deleted_at IS NULL);

UPDATE sys_menu SET parent_id=(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) WHERE permission='gb28181:play:snapshot' AND type=3 AND deleted_at IS NULL AND (SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) IS NOT NULL AND parent_id<>(SELECT id FROM (SELECT MIN(id) AS id FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL) AS catalog_parent) AND (parent_id=0 OR parent_id NOT IN (SELECT id FROM (SELECT DISTINCT id FROM sys_menu WHERE deleted_at IS NULL) AS catalog_existing_parent));

-- button-permission-catalog:end

-- guest-readonly:start

-- Generated from guest-permissions.json. Configure guest only; keep other roles and user assignments.

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT m.id,'','Permission_gb28181_play_share','','复制或分享播放地址',1,0,100,3,'gb28181:play:share','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu m WHERE m.path='/gb28181/multi-screen-playback' AND m.type=2 AND m.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu x WHERE x.permission='gb28181:play:share' AND x.deleted_at IS NULL);

INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT m.id,'','Permission_gb28181_play_snapshot','','播放器本地图像截图',1,0,100,3,'gb28181:play:snapshot','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM sys_menu m WHERE m.path='/gb28181/multi-screen-playback' AND m.type=2 AND m.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu x WHERE x.permission='gb28181:play:snapshot' AND x.deleted_at IS NULL);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '视频探针诊断','/api/gb28181/stream-probes/:streamId','POST','按钮权限目录',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/stream-probes/:streamId' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT DISTINCT ma.menu_id,n.id FROM sys_menu_api ma JOIN sys_api o ON o.id=ma.api_id CROSS JOIN sys_api n WHERE o.path='/api/gb28181/play/:deviceId/probe' AND o.method='POST' AND n.path='/api/gb28181/stream-probes/:streamId' AND n.method='POST' AND n.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=ma.menu_id AND x.api_id=n.id);

DELETE FROM sys_menu_api WHERE api_id IN(SELECT id FROM sys_api WHERE path='/api/gb28181/play/:deviceId/probe' AND method='POST');

UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE path='/api/gb28181/play/:deviceId/probe' AND method='POST' AND deleted_at IS NULL;

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT DISTINCT p.ptype,p.v0,'/api/gb28181/stream-probes/:streamId',p.v2,p.v3,p.v4,p.v5 FROM (SELECT DISTINCT ptype,v0,v2,v3,v4,v5 FROM sys_casbin_rule WHERE ptype='p' AND v1='/api/gb28181/play/:deviceId/probe' AND v2='POST') p WHERE NOT EXISTS(SELECT 1 FROM sys_casbin_rule n WHERE n.ptype=p.ptype AND n.v0=p.v0 AND n.v1='/api/gb28181/stream-probes/:streamId' AND n.v2=p.v2 AND n.v3=p.v3);

DELETE FROM sys_casbin_rule WHERE ptype='p' AND v1='/api/gb28181/play/:deviceId/probe' AND v2='POST';

INSERT INTO sys_role_menu(role_id,menu_id) SELECT DISTINCT r.role_id,d.id FROM sys_role_menu r JOIN sys_menu v ON v.id=r.menu_id CROSS JOIN sys_menu d JOIN sys_role sr ON sr.id=r.role_id WHERE v.permission='gb28181:recording:view' AND v.deleted_at IS NULL AND d.permission='gb28181:recording:download' AND d.deleted_at IS NULL AND sr.name<>'游客' AND EXISTS(SELECT 1 FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id WHERE ma.menu_id=v.id AND a.path='/api/gb28181/cloud-recordings/files/:id/downloads') AND NOT EXISTS(SELECT 1 FROM sys_role_menu x WHERE x.role_id=r.role_id AND x.menu_id=d.id);

DELETE FROM sys_menu_api WHERE menu_id IN(SELECT id FROM sys_menu WHERE permission='gb28181:recording:view') AND api_id IN(SELECT id FROM sys_api WHERE (path='/api/gb28181/cloud-recordings/files/:id/downloads' AND method='POST') OR (path='/api/gb28181/cloud-recordings/downloads/:taskId' AND method IN('GET','DELETE')));

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/users/profile','GET','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/users/profile' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.path='/home' AND m.type=2 AND m.deleted_at IS NULL AND a.path='/api/users/profile' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/sysMenu/getRouters','GET','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/sysMenu/getRouters' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.path='/home' AND m.type=2 AND m.deleted_at IS NULL AND a.path='/api/sysMenu/getRouters' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/users/logout','POST','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/users/logout' AND method='POST' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.path='/home' AND m.type=2 AND m.deleted_at IS NULL AND a.path='/api/users/logout' AND a.method='POST' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/dashboard/snapshot','GET','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/dashboard/snapshot' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/dashboard/snapshot' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/zlm/overview','GET','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/zlm/overview' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:home:view' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/zlm/overview' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/service-config/default-playback-protocol','GET','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/default-playback-protocol' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/default-playback-protocol' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/service-config/playback-settings','GET','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/playback-settings' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/playback-settings' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '查看与观看依赖','/api/gb28181/sip/service-config/fixed-address-playback','GET','游客权限依赖',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='/api/gb28181/sip/service-config/fixed-address-playback' AND method='GET' AND deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE m.permission='gb28181:play:start' AND m.type=3 AND m.deleted_at IS NULL AND a.path='/api/gb28181/sip/service-config/fixed-address-playback' AND a.method='GET' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_role(name,sort,status,description,parent_id,data_scope,checked_depts,created_at,updated_at,created_by) SELECT '游客',100,1,'只读游客：允许业务查看、实时观看和录像回放；禁止下载、控制与修改',0,4,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 WHERE NOT EXISTS(SELECT 1 FROM sys_role WHERE name='游客' AND deleted_at IS NULL);

UPDATE sys_role SET description='只读游客：允许业务查看、实时观看和录像回放；禁止下载、控制与修改',parent_id=0,status=1,updated_at=CURRENT_TIMESTAMP WHERE name='游客' AND deleted_at IS NULL AND (COALESCE(description,'')<>'只读游客：允许业务查看、实时观看和录像回放；禁止下载、控制与修改' OR parent_id<>0 OR status<>1);

DELETE FROM sys_role_menu WHERE role_id=(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL) AND menu_id NOT IN(SELECT m.id FROM sys_menu m WHERE m.deleted_at IS NULL AND m.disable=0 AND ((m.type=2 AND m.path IN ('/home','/gb28181/device-mgmt/index','/gb28181/multi-screen-playback','/gb28181/device-record-playback/:channelId','/gb28181/cloud-recordings','/gb28181/alarm-management')) OR (m.type=3 AND m.permission IN ('gb28181:home:view','gb28181:device:view','gb28181:play:start','gb28181:play:monitor','gb28181:device-record:query','gb28181:device-record:play'))));

INSERT INTO sys_role_menu(role_id,menu_id) SELECT (SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL),m.id FROM sys_menu m WHERE m.deleted_at IS NULL AND m.disable=0 AND ((m.type=2 AND m.path IN ('/home','/gb28181/device-mgmt/index','/gb28181/multi-screen-playback','/gb28181/device-record-playback/:channelId','/gb28181/cloud-recordings','/gb28181/alarm-management')) OR (m.type=3 AND m.permission IN ('gb28181:home:view','gb28181:device:view','gb28181:play:start','gb28181:play:monitor','gb28181:device-record:query','gb28181:device-record:play'))) AND NOT EXISTS(SELECT 1 FROM sys_role_menu x WHERE x.role_id=(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL) AND x.menu_id=m.id);

DELETE FROM sys_casbin_rule WHERE v0=CONCAT('role_',(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL)) AND ((ptype='p' AND (v3<>'*' OR NOT ((v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND v2='DELETE') OR (v1='/api/gb28181/alarms' AND v2='GET') OR (v1='/api/gb28181/alarms/:id' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/active' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files/:id' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files/options' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/catalog/tree' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/catalog/tree/:id' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/catalog/tree/:id/children' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/mounts' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/record-query/options' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channel/:id/timeline' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/channels' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/device/:id' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/device/:id/status-events' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/devices' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/directory/tree' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/map/clusters' AND v2='GET') OR (v1='/api/gb28181/device-mgmt/map/markers' AND v2='GET') OR (v1='/api/gb28181/home/drilldown/play' AND v2='GET') OR (v1='/api/gb28181/home/drilldown/sip' AND v2='GET') OR (v1='/api/gb28181/home/drilldown/traffic' AND v2='GET') OR (v1='/api/gb28181/home/layout' AND v2='GET') OR (v1='/api/gb28181/home/summary' AND v2='GET') OR (v1='/api/gb28181/play/:streamId/monitor' AND v2='GET') OR (v1='/api/gb28181/sip/dashboard/snapshot' AND v2='GET') OR (v1='/api/gb28181/sip/service-config/default-playback-protocol' AND v2='GET') OR (v1='/api/gb28181/sip/service-config/fixed-address-playback' AND v2='GET') OR (v1='/api/gb28181/sip/service-config/playback-settings' AND v2='GET') OR (v1='/api/gb28181/zlm/nodes' AND v2='GET') OR (v1='/api/gb28181/zlm/nodes/:id/recordings/runtime/status' AND v2='GET') OR (v1='/api/gb28181/zlm/overview' AND v2='GET') OR (v1='/api/sysMenu/getRouters' AND v2='GET') OR (v1='/api/users/profile' AND v2='GET') OR (v1='/api/gb28181/cloud-recordings/files/:id/access' AND v2='POST') OR (v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions' AND v2='POST') OR (v1='/api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions' AND v2='POST') OR (v1='/api/gb28181/device-mgmt/channel/:id/record-query' AND v2='POST') OR (v1='/api/gb28181/play/:deviceId/:channelId' AND v2='POST') OR (v1='/api/gb28181/play/:deviceId/:channelId/authorization' AND v2='POST') OR (v1='/api/users/logout' AND v2='POST')))) OR ptype='g');

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT DISTINCT 'p',CONCAT('role_',(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL)),a.path,a.method,'*','','' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE rm.role_id=(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL) AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_casbin_rule p WHERE p.ptype='p' AND p.v0=CONCAT('role_',(SELECT MIN(id) FROM sys_role WHERE name='游客' AND deleted_at IS NULL)) AND p.v1=a.path AND p.v2=a.method AND p.v3='*');
-- guest-readonly:end

-- custom-ren: work-recording schema
-- Additive work recording schema. Existing recording history is retained.
CREATE TABLE IF NOT EXISTS gb_work_recording (
 id VARCHAR(36) NOT NULL PRIMARY KEY,
 batch_id VARCHAR(36) NOT NULL DEFAULT '',
 channel_id BIGINT NOT NULL,
 created_by BIGINT NOT NULL,
 request_id VARCHAR(128) NOT NULL,
 state VARCHAR(20) NOT NULL,
 desired_action VARCHAR(20) NOT NULL,
 version BIGINT NOT NULL,
 recorder_claim_version BIGINT NOT NULL DEFAULT 0,
 node_id BIGINT NOT NULL DEFAULT 0,
 v_host VARCHAR(128) NOT NULL DEFAULT '',
 app VARCHAR(64) NOT NULL DEFAULT '',
 stream VARCHAR(64) NOT NULL DEFAULT '',
 recording_root VARCHAR(1024) NOT NULL DEFAULT '',
 generation BIGINT NOT NULL DEFAULT 0,
 started_at TIMESTAMP NULL,
 stopped_at TIMESTAMP NULL,
 last_checked_at TIMESTAMP NULL,
 last_error VARCHAR(500) NOT NULL DEFAULT '',
 file_state VARCHAR(20) NOT NULL DEFAULT 'pending',
 form_state VARCHAR(20) NOT NULL DEFAULT 'draft',
 form_version BIGINT NOT NULL DEFAULT 0,
 schema_version BIGINT NOT NULL DEFAULT 1,
 device_id VARCHAR(20) NOT NULL DEFAULT '',
 form_json TEXT NOT NULL,
 created_at TIMESTAMP NULL,
 updated_at TIMESTAMP NULL,
 CONSTRAINT uk_work_recording_request UNIQUE(created_by, request_id)
);

CREATE TABLE IF NOT EXISTS gb_work_recording_batch (
 id VARCHAR(36) NOT NULL PRIMARY KEY,
 created_by BIGINT NOT NULL,
 request_id VARCHAR(128) NOT NULL,
 state VARCHAR(20) NOT NULL,
 version BIGINT NOT NULL DEFAULT 1,
 form_state VARCHAR(20) NOT NULL DEFAULT 'draft',
 form_version BIGINT NOT NULL DEFAULT 0,
 schema_version BIGINT NOT NULL DEFAULT 1,
 device_id VARCHAR(20) NOT NULL DEFAULT '',
 form_json TEXT NOT NULL,
 last_error VARCHAR(500) NOT NULL DEFAULT '',
 created_at TIMESTAMP NULL,
 updated_at TIMESTAMP NULL,
 CONSTRAINT uk_work_recording_batch_request UNIQUE(created_by, request_id)
);
CREATE INDEX IF NOT EXISTS idx_work_recording_batch ON gb_work_recording(batch_id);

CREATE TABLE IF NOT EXISTS gb_recorder_claim (
 channel_id BIGINT NOT NULL DEFAULT 0,
 node_id BIGINT NOT NULL DEFAULT 0,
 v_host VARCHAR(128) NOT NULL DEFAULT '',
 app VARCHAR(64) NOT NULL DEFAULT '',
 stream VARCHAR(64) NOT NULL DEFAULT '',
 recording_root VARCHAR(1024) NOT NULL DEFAULT '',
 resource_key VARCHAR(64) NOT NULL PRIMARY KEY,
 owner_kind VARCHAR(20) NOT NULL,
 owner_id VARCHAR(128) NOT NULL,
 state VARCHAR(20) NOT NULL,
 version BIGINT NOT NULL,
 generation BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMP NULL,
 updated_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS gb_work_recording_file (
 file_id BIGINT NOT NULL PRIMARY KEY,
 work_recording_id VARCHAR(36) NOT NULL,
 evidence VARCHAR(500) NOT NULL DEFAULT '',
 created_at TIMESTAMP NULL
);

-- custom-ren: work-recording permissions (PostgreSQL)
-- Start and stop are separate buttons; both may read status and job detail.
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL),0),'','Permission_gb28181_work_recording_start','','开启作业录像',1,0,100,3,'gb28181:work-recording:start','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-recording:start' AND deleted_at IS NULL);
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL),0),'','Permission_gb28181_work_recording_stop','','停止作业录像',1,0,100,3,'gb28181:work-recording:stop','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-recording:stop' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT s.title,s.path,s.method,'GB28181 作业录像',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (
 SELECT '开启作业录像' AS title,'/api/gb28181/work-recordings' AS path,'POST' AS method
 UNION ALL SELECT '停止作业录像','/api/gb28181/work-recordings/:id/stop','POST'
 UNION ALL SELECT '查询作业录像状态','/api/gb28181/work-recordings/status','GET'
 UNION ALL SELECT '查询作业录像详情','/api/gb28181/work-recordings/:id','GET'
) s
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=s.path AND a.method=s.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-recording:start' AND m.deleted_at IS NULL
  AND ((a.path='/api/gb28181/work-recordings' AND a.method='POST')
    OR (a.path='/api/gb28181/work-recordings/batches' AND a.method IN ('POST','GET'))
    OR (a.path='/api/gb28181/work-recordings/batches/:batchId' AND a.method='GET')
    OR (a.path IN ('/api/gb28181/work-recordings/status','/api/gb28181/work-recordings/:id') AND a.method='GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-recording:stop' AND m.deleted_at IS NULL
  AND ((a.path='/api/gb28181/work-recordings/:id/stop' AND a.method='POST')
    OR (a.path='/api/gb28181/work-recordings/batches/:batchId/stop' AND a.method='POST')
    OR (a.path='/api/gb28181/work-recordings/batches' AND a.method='GET')
    OR (a.path='/api/gb28181/work-recordings/batches/:batchId' AND a.method='GET')
    OR (a.path IN ('/api/gb28181/work-recordings/status','/api/gb28181/work-recordings/:id') AND a.method='GET'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- custom-ren: work-recording form permissions (PostgreSQL)
-- Start and stop may read the form; only the form permission may save it.
INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/multi-screen-playback' AND type IN (1,2) AND deleted_at IS NULL),0),'','Permission_gb28181_work_recording_form','','编辑作业表单',1,0,100,3,'gb28181:work-recording:form','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-recording:form' AND deleted_at IS NULL);

INSERT INTO sys_role_menu(role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.permission='gb28181:work-recording:form' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by)
SELECT s.title,s.path,s.method,'GB28181 作业录像',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (
 SELECT '我的作业列表' AS title,'/api/gb28181/work-recordings' AS path,'GET' AS method
 UNION ALL SELECT '查询作业台账','/api/gb28181/work-recordings/batches','GET'
 UNION ALL SELECT '查询作业台账详情','/api/gb28181/work-recordings/batches/:batchId','GET'
 UNION ALL SELECT '查询台账表单','/api/gb28181/work-recordings/batches/:batchId/form','GET'
 UNION ALL SELECT '保存台账草稿','/api/gb28181/work-recordings/batches/:batchId/form','PUT'
 UNION ALL SELECT '查询作业表单','/api/gb28181/work-recordings/:id/form','GET'
 UNION ALL SELECT '保存作业草稿','/api/gb28181/work-recordings/:id/form','PUT'
) s
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=s.path AND a.method=s.method AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND m.deleted_at IS NULL
  AND a.method='GET' AND a.path IN ('/api/gb28181/work-recordings','/api/gb28181/work-recordings/:id/form','/api/gb28181/work-recordings/batches','/api/gb28181/work-recordings/batches/:batchId','/api/gb28181/work-recordings/batches/:batchId/form')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);
INSERT INTO sys_menu_api(menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-recording:form' AND m.deleted_at IS NULL
  AND ((a.method='GET' AND a.path IN ('/api/gb28181/work-recordings','/api/gb28181/work-recordings/:id/form','/api/gb28181/work-recordings/batches','/api/gb28181/work-recordings/batches/:batchId','/api/gb28181/work-recordings/batches/:batchId/form'))
    OR (a.method='PUT' AND a.path='/api/gb28181/work-recordings/batches/:batchId/form')
    OR (a.method='PUT' AND a.path='/api/gb28181/work-recordings/:id/form'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form') AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- 作业单菜单与权限（PostgreSQL 12+，幂等）。
-- 作业单是录制的唯一入口：先填作业单、校验通过后才开始录制。
-- 组件路径对应 web/src/views/gb28181/work-orders/index.vue。
-- GB28181 菜单为根级平铺（parent_id = 0），排序接在 cascade(13) 之后。

-- 1) 菜单
INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT 0,'/gb28181/work-orders','gb28181-work-orders','gb28181/work-orders/index','作业单',0,0,14,2,'','lucide:ClipboardList',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE path='/gb28181/work-orders' AND deleted_at IS NULL);

-- 2) 按钮权限
INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/work-orders' AND type=2 AND deleted_at IS NULL),0),'','Permission_gb28181_work_order_create','','新建作业单',1,0,1,3,'gb28181:work-order:create','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-order:create' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/work-orders' AND type=2 AND deleted_at IS NULL),0),'','Permission_gb28181_work_order_stop','','结束作业单录制',1,0,2,3,'gb28181:work-order:stop','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-order:stop' AND deleted_at IS NULL);

INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/work-orders' AND type=2 AND deleted_at IS NULL),0),'','Permission_gb28181_work_order_view','','查看作业单',1,0,3,3,'gb28181:work-order:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-order:view' AND deleted_at IS NULL);

-- 3) 角色绑定（菜单本身 + 三个按钮权限）
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.deleted_at IS NULL
  AND (m.path='/gb28181/work-orders' OR m.permission IN ('gb28181:work-order:create','gb28181:work-order:stop','gb28181:work-order:view'))
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

-- 4) API 权限
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT s.title,s.path,s.method,'GB28181 作业单',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (
 SELECT '新建作业单' AS title,'/api/gb28181/work-orders' AS path,'POST' AS method
 UNION ALL SELECT '作业单列表','/api/gb28181/work-orders','GET'
 UNION ALL SELECT '进行中作业单','/api/gb28181/work-orders/active','GET'
 UNION ALL SELECT '作业单详情','/api/gb28181/work-orders/:id','GET'
 UNION ALL SELECT '结束作业单录制','/api/gb28181/work-orders/:id/stop','POST'
 UNION ALL SELECT '下载作业单录像','/api/gb28181/work-orders/:id/download','GET'
 UNION ALL SELECT '播放作业单录像分片','/api/gb28181/work-orders/:id/files/:fileId','GET'
) s
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=s.path AND a.method=s.method AND a.deleted_at IS NULL);

-- 5) 菜单与 API 的精确绑定
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-order:view' AND m.deleted_at IS NULL
  AND a.method='GET' AND a.path IN ('/api/gb28181/work-orders','/api/gb28181/work-orders/active','/api/gb28181/work-orders/:id','/api/gb28181/work-orders/:id/download','/api/gb28181/work-orders/:id/files/:fileId')
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-order:create' AND m.deleted_at IS NULL
  AND a.method='POST' AND a.path='/api/gb28181/work-orders'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-order:stop' AND m.deleted_at IS NULL
  AND a.method='POST' AND a.path='/api/gb28181/work-orders/:id/stop'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

-- 6) Casbin 规则（同一角色可能通过菜单与按钮命中同一 API，写入前必须去重）
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE (m.path='/gb28181/work-orders' OR m.permission IN ('gb28181:work-order:create','gb28181:work-order:stop','gb28181:work-order:view'))
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- 退役旧的单通道/批次录像权限元数据（PostgreSQL 12+，幂等）。
-- 背景：单通道与批次录像的对外路由（/api/gb28181/work-recordings*）与处理器已被删除，
--       该能力由作业单（/api/gb28181/work-orders）统一接管。
--       但历史迁移写入的菜单 / API / 授权记录仍留在库里，指向已不存在的端点，
--       会在菜单树里留下点不动、且与作业单重复的按钮权限。
-- 本迁移只退役这些孤儿记录，不触碰任何其它权限。
-- 幂等：可重复执行；尚未写入过这些记录的环境执行时为空操作。
-- 注意：sys_casbin_rule / sys_menu_api / sys_role_menu 无 deleted_at 列，只能物理删除；
--       sys_menu / sys_api 走软删（deleted_at）以保留审计痕迹。

-- 1) Casbin 规则
DELETE FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/work-recordings%';

-- 2) 菜单与 API 的绑定（先按 API 命中，再按菜单命中，覆盖两种绑定来源）
DELETE FROM sys_menu_api WHERE api_id IN (
  SELECT id FROM sys_api WHERE path LIKE '/api/gb28181/work-recordings%'
);
DELETE FROM sys_menu_api WHERE menu_id IN (
  SELECT id FROM sys_menu
  WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')
);

-- 3) 角色与菜单的绑定
DELETE FROM sys_role_menu WHERE menu_id IN (
  SELECT id FROM sys_menu
  WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')
);

-- 4) 按钮权限菜单软删
UPDATE sys_menu SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')
  AND deleted_at IS NULL;

-- 5) API 记录软删
UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE path LIKE '/api/gb28181/work-recordings%'
  AND deleted_at IS NULL;

-- 作业单删除权限（PostgreSQL 12+，幂等）。
-- 列表页新增「删除」与「批量删除」两个动作，对应两条接口：
--   DELETE /api/gb28181/work-orders/:id          单条删除
--   POST   /api/gb28181/work-orders/batch-delete 勾选批量删除
-- 删除只允许作用于已结束/失败的作业单，正在录制的由服务端跳过并回报，
-- 所以它是一项独立于「结束录像」的权限，单独授予。

-- 1) 按钮权限（挂在作业单菜单下）
INSERT INTO sys_menu (parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by)
SELECT COALESCE((SELECT MIN(id) FROM sys_menu WHERE path='/gb28181/work-orders' AND type=2 AND deleted_at IS NULL),0),'','Permission_gb28181_work_order_delete','','删除作业单',1,0,4,3,'gb28181:work-order:delete','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1
WHERE NOT EXISTS (SELECT 1 FROM sys_menu WHERE permission='gb28181:work-order:delete' AND deleted_at IS NULL);

-- 2) 角色绑定
INSERT INTO sys_role_menu (role_id,menu_id)
SELECT 1,m.id FROM sys_menu m
WHERE m.permission='gb28181:work-order:delete' AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_role_menu x WHERE x.role_id=1 AND x.menu_id=m.id);

-- 3) API 权限
INSERT INTO sys_api (title,path,method,api_group,created_at,updated_at,created_by)
SELECT s.title,s.path,s.method,'GB28181 作业单',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1 FROM (
 SELECT '删除作业单' AS title,'/api/gb28181/work-orders/:id' AS path,'DELETE' AS method
 UNION ALL SELECT '批量删除作业单','/api/gb28181/work-orders/batch-delete','POST'
) s
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path=s.path AND a.method=s.method AND a.deleted_at IS NULL);

-- 4) 菜单与 API 的精确绑定
INSERT INTO sys_menu_api (menu_id,api_id)
SELECT m.id,a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-order:delete' AND m.deleted_at IS NULL
  AND ((a.method='DELETE' AND a.path='/api/gb28181/work-orders/:id')
    OR (a.method='POST' AND a.path='/api/gb28181/work-orders/batch-delete'))
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

-- 5) Casbin 规则（同一角色可能通过菜单与按钮命中同一 API，写入前必须去重）
INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5)
SELECT DISTINCT 'p','role_'||rm.role_id,a.path,a.method,'*','',''
FROM sys_role_menu rm
JOIN sys_menu m ON m.id=rm.menu_id
JOIN sys_menu_api ma ON ma.menu_id=m.id
JOIN sys_api a ON a.id=ma.api_id
WHERE m.permission='gb28181:work-order:delete'
  AND m.deleted_at IS NULL AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_'||rm.role_id AND c.v1=a.path AND c.v2=a.method AND c.v3='*');

-- 作业单表单历史值（PostgreSQL 12+）。
CREATE TABLE IF NOT EXISTS gb_work_order_form_history (
  id BIGSERIAL PRIMARY KEY,
  field_key VARCHAR(64) NOT NULL,
  value VARCHAR(256) NOT NULL,
  use_count INTEGER NOT NULL DEFAULT 1,
  last_used_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_gb_work_order_form_history_field_value
  ON gb_work_order_form_history (field_key, value);
CREATE INDEX IF NOT EXISTS idx_gb_work_order_form_history_field_used
  ON gb_work_order_form_history (field_key, last_used_at);
CREATE INDEX IF NOT EXISTS idx_gb_work_order_form_history_field_count
  ON gb_work_order_form_history (field_key, use_count);

-- 作业单表单历史值接口权限（PostgreSQL 12+，幂等）。
INSERT INTO sys_api (title, path, method, api_group, created_at, updated_at, created_by)
SELECT '作业单表单历史值', '/api/gb28181/work-orders/form-history', 'GET', 'GB28181 作业单', NOW(), NOW(), 1
WHERE NOT EXISTS (SELECT 1 FROM sys_api a WHERE a.path='/api/gb28181/work-orders/form-history' AND a.method='GET' AND a.deleted_at IS NULL);

INSERT INTO sys_menu_api (menu_id, api_id)
SELECT m.id, a.id FROM sys_menu m JOIN sys_api a ON a.deleted_at IS NULL
WHERE m.permission='gb28181:work-order:view' AND m.deleted_at IS NULL
  AND a.method='GET' AND a.path='/api/gb28181/work-orders/form-history'
  AND NOT EXISTS (SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id);

INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT DISTINCT 'p', 'role_1', a.path, a.method, '*', '', ''
FROM sys_api a
WHERE a.path='/api/gb28181/work-orders/form-history' AND a.method='GET' AND a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule c WHERE c.ptype='p' AND c.v0='role_1' AND c.v1=a.path AND c.v2=a.method AND c.v3='*');
