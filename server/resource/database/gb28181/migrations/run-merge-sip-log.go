//go:build ignore

// 临时迁移执行器 - 合并 SIP 日志菜单
// 用法: go run run-merge-sip-log.go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// 连接数据库
	dsn := os.Getenv("UVP_DATABASE_DSN")
	if dsn == "" {
		log.Fatal("UVP_DATABASE_DSN 环境变量未设置")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer db.Close()

	// 测试连接
	if err := db.Ping(); err != nil {
		log.Fatalf("数据库 Ping 失败: %v", err)
	}

	fmt.Println("✓ 数据库连接成功")

	// 开始事务
	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("开启事务失败: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Fatalf("执行失败,已回滚: %v", r)
		}
	}()

	now := time.Now()

	// 1. 更新旧的"SIP 日志"菜单,改用新版 component
	result1, err := tx.Exec(`
		UPDATE sys_menu
		SET component = 'gb28181/sip-log-v2/index',
		    updated_at = ?
		WHERE path = '/gb28181/sip-traces'
		  AND deleted_at IS NULL
	`, now)
	if err != nil {
		tx.Rollback()
		log.Fatalf("更新 SIP 日志菜单失败: %v", err)
	}
	affected1, _ := result1.RowsAffected()
	fmt.Printf("✓ 更新 SIP 日志菜单: %d 行\n", affected1)

	// 2. 软删除"SIP 日志新"菜单
	result2, err := tx.Exec(`
		UPDATE sys_menu
		SET deleted_at = ?,
		    updated_at = ?
		WHERE path = '/gb28181/sip-traces-v2'
		  AND deleted_at IS NULL
	`, now, now)
	if err != nil {
		tx.Rollback()
		log.Fatalf("删除 SIP 日志新菜单失败: %v", err)
	}
	affected2, _ := result2.RowsAffected()
	fmt.Printf("✓ 删除 SIP 日志新菜单: %d 行\n", affected2)

	// 3. 清理"SIP 日志新"菜单的角色关联
	result3, err := tx.Exec(`
		DELETE FROM sys_role_menu
		WHERE menu_id IN (
		  SELECT id FROM sys_menu
		  WHERE path = '/gb28181/sip-traces-v2'
		)
	`)
	if err != nil {
		tx.Rollback()
		log.Fatalf("清理角色菜单关联失败: %v", err)
	}
	affected3, _ := result3.RowsAffected()
	fmt.Printf("✓ 清理角色菜单关联: %d 行\n", affected3)

	// 提交事务
	if err := tx.Commit(); err != nil {
		log.Fatalf("提交事务失败: %v", err)
	}

	fmt.Println("\n✓ 菜单合并完成!")
	fmt.Println("  - SIP 日志菜单已更新为新版组件")
	fmt.Println("  - SIP 日志新菜单已删除")

	os.Exit(0)
}
