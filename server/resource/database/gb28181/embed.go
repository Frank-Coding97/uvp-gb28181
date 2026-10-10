// Package migrationsfs 将当前管理员初始密码状态迁移打包进服务端二进制。
package migrationsfs

import "embed"

// 其它历史 SQL 未打包；该范围只包含当前发布需要的新增迁移。
//
//go:embed migrations/2026-10-08-initial-admin-password-change*.sql migrations/2026-10-08-sip-platform-hook-ip*.sql migrations/2026-10-08-sip-platform-stream-ip*.sql migrations/2026-10-08-meta-node-hook-ip*.sql migrations/2026-10-09-meta-node-receive-mode*.sql migrations/2026-10-09-job-result-summary*.sql
var FS embed.FS
