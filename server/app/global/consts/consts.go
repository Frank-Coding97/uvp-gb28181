package consts

import "strings"

// ContextKey 定义context key类型，避免使用内置string类型导致的冲突
type ContextKey string

const (
	BindContextKeyName            = "userToken"          // token解析值绑定上下文键名
	BindContextKey     ContextKey = "userToken"          // 标准 context 使用的认证键
	ConfigFilePath                = "/config/config.yml" // 配置文件路径
	//服务器代码发生错误
	ServerOccurredErrorCode int    = -500100
	ServerOccurredErrorMsg  string = "服务器内部发生代码执行错误,请联系开发者排查错误日志"
	// 数据库类型
	DbTypeMySql      = "mysql"
	DbTypeSqlServer  = "sqlserver"
	DbTypePostgreSql = "postgresql"
	// DbTypeSqlite 是**绿色安装包默认库**：单文件、零外部依赖，装完即用。
	// 用纯 Go 驱动（modernc.org/sqlite），不需CGO，因此可交叉编译进静态包。
	DbTypeSqlite   = "sqlite"
	RequestAborted = "request_aborted"
	// 国产化数据库。⛔ 这三个**不是**「第五、六、七种 SQL 方言」，而是挂在已有
	// 方言族上的部署形态 —— 决定成本的是它跟谁同协议，不是它叫什么名字：
	//
	//	kingbase → 复用 postgres 驱动与整批 -postgresql.sql 迁移集（同名方言）
	//	polardb  → 复用 mysql    驱动与整批 mysql 迁移集（同名方言）
	//	dm       → 达梦自有协议，**唯一**需要独立 Dialect 与独立迁移集的
	//
	// 前三者加类型只需要注册配置名与 DSN；只有 dm 会牵动 migration 包。
	DbTypeKingbase = "kingbase"
	DbTypePolarDB  = "polardb"
	DbTypeDM       = "dm"
	// 上传类型
	UploadTypeLocal = "local"
	UploadTypeQiniu = "qiniu"
	// 上传文件类型
	UploadFileTypeImage    = "image"
	UploadFileTypeVideo    = "video"
	UploadFileTypeAudio    = "audio"
	UploadFileTypeDocument = "document"
	UploadFileTypeOther    = "other"
)

// NormalizeDbType 把配置里可能出现的写法收敛成上面的规范名，未知返回空串。
//
// ⛔ 为什么必须有这一个：`gormv2.usedbtype` 的取值原本散落在至少 6 处
// switch 里各自硬编码，并且**已经漂移过** —— app.DB() 只认 "postgresql"，
// 而 handler/deviceinfo.go 自己额外认 "postgres"。于是配成 "postgres" 时，
// app.DB() 会静默落到 default 分支拿到 MySQL 连接，deviceinfo 却拿 PG，
// 同一个进程读写两个库。收敛成一个函数后，新增库只改这里。
func NormalizeDbType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DbTypeMySql, "mariadb", "tidb":
		return DbTypeMySql
	case DbTypePostgreSql, "postgres", "postgre":
		return DbTypePostgreSql
	case DbTypeSqlServer, "mssql":
		return DbTypeSqlServer
	case DbTypeSqlite:
		return DbTypeSqlite
	case DbTypeKingbase, "kingbasees", "kes":
		return DbTypeKingbase
	case DbTypePolarDB:
		return DbTypePolarDB
	case DbTypeDM, "dameng", "dm8":
		return DbTypeDM
	default:
		return ""
	}
}
