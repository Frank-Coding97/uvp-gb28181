//go:build ignore

// 删除"SIP 日志新"菜单及其角色关联
// 用法: go run remove_sip_log_new_menu.go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	dsn := os.Getenv("UVP_DATABASE_DSN")
	if dsn == "" {
		log.Fatal("UVP_DATABASE_DSN 环境变量未设置")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("数据库 Ping 失败: %v", err)
	}

	fmt.Println("✓ 数据库连接成功")

	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("开启事务失败: %v", err)
	}

	// 1. 删除"SIP 日志新"菜单的角色关联
	result1, err := tx.Exec("DELETE FROM sys_role_menu WHERE menu_id = 140363")
	if err != nil {
		tx.Rollback()
		log.Fatalf("删除角色菜单关联失败: %v", err)
	}
	affected1, _ := result1.RowsAffected()
	fmt.Printf("✓ 删除角色菜单关联: %d 行\n", affected1)

	// 2. 软删除"SIP 日志新"菜单
	result2, err := tx.Exec("UPDATE sys_menu SET deleted_at = NOW(), updated_at = NOW() WHERE id = 140363")
	if err != nil {
		tx.Rollback()
		log.Fatalf("删除菜单失败: %v", err)
	}
	affected2, _ := result2.RowsAffected()
	fmt.Printf("✓ 删除 SIP 日志新菜单: %d 行\n", affected2)

	if err := tx.Commit(); err != nil {
		log.Fatalf("提交事务失败: %v", err)
	}

	fmt.Println("\n✓ 菜单删除完成!")
	fmt.Println("  - SIP 日志新菜单已删除")
	fmt.Println("  - 相关角色关联已清理")
}
