-- Refuse to discard current media ownership while any live state is present.
IF OBJECT_ID(N'gb_channel', N'U') IS NOT NULL
   AND COL_LENGTH(N'gb_channel', N'current_ssrc') IS NOT NULL
BEGIN
  IF EXISTS (SELECT 1 FROM [gb_channel] WHERE [stream_id] <> N'' OR [current_ssrc] <> N'')
    THROW 50001, 'current_ssrc rollback blocked: active stream state exists', 1;

  DECLARE @default_constraint SYSNAME;
  SELECT @default_constraint = dc.[name]
  FROM sys.default_constraints AS dc
  INNER JOIN sys.columns AS c
    ON c.[default_object_id] = dc.[object_id]
  WHERE dc.[parent_object_id] = OBJECT_ID(N'gb_channel')
    AND c.[name] = N'current_ssrc';

  IF @default_constraint IS NOT NULL
    EXEC(N'ALTER TABLE [gb_channel] DROP CONSTRAINT [' + @default_constraint + N']');

  ALTER TABLE [gb_channel] DROP COLUMN [current_ssrc];
END;
