package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/consts"
)

// metadataSQLDialect 决定代码生成器/插件管理用哪一族元数据 SQL。
//
// ⛔ 金仓与 PolarDB 必须映射回 postgres / mysql：它们的系统目录与原生库
// 完全一致，复制一份 case 只会让两份 SQL 慢慢漂移。映射写错的症状是
// 「生成出来的表结构与实际库不符」，而不是报错。
func TestMetadataSQLDialect(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"mysql", consts.DbTypeMySql},
		{"postgresql", consts.DbTypePostgreSql},
		{"postgres", consts.DbTypePostgreSql},
		{"sqlserver", consts.DbTypeSqlServer},
		{"sqlite", consts.DbTypeSqlite},

		// 复用关系
		{"kingbase", consts.DbTypePostgreSql},
		{"KingbaseES", consts.DbTypePostgreSql},
		{"polardb", consts.DbTypeMySql},

		// 达梦没有对应族，必须落到一个谁也匹配不上的值，
		// 由各函数的 default 分支报错，而不是拿 PG/MySQL 的 SQL 去猜。
		{"dm", "unsupported"},
		{"DM8", "unsupported"},

		// 未知取值原样透出，交给调用方报错
		{"oracle", "oracle"},
	} {
		require.Equal(t, tc.want, metadataSQLDialect(tc.raw), "输入 %q", tc.raw)
	}
}
