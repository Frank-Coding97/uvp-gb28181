// Package migrationsfs 将增量迁移 SQL 打包进二进制,
// 部署时 backend 启动即自动执行未应用的迁移(见 app/gb28181/migration 包)。
package migrationsfs

import "embed"

//go:embed migrations/*.sql
var FS embed.FS
