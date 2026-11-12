package main

import (
	"fmt"
	"strings"
)

// runConfigGet 读 config.yml 里某段（可嵌套）下某个键的标量值，剥引号与行尾注释。
//
//	uvp-setup config-get --yml config/config.yml --section demoaccount --key defaultpassword
//
// 供 ctl 脚本取三处值：出厂口令（启动摘要 / reset-password）、redis.indexdb。
//
// ⛔ 找不到就**非零退出**，不打印空串：调用方拿空串当"配置里就是空"会得出
// 完全错误的结论（例如把口令比对失败误报成"口令被改过"）。
func runConfigGet(args []string) error {
	fs := newFlagSet("config-get")
	ymlPath := fs.String("yml", "", "config.yml 路径")
	section := fs.String("section", "", "段落名（可嵌套，任意缩进）")
	key := fs.String("key", "", "键名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*ymlPath) == "" || strings.TrimSpace(*section) == "" || strings.TrimSpace(*key) == "" {
		return fmt.Errorf("config-get: 需要 --yml / --section / --key")
	}
	resolved, err := resolvePath(*ymlPath)
	if err != nil {
		return err
	}
	value, ok, err := readConfigString(resolved, *section, *key)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("config.yml 里没找到 %s.%s", *section, *key)
	}
	fmt.Println(value)
	return nil
}
