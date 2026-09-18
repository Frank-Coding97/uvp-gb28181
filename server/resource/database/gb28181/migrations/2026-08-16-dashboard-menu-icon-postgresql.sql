-- Use a gauge icon for the dashboard menu entry (PostgreSQL 12+).
UPDATE sys_menu
SET svg_icon = '',
    icon = 'lucide:Gauge',
    updated_at = CURRENT_TIMESTAMP
WHERE path = '/home'
  AND deleted_at IS NULL
  AND (svg_icon <> '' OR icon <> 'lucide:Gauge');
