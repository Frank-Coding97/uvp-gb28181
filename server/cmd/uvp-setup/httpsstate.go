package main

import (
	"fmt"

	"uvplatform.com/uvp-gb28181/internal/httpsconfig"
)

// runHTTPSState 把页面保存的 HTTPS 状态打印成一行，供 ctl 脚本决定 nginx 听哪个端口。
//
// 输出格式：`<0|1> <port>`（1=启用 HTTPS）。**没有文件**时输出默认值，
// 这不是错误 —— 首次启动前就没有这个文件，而首次启动必须能起来。
//
// ⛔ 为什么让 Go 读、而不是 shell 里 sed/grep：这个文件是 JSON，
// 用 sed 抠字段一旦格式变化就会**静默拿到空值**，然后 nginx 去监听 0 号端口。
func runHTTPSState(args []string) error {
	fs := newFlagSet("https-state")
	root := fs.String("root", ".", "绿色安装包根目录")
	defaultPort := fs.Int("default-port", 51000, "尚未落盘时使用的默认 HTTPS 端口")
	if err := fs.Parse(args); err != nil {
		return err
	}
	absRoot, err := resolvePath(*root)
	if err != nil {
		return err
	}
	s, exists, err := httpsconfig.ReadSettingsFile(httpsconfig.SettingsPath(absRoot))
	if err != nil {
		return err
	}
	if !exists {
		fmt.Printf("0 %d\n", *defaultPort)
		return nil
	}
	enabled := 0
	if s.Enabled {
		enabled = 1
	}
	fmt.Printf("%d %d\n", enabled, s.Port)
	return nil
}
