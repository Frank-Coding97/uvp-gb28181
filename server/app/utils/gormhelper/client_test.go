package gormhelper

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// dsnTestConfig 是 YmlConfigInterf 的最小桩：只需要 GetString / GetInt。
//
// 上面接口有十来个别的方法，全部返回零值即可 —— getDsn 只用这两个。
type dsnTestConfig struct{ values map[string]interface{} }

func (*dsnTestConfig) ConfigFileChangeListen(...func()) {}
func (c *dsnTestConfig) Get(key string) interface{}     { return c.values[key] }
func (c *dsnTestConfig) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (*dsnTestConfig) GetBool(string) bool                 { return false }
func (c *dsnTestConfig) GetInt(key string) int             { value, _ := c.values[key].(int); return value }
func (*dsnTestConfig) GetInt32(string) int32               { return 0 }
func (*dsnTestConfig) GetInt64(string) int64               { return 0 }
func (*dsnTestConfig) GetFloat64(string) float64           { return 0 }
func (*dsnTestConfig) GetDuration(string) time.Duration    { return 0 }
func (*dsnTestConfig) GetStringSlice(string) []string      { return nil }
func (*dsnTestConfig) GetUintSlice(string) []uint          { return nil }
func (c *dsnTestConfig) Set(key string, value interface{}) { c.values[key] = value }
func (*dsnTestConfig) SaveConfig() error                   { return nil }

func useDSNConfig(t *testing.T, values map[string]interface{}) {
	t.Helper()
	previous := app.ConfigYml
	app.ConfigYml = &dsnTestConfig{values: values}
	t.Cleanup(func() { app.ConfigYml = previous })
}

// 金仓与 PolarDB 复用已有驱动，DSN 生成必须与各自的原生方言**逐字一致** ——
// 一旦分叉，现场就会出现「金仓连不上但 PG 能连」这种无从下手的现象。
func TestGetDsnKingbaseIsPostgresShaped(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{
		"gormv2.kingbase.write.host":     "10.0.0.5",
		"gormv2.kingbase.write.port":     54321,
		"gormv2.kingbase.write.database": "uvp_gb28181",
		"gormv2.kingbase.write.user":     "system",
		"gormv2.kingbase.write.pass":     "Kb@123",
	})

	got := getDsn("kingbase", "write")
	require.Equal(t,
		"host=10.0.0.5 port=54321 dbname=uvp_gb28181 user=system password=Kb@123 sslmode=disable TimeZone=Asia/Shanghai",
		got)
}

func TestGetDsnPolarDBIsMySQLShaped(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{
		"gormv2.polardb.write.host":     "10.0.0.6",
		"gormv2.polardb.write.port":     3306,
		"gormv2.polardb.write.database": "uvp_gb28181",
		"gormv2.polardb.write.user":     "uvp",
		"gormv2.polardb.write.pass":     "Pl@123",
		"gormv2.polardb.write.charset":  "utf8mb4",
	})

	got := getDsn("polardb", "write")
	// multiStatements=true 不能丢：迁移 runner 一次 Exec 整个 .sql 文件。
	// transaction_isolation 也不能丢：MySQL 服务端默认 REPEATABLE-READ，
	// 会在并发写路径上产生间隙锁死锁（见 DefaultMySQLTransactionIsolation）。
	require.Equal(t,
		"uvp:Pl@123@tcp(10.0.0.6:3306)/uvp_gb28181?charset=utf8mb4&parseTime=true&loc=Local&multiStatements=true&transaction_isolation=%27READ-COMMITTED%27",
		got)
}

// TestMySQLDsnCarriesTransactionIsolation 锁住 MySQL DSN 上的隔离级别参数。
//
// 这条不是「字符串里有就行」的形式检查：参数名写错、引号漏了，驱动都会
// 静默不生效（参数只是被当成未知变量拼进 SET，拼出非法 SQL 时报错也可能
// 被连接重试吞掉）。真正的落地验证在 TestMySQLIsolationLevelTakesEffect，
// 那里会开真连接读回 @@transaction_isolation。
//
// ⛔ 默认值必须是 READ-COMMITTED：MySQL 服务端默认 REPEATABLE-READ，
// 而本仓的 check-then-act 写路径在 RR 下会因间隙锁死锁。2026-11-09 万级
// 压测实测死锁 6311 条、通道覆盖率仅 8.4%，就是这一条配置缺失造成的。
func TestMySQLDsnCarriesTransactionIsolation(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{
		"gormv2.mysql.write.host":     "10.0.0.7",
		"gormv2.mysql.write.port":     3306,
		"gormv2.mysql.write.database": "uvp_gb28181",
		"gormv2.mysql.write.user":     "uvp",
		"gormv2.mysql.write.pass":     "My@123",
		"gormv2.mysql.write.charset":  "utf8mb4",
	})

	got := getDsn("mysql", "write")
	require.Contains(t, got, "transaction_isolation=%27READ-COMMITTED%27",
		"未配置时必须默认下发 READ-COMMITTED")
}

