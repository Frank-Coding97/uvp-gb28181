package migration

import (
	"fmt"

	"gorm.io/gorm"
)

// Down 手动回滚单个迁移:执行 <up>-down.sql 并从版本表删除记录。
// 运维入口,由 -migrate-down 标志触发;删除记录后下次启动 Up 会重新
// 应用该迁移(手动回滚语义,文档已注明)。
func Down(db *gorm.DB, d Dialect, upFileName string) error {
	store := NewStore(db)
	exec := &dbExecutor{db: db}
	return downWith(store, exec, &embedSource{dialect: d}, d, upFileName)
}

// downWith 是 Down 的纯依赖版本,便于 fake 注入测试。
func downWith(store versionStore, exec migrationExecutor, src migrationSource, d Dialect, upFileName string) error {
	downName := DownFileName(upFileName)
	sqlText, err := src.ReadSQL(downName)
	if err != nil {
		return fmt.Errorf("down 迁移文件 %s 不存在(方言 %s): %w", downName, d, err)
	}
	if err := exec.ExecSQL(sqlText); err != nil {
		return fmt.Errorf("down 迁移 %s 执行失败: %w\nSQL: %s", downName, err, sqlText)
	}
	if err := store.DeleteApplied(upFileName); err != nil {
		return fmt.Errorf("删除版本记录 %s 失败: %w", upFileName, err)
	}
	return nil
}
