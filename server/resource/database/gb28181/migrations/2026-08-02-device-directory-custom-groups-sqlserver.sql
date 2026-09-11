-- Device custom groups. SQL Server 2017+, repeatable.
IF OBJECT_ID(N'gb_custom_group', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_custom_group] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [owner_dept_id] BIGINT NOT NULL,
    [parent_id] BIGINT NOT NULL CONSTRAINT [df_custom_group_parent] DEFAULT 0,
    [path] NVARCHAR(1024) NOT NULL,
    [depth] SMALLINT NOT NULL CONSTRAINT [df_custom_group_depth] DEFAULT 0,
    [name] NVARCHAR(64) NOT NULL,
    [created_by] BIGINT NOT NULL,
    [created_at] DATETIME2(3) NOT NULL,
    [updated_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_gb_custom_group] PRIMARY KEY ([id]),
    CONSTRAINT [uk_custom_group_sibling_name] UNIQUE ([owner_dept_id], [parent_id], [name])
  );
  CREATE INDEX [idx_custom_group_parent] ON [gb_custom_group] ([parent_id]);
  CREATE INDEX [idx_custom_group_dept_path] ON [gb_custom_group] ([owner_dept_id], [path]);
END;

IF OBJECT_ID(N'gb_custom_group_device', N'U') IS NULL
BEGIN
  CREATE TABLE [gb_custom_group_device] (
    [id] BIGINT IDENTITY(1,1) NOT NULL,
    [group_id] BIGINT NOT NULL,
    [device_id] BIGINT NOT NULL,
    [created_by] BIGINT NOT NULL,
    [created_at] DATETIME2(3) NOT NULL,
    CONSTRAINT [pk_gb_custom_group_device] PRIMARY KEY ([id]),
    CONSTRAINT [uk_custom_group_device] UNIQUE ([group_id], [device_id])
  );
  CREATE INDEX [idx_custom_group_device_group] ON [gb_custom_group_device] ([group_id]);
  CREATE INDEX [idx_custom_group_device_device] ON [gb_custom_group_device] ([device_id]);
END;
