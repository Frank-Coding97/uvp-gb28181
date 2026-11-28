-- Preserve complete GB/T 28181-2022 ParentID values without splitting or truncation.
ALTER TABLE [gb_channel] ALTER COLUMN [parent_id] NVARCHAR(512) NOT NULL;
