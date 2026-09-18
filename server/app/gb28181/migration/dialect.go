package migration

import (
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
	DialectUnknown   Dialect = ""
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
	default:
		return DialectUnknown
	}
}
