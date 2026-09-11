IF EXISTS (SELECT TOP 1 1 FROM [gb_custom_group_device])
   OR EXISTS (SELECT TOP 1 1 FROM [gb_custom_group])
  THROW 51000, 'custom group tables are not empty', 1;
DROP TABLE IF EXISTS [gb_custom_group_device];
DROP TABLE IF EXISTS [gb_custom_group];
