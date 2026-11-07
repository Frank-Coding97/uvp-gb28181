package migration

import (
	"github.com/glebarez/sqlite"
	dameng "github.com/godoes/gorm-dameng"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// Dialect 标识迁移 SQL 的目标数据库方言,决定选用哪一组迁移文件。
type Dialect string

const (
	DialectMySQL     Dialect = "mysql"
	DialectPostgres  Dialect = "postgres"
	DialectSQLServer Dialect = "sqlserver"
	// ⛔ SQLite 必须有独立方言，不能借用 MySQL。原先只有前三个，
	//   绿色安装包默认的 SQLite 下 DialectOf 会返回 DialectUnknown，
	//   于是「哪些迁移文件适用」「数据库身份怎么读」都无从判断 ——
	//   症状是 -migrate-up 直接失败。
	DialectSQLite  Dialect = "sqlite"
	DialectUnknown Dialect = ""

	// DialectDM 是达梦 DM8。
	//
	// ⛔ 只有达梦需要这一项。人大金仓与阿里 PolarDB **刻意不设独立方言**：
	//   它们分别由 postgres.Open / mysql.Open 建连，DialectOf 会自然判成
	//   DialectPostgres / DialectMySQL，于是整批 -postgresql.sql / mysql
	//   迁移集直接复用。给它们各加一套方言只会多出两批内容完全相同的
	//   SQL 文件，将来必然漂移。
	DialectDM Dialect = "dm"
)

// DialectOf 从 GORM dialector 判定数据库方言,未知方言返回 DialectUnknown。
func DialectOf(d gorm.Dialector) Dialect {
	switch d.(type) {
	case *mysql.Dialector, mysql.Dialector:
		return DialectMySQL
	case *postgres.Dialector, postgres.Dialector:
		return DialectPostgres
	case *sqlserver.Dialector, sqlserver.Dialector:
		return DialectSQLServer
	case sqlite.Dialector, *sqlite.Dialector:
		// ⛔ 两种形态都要列：sqlite.Open(...) 的返回值是**值**，
		//   而 &sqlite.Dialector{} 是**指针**。只写一个的话另一种静默判成 Unknown
		//   —— 而调用方拿到 Unknown 后的行为是"迁移文件找不到"，报错与根因无关。
		//   （现有三个方言都是"值+指针"都写，这里跟着同一形状。）
		return DialectSQLite
	case *dameng.Dialector, dameng.Dialector:
		// 同理两种形态都列。dameng.Open() 返回的是 *Dialector，
		// 但 Dialector 的方法集是值接收者，与 sqlite 一样的坑。
		return DialectDM
	default:
		return DialectUnknown
	}
}
