-- drop-dup-security-menu:start
-- Physically drop the duplicate "GB28181 access security" menu (PostgreSQL, idempotent).
-- Its page gb28181/security/index only re-exports security/preview.vue, so it opens exactly the
-- same view as the in-service menu /security-preview, and the row is already disabled (disable=1).
-- Only this duplicate menu and its bindings are removed; /security-preview (id=140371), its five
-- button permissions, and the security API grants in sys_api / sys_casbin_rule stay untouched.
DELETE FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE path='/gb28181/security');
DELETE FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE path='/gb28181/security');
DELETE FROM sys_menu WHERE path='/gb28181/security';
-- drop-dup-security-menu:end
