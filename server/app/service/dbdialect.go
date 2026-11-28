package service

import (
	"uvplatform.com/uvp-gb28181/app/global/consts"
)

// metadataSQLDialect 把「部署形态」收敛成「元数据 SQL 方言族」。
//
// 代码生成器与插件管理读的是系统目录（information_schema / pg_catalog /
// sys.*）以及建删表语句，这类 SQL 只按**族**分，不按厂商分：
//
//	kingbase → 与 postgresql 完全同族（pg_catalog + information_schema 双轨）
//	polardb  → 与 mysql      完全同族（SHOW DATABASES + INFORMATION_SCHEMA）
//
// 所以这里只做映射，不复制 case —— 复制出来的两份 SQL 迟早会漂移。
//
// ⛔ 达梦返回 "unsupported"：DM8 的系统视图是 USER_TAB_COLUMNS 那一路
// （Oracle 风格），与 PG / MySQL 都不同，没有实机无法验证。这个后台功能
// 宁可明确不可用（落到各函数的 default 分支报错），也不能拿 PG/MySQL 的
// 元数据 SQL 去猜，否则会生成语义错误的代码。
// 有实机后：在这里加一个 case 返回 consts.DbTypeDM，再到三个 switch 里
// 补上 DM 的分支即可。
//
// ⚠️ 空串或未知取值原样返回，交给调用方的 default 分支报错 ——
// 不要静默当 MySQL，那会在未知库上执行 SHOW DATABASES 并返回空列表。
func metadataSQLDialect(dbType string) string {
	normalized := consts.NormalizeDbType(dbType)
	switch normalized {
	case "":
		return dbType
	case consts.DbTypeKingbase:
		return consts.DbTypePostgreSql
	case consts.DbTypePolarDB:
		return consts.DbTypeMySql
	case consts.DbTypeDM:
		return "unsupported"
	default:
		return normalized
	}
}
