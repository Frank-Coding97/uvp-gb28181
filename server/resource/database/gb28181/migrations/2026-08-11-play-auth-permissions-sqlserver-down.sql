-- Safe non-destructive rollback for SQL Server.
-- The up migration may reuse existing /api/gb28181/sip/service-config/play-auth,
-- /api/gb28181/play/:deviceId/:channelId and
-- /api/gb28181/play/:deviceId/:channelId/authorization records. Without ownership
-- metadata, removing those records or the gb28181:play:start relationships could
-- destroy pre-existing role permissions. Keep the additive metadata, and never
-- restore the legacy SIP-config authorization mapping removed by the up migration.
SELECT 1;
