-- Restore the previous dashboard house icon (SQL Server 2017+).
UPDATE sys_menu
SET icon = 'lucide:House',
    updated_at = CURRENT_TIMESTAMP
WHERE path = '/home'
  AND deleted_at IS NULL
  AND icon = 'lucide:Gauge';
