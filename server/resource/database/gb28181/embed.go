// Package migrationsfs 历史迁移已清理，保留空 embed.FS 以维持代码兼容性。
// 新环境使用三方言全量初始化脚本 (uvp-gb28181.sql / postgresql_converted.sql / sqlserver_converted.sql)。
package migrationsfs

import "embed"

//go:embed embed.go
var FS embed.FS
