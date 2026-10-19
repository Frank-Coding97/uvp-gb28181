// Package migrationsfs 将当前管理员初始密码状态迁移打包进服务端二进制。
package migrationsfs

import "embed"

// 其它历史 SQL 未打包；该范围只包含当前发布需要的新增迁移。
//
//go:embed migrations/2026-10-08-initial-admin-password-change*.sql migrations/2026-10-08-sip-platform-hook-ip*.sql migrations/2026-10-08-sip-platform-stream-ip*.sql migrations/2026-10-08-meta-node-hook-ip*.sql migrations/2026-10-09-meta-node-receive-mode*.sql migrations/2026-10-09-job-result-summary*.sql migrations/2026-10-11-openapi-device-capture-session*.sql migrations/2026-10-11-openapi-device-event-subscription*.sql migrations/2026-10-12-catalog-node-source-device*.sql migrations/2026-10-13-channel-parent-ids*.sql migrations/2026-10-13-cascade-business-group*.sql migrations/2026-10-13-cascade-auto-push-catalog*.sql migrations/2026-10-13-cascade-recording-enabled*.sql migrations/2026-10-13-cascade-rtcp-keepalive*.sql migrations/2026-10-13-cascade-publish-group-permission*.sql migrations/2026-10-16-service-upgrade-permission*.sql migrations/2026-10-16-service-upgrade-migration-plan*.sql migrations/2026-10-18-service-upgrade-menu*.sql migrations/2026-10-18-service-upgrade-async-import-permission*.sql migrations/2026-10-18-https-management*.sql migrations/2026-10-18-main-menu-order*.sql migrations/2026-10-18-menu-icon-refresh*.sql migrations/2026-10-18-hide-plugin-menus*.sql migrations/2026-10-18-system-menu-order*.sql
var FS embed.FS
