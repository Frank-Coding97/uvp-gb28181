ALTER TABLE gb_cascade_platform DROP CONSTRAINT uk_cascade_platform_local_identity;
ALTER TABLE gb_cascade_platform ADD CONSTRAINT uk_cascade_platform_connection UNIQUE (local_device_id, local_domain, upstream_server_id, host, port, transport);
