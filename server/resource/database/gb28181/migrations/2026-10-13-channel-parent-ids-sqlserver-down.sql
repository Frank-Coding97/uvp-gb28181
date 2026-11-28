-- SQL Server rejects narrowing when any stored ParentID exceeds 20 characters.
ALTER TABLE [gb_channel] ALTER COLUMN [parent_id] NVARCHAR(20) NOT NULL;
