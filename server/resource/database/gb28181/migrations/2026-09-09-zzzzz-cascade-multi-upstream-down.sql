ALTER TABLE gb_cascade_platform ADD UNIQUE INDEX uk_cascade_platform_local_identity (local_device_id, local_domain), DROP INDEX uk_cascade_platform_connection;
