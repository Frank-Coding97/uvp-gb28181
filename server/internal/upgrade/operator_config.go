package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ZLM 的配置文件在部署目录里有两份角色，必须分开：
//
//	<release>/vendor/zlm/config.ini  随包发布的**模板**，升级时整目录换新；
//	<root>/config/zlm.ini            MediaServer 真正加载的**持久副本**。
//
// ⛔⛔ 为什么要有持久副本（2026-11-20 客户现场踩到）：
//
//	以前的启动脚本直接让 ZLM 读发布目录里那份。而在线升级是「解包到
//	releases/<taskID>/ 再翻转 current」，vendor/ 整个换新 ⇒ 现场在平台
//	页面里设过的 [rtc] externIP（以及其它调过的键）每次升级都被重置成
//	出厂值，表现为「升级完对讲又没声了」，而且没有任何提示。
//	Docker 版一直是持久副本（/opt/zlm/run/config.ini），单机版现在与之对齐。
//
// 本函数负责**一次性迁移**：老装机的配置还在发布目录里，升级到带持久副本的
// 版本时把它搬过去；已经搬过（目标存在）或旧位置本来就没有文件时什么都不做。
// 幂等，且不碰旧文件 —— 回滚到老版本时老版本仍读自己那份。
//
// root 是安装根（持久目录 config/ 在这里）；legacyRoot 是"旧版本放 config.ini
// 的那一层"—— Linux 单机版是 current 指向的发布目录，Windows 绿色包就是安装根
// 自身（它没有 current 符号链接，整个包原地覆盖）。
const (
	// persistentZLMConfigPath 相对安装根：持久副本（备份 config/ 时会一起带上）。
	persistentZLMConfigPath = "config/zlm.ini"
	// legacyZLMConfigPath 相对发布目录：旧版本直接给 ZLM 加载的那份。
	legacyZLMConfigPath = "vendor/zlm/config.ini"
)

func preserveZLMConfig(root, legacyRoot string) error {
	if root == "" || legacyRoot == "" {
		return nil
	}
	target := filepath.Join(root, persistentZLMConfigPath)
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat persistent zlm config: %w", err)
	}
	source := filepath.Join(legacyRoot, legacyZLMConfigPath)
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		// 老版本没带这份文件：留给启动脚本从新包的模板种子，不在这里猜。
		return nil
	} else if err != nil {
		return fmt.Errorf("read legacy zlm config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create persistent config directory: %w", err)
	}
	// 先写临时文件再改名：升级过程中断电时不能留下半份配置 —— 半份 ini
	// 会让 ZLM 用默认端口启动，症状是"升级完端口全乱"。
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write persistent zlm config: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("sync persistent zlm config: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install persistent zlm config: %w", err)
	}
	return nil
}
