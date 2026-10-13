package migration

import (
	"fmt"
	"sort"

	"gorm.io/gorm"
)

// runUp 包级注入点,bootstrap 启动链路经 RunMigrations 调用;测试可替换为 fake。
var runUp = Up

// RunMigrations 对已初始化的数据库连接按名字执行迁移,返回第一个错误。
// nil 连接被忽略;方言未知的连接跳过(不支持的库不做迁移)。
func RunMigrations(dbs map[string]*gorm.DB) error {
	names := make([]string, 0, len(dbs))
	for name, db := range dbs {
		if db != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		db := dbs[name]
		d := DialectOf(db.Dialector)
		if d == DialectUnknown {
			continue
		}
		if err := runUp(db, d); err != nil {
			return fmt.Errorf("数据库 %s 迁移失败: %w", name, err)
		}
	}
	return nil
}
