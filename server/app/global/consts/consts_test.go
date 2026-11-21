package consts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// NormalizeDbType 是数据库类型别名的**唯一真源**。
//
// ⛔ 这条测试防的是一类已经发生过的故障：app.DB() 只认 "postgresql"，
// 而 handler 那边自己另写了一份 switch 额外认 "postgres"，于是同一份配置
// 在两个模块被解析成不同的库（app.DB 回落 MySQL），表现为「数据一部分写进
// MySQL、一部分写进 PG」。收敛成一个函数后，这里红就代表别名口径变了。
func TestNormalizeDbType(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		// 已有四种：别名与大小写、前后空格都要收敛
		{"mysql", DbTypeMySql},
		{" MySQL ", DbTypeMySql},
		{"mariadb", DbTypeMySql},
		{"tidb", DbTypeMySql},
		{"postgresql", DbTypePostgreSql},
		{"postgres", DbTypePostgreSql},
		{"postgre", DbTypePostgreSql},
		{"POSTGRES", DbTypePostgreSql},
		{"sqlserver", DbTypeSqlServer},
		{"mssql", DbTypeSqlServer},
		{"sqlite", DbTypeSqlite},

		// 国产化三种
		{"kingbase", DbTypeKingbase},
		{"KingbaseES", DbTypeKingbase},
		{"kes", DbTypeKingbase},
		{"polardb", DbTypePolarDB},
		{"PolarDB", DbTypePolarDB},
		{"dm", DbTypeDM},
		{"DM8", DbTypeDM},
		{"dameng", DbTypeDM},

		// 未配置与未知取值：一律返回空串，由调用方决定兜底语义
		{"", ""},
		{"   ", ""},
		{"oracle", ""},
		{"kingbase8x", ""},
	} {
		require.Equal(t, tc.want, NormalizeDbType(tc.raw), "输入 %q", tc.raw)
	}
}

// 三个国产库的类型名必须与配置节名一致 —— getDsn 用
// "gormv2." + sqlType + ".write.host" 拼配置键，名字对不上就读不到连接参数，
// 症状是「配置明明写了却连 127.0.0.1」。
func TestDomesticDbTypeMatchesConfigSectionName(t *testing.T) {
	require.Equal(t, "kingbase", DbTypeKingbase)
	require.Equal(t, "polardb", DbTypePolarDB)
	require.Equal(t, "dm", DbTypeDM)
}