// TestMySQLIsolationOverridable 确认配置能覆盖默认值（现场若确有需要回退 RR 的场景）。
func TestMySQLIsolationOverridable(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{
		"gormv2.mysql.write.host":           "10.0.0.7",
		"gormv2.mysql.write.port":           3306,
		"gormv2.mysql.write.database":       "uvp_gb28181",
		"gormv2.mysql.write.user":           "uvp",
		"gormv2.mysql.write.pass":           "My@123",
		"gormv2.mysql.write.charset":        "utf8mb4",
		"gormv2.mysql.transactionisolation": "repeatable-read", // 大小写不敏感
	})

	require.Contains(t, getDsn("mysql", "write"), "transaction_isolation=%27REPEATABLE-READ%27")
}

// TestMySQLIsolationRejectsIllegalValue 确认非法值回落默认，且**绝不会**进入 DSN。
//
// ⛔ 这条是安全断言：隔离级别会被拼进 `SET transaction_isolation = '...'`，
// 驱动不做转义，所以任何不在白名单里的值都必须被丢掉，否则就是注入面。
func TestMySQLIsolationRejectsIllegalValue(t *testing.T) {
	for _, bad := range []string{
		"READ-COMMITTED' ; DROP TABLE sys_users; --",
		"SERIALIZABLE'",
		"bogus",
	} {
		useDSNConfig(t, map[string]interface{}{
			"gormv2.mysql.write.host":           "10.0.0.7",
			"gormv2.mysql.write.port":           3306,
			"gormv2.mysql.write.database":       "uvp_gb28181",
			"gormv2.mysql.write.user":           "uvp",
			"gormv2.mysql.write.pass":           "My@123",
			"gormv2.mysql.write.charset":        "utf8mb4",
			"gormv2.mysql.transactionisolation": bad,
		})

		got := getDsn("mysql", "write")
		require.Contains(t, got, "transaction_isolation=%27READ-COMMITTED%27", "非法值 %q 必须回落默认", bad)
		require.NotContains(t, got, "DROP TABLE", "非法值绝不能进入 DSN")
	}
}

func TestGetDsnDMCarriesSchema(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{
		"gormv2.dm.write.host":   "10.0.0.7",
		"gormv2.dm.write.port":   5236,
		"gormv2.dm.write.user":   "SYSDBA",
		"gormv2.dm.write.pass":   "Dm@123",
		"gormv2.dm.write.schema": "UVP",
	})

	// 密码里的 @ 必须被百分号转义，否则连接串会被截断到错误的 host。
	require.Equal(t, "dm://SYSDBA:Dm%40123@10.0.0.7:5236?schema=UVP", getDsn("dm", "write"))
}

func TestGetDsnDMReadSchemaOverridesSectionSchema(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{
		"gormv2.dm.write.host":  "10.0.0.7",
		"gormv2.dm.write.port":  5236,
		"gormv2.dm.write.user":  "SYSDBA",
		"gormv2.dm.write.pass":  "p",
		"gormv2.dm.read.host":   "10.0.0.8",
		"gormv2.dm.read.port":   5236,
		"gormv2.dm.read.user":   "SYSDBA",
		"gormv2.dm.read.pass":   "p",
		"gormv2.dm.read.schema": "REPORT",
		"gormv2.dm.schema":      "FALLBACK",
	})

	// read 自己配了 schema，就用它，不能被 section 级的值盖掉。
	require.Equal(t, "dm://SYSDBA:p@10.0.0.8:5236?schema=REPORT", getDsn("dm", "read"))
	// write 没配，退到 section 级。
	require.Equal(t, "dm://SYSDBA:p@10.0.0.7:5236?schema=FALLBACK", getDsn("dm", "write"))
}

// ⛔ schema 读不到时**必须**退回用户名，不能留给驱动自己猜。
// 达梦按模式隔离表，落错模式的症状是「建表成功但查不到表」，
// 排查时完全看不出跟连接串有关。
func TestGetDsnDMFallsBackToUserAsSchema(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{
		"gormv2.dm.write.host": "10.0.0.7",
		"gormv2.dm.write.port": 5236,
		"gormv2.dm.write.user": "UVP_OWNER",
		"gormv2.dm.write.pass": "p",
	})

	require.Equal(t, "dm://UVP_OWNER:p@10.0.0.7:5236?schema=UVP_OWNER", getDsn("dm", "write"))
}

// 未知类型必须返回空串，由 getDbDialector 报「驱动不存在」；
// 不能返回一个看起来能用的 DSN，那会让错误延迟到第一次查询才爆。
func TestGetDsnUnknownTypeReturnsEmpty(t *testing.T) {
	useDSNConfig(t, map[string]interface{}{})
	require.Equal(t, "", getDsn("oracle", "write"))
}
