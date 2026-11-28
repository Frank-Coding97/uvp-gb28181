package main

import (
	"fmt"

	"uvplatform.com/uvp-gb28181/internal/httpsconfig"
)

// runHTTPSRepairPort 修复"落盘 HTTPS 端口与本包明文端口撞车"的存量配置。
//
// 背景（2026-10-19 端口重排：HTTP 51000 / HTTPS 51001）：
// 上一版是 HTTPS 51000 / HTTP 51001，而 `data/https/settings.json` 在
// **首次启动时无条件落盘**（httpsconfig.RenderStartup）⇒ 每个装过并启动过的
// 实例里都存着 port=51000。升级到新包后 51000 变成明文端口，
// 两个 listen 抢同一端口，nginx 直接起不来（"duplicate listen" / bind 失败，
// 或 https-render 报"HTTPS 端口不能与 HTTP 端口相同"）。
//
// 语义刻意做成**只修冲突、不碰其它值**：
//   - 落盘端口 == --conflict  ⇒ 改成 --fallback 并写回（幂等：再跑一次就是 unchanged）；
//   - 落盘端口 != --conflict  ⇒ 原样保留（用户手工设的 8443 之类不许动）；
//   - 文件不存在               ⇒ 什么都不做（首次启动还没有这个文件）。
//
// 输出：`absent` / `unchanged <port>` / `migrated <from> -> <to>`，便于脚本日志与测试断言。
func runHTTPSRepairPort(args []string) error {
	fs := newFlagSet("https-repair-port")
	root := fs.String("root", ".", "绿色安装包根目录")
	conflict := fs.Int("conflict", 0, "与本包明文端口相同的那个落盘端口（要迁移走的）")
	fallback := fs.Int("fallback", 0, "迁移目标端口（本包的 HTTPS 默认端口）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *conflict < 1 || *conflict > 65535 {
		return fmt.Errorf("--conflict 必须是 1-65535 之间的端口，收到 %d", *conflict)
	}
	if *fallback < 1 || *fallback > 65535 {
		return fmt.Errorf("--fallback 必须是 1-65535 之间的端口，收到 %d", *fallback)
	}
	if *conflict == *fallback {
		// ⛔ 迁移到同一个值等于没迁，还会让调用方以为已经修好了。
		return fmt.Errorf("--fallback(%d) 不能与 --conflict(%d) 相同", *fallback, *conflict)
	}

	absRoot, err := resolvePath(*root)
	if err != nil {
		return err
	}
	path := httpsconfig.SettingsPath(absRoot)
	s, exists, err := httpsconfig.ReadSettingsFile(path)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Println("absent")
		return nil
	}
	if s.Port != *conflict {
		fmt.Printf("unchanged %d\n", s.Port)
		return nil
	}
	s.Port = *fallback
	if err := httpsconfig.WriteSettingsFile(path, s); err != nil {
		return fmt.Errorf("写回 %s 失败: %w", path, err)
	}
	fmt.Printf("migrated %d -> %d\n", *conflict, *fallback)
	return nil
}
