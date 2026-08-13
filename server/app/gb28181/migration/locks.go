package migration

import (
	"fmt"

	"gorm.io/gorm"
)

const (
	lockName = "uvp_gb28181_migration"
	// lockKey PG advisory lock 的键,项目内唯一即可。
	lockKey = 861357328
)

// dbLocker 基于数据库 session 级锁实现多实例迁移互斥。
// 注:锁是 session(连接)级,长连接归还池后锁仍持有,迁移完成后
// 进程内首个连接会一直带锁到连接关闭——单进程启动场景无影响,
// 多实例并发部署时由锁超时兜底。
type dbLocker struct {
	db *gorm.DB
	d  Dialect
}

func newDBLocker(db *gorm.DB, d Dialect) *dbLocker {
	return &dbLocker{db: db, d: d}
}

// acquireSQL 各方言取锁 SQL(测试断言用)。
func acquireSQL(d Dialect) string {
	switch d {
	case DialectPostgres:
		return fmt.Sprintf("SELECT pg_advisory_lock(%d)", lockKey)
	case DialectSQLServer:
		return fmt.Sprintf("EXEC sp_getapplock @Resource = '%s', @LockMode = 'Exclusive', @LockOwner = 'Session', @LockTimeout = 30000", lockName)
	default: // DialectMySQL
		return fmt.Sprintf("SELECT GET_LOCK('%s', 30)", lockName)
	}
}

// releaseSQL 各方言放锁 SQL。
func releaseSQL(d Dialect) string {
	switch d {
	case DialectPostgres:
		return fmt.Sprintf("SELECT pg_advisory_unlock(%d)", lockKey)
	case DialectSQLServer:
		return fmt.Sprintf("EXEC sp_releaseapplock @Resource = '%s', @LockOwner = 'Session'", lockName)
	default:
		return fmt.Sprintf("SELECT RELEASE_LOCK('%s')", lockName)
	}
}

// Acquire 取锁。MySQL/SQLServer 检查返回值,非成功即报错。
func (l *dbLocker) Acquire() error {
	switch l.d {
	case DialectPostgres:
		// pg_advisory_lock 返回 void,无结果集,Exec 直接执行。
		return l.db.Exec(acquireSQL(l.d)).Error
	case DialectSQLServer:
		var rc int
		row, err := l.db.Raw(acquireSQL(l.d)).Rows()
		if err != nil {
			return err
		}
		defer row.Close()
		if !row.Next() {
			return fmt.Errorf("sp_getapplock 无返回码")
		}
		if err := row.Scan(&rc); err != nil {
			return err
		}
		if rc != 0 {
			return fmt.Errorf("sp_getapplock 返回 %d(0=成功,1=超时,<0=错误)", rc)
		}
		return nil
	default: // DialectMySQL
		var got int
		if err := l.db.Raw(acquireSQL(l.d)).Scan(&got).Error; err != nil {
			return err
		}
		if got != 1 {
			return fmt.Errorf("GET_LOCK 未获取到锁(超时或被其他实例持有)")
		}
		return nil
	}
}

// Release 放锁,失败则透传(锁随连接关闭自动释放,兜底)。
func (l *dbLocker) Release() error {
	return l.db.Exec(releaseSQL(l.d)).Error
}
