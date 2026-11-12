package main

import (
	"fmt"
	"os"
	"strings"
)

// runSecretSync 把「平台侧 config.yml 的 zlm.secret」对齐到「ZLM 自己的 config.ini」。
//
//	uvp-setup secret-sync --ini vendor/zlm/config.ini --yml config/config.yml
//
// ⛔⛔ 这是绿色包最隐蔽的一个坑（实测踩过一整轮）：
//
//	config.example.yml 里 zlm.secret 写的是占位符 `CHANGE_ME`，而 ZLM 侧是构建时
//	随机/固定写进 config.ini 的值。两者对不上 ⇒ ZLM 每次 API 调用都返回
//	`{"code":-100,"msg":"Please login first"}`，症状链条极长（录像缓存服务未装配 /
//	点播失败 / hook 全失效），且**每一环都不指向真因**。
//
// ⛔ 方向只能"以 ZLM 为准"：它才是真正在跑的那个进程，反过来改它会让已在跑的 ZLM 失联。
// ⛔ 只替换**占位符或空值**：运维手工填过的真值一律尊重（幂等的关键）。
func runSecretSync(args []string) error {
	fs := newFlagSet("secret-sync")
	iniPath := fs.String("ini", "", "ZLM config.ini 路径")
	ymlPath := fs.String("yml", "", "config.yml 路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*iniPath) == "" || strings.TrimSpace(*ymlPath) == "" {
		return fmt.Errorf("secret-sync: 需要 --ini / --yml")
	}
	absIni, err := resolvePath(*iniPath)
	if err != nil {
		return err
	}
	absYML, err := resolvePath(*ymlPath)
	if err != nil {
		return err
	}
	secret, err := iniScalar(absIni, "api", "secret")
	if err != nil {
		return err
	}
	if secret == "" {
		// 读不到就什么都不做：宁可让后端报「连不上 ZLM」，也不要写一个空 secret 进去。
		note("ZLM config.ini 里没有 [api] secret，跳过对齐")
		return nil
	}
	changed, err := syncYAMLSecret(absYML, secret)
	if err != nil {
		return err
	}
	if changed {
		note("ZLM secret 已按 config.ini 对齐写入 config.yml（%d 字符）", len(secret))
		return nil
	}
	note("ZLM secret 与 config.ini 一致，无需改动")
	return nil
}

// placeholderSecret 是 config.example.yml 里那些"还没填"的值。
// ⛔ 只认这几种；其余一律当"运维填过"处理。
func placeholderSecret(v string) bool {
	if v == "" {
		return true
	}
	switch strings.ToUpper(v) {
	case "CHANGE_ME", "CHANGE-ME", "TODO", "YOUR_SECRET":
		return true
	}
	return false
}

// syncYAMLSecret 在 zlm 段内替换 secret（保留缩进、冒号后空格、行尾注释）。
//
// ⛔ 必须按**缩进层级**定位 zlm 段：config.yml 里它是嵌套段（gb28181: → zlm:），
// 只认"顶格 + 结尾冒号"的判据永远不成立 ⇒ 静默不替换 ⇒ 后端仍拿 CHANGE_ME 去连。
// ⛔ 必须先剥掉行尾注释再判值，否则 `"CHANGE_ME"  # ZLM API secret(...)`
// 会被判成"已有真值"而**谎报成功**。
func syncYAMLSecret(path, secret string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	eol := detectEOL(string(raw))
	lines := splitLines(string(raw), eol)
	start, indent := findSection(lines, "zlm")
	if start < 0 {
		return false, fmt.Errorf("config.yml 里没找到 zlm 段（无法对齐 secret）：%s", path)
	}
	end := sectionEnd(lines, start, indent)
	for i := start + 1; i < end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "secret:") {
			continue
		}
		oldIndent := line[:leadingSpace(line)]
		rest := strings.TrimPrefix(trimmed, "secret:")
		sep := ":"
		if strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t") {
			sep = ":" + rest[:len(rest)-len(strings.TrimLeft(rest, " \t"))]
		}
		value, comment := splitValueAndComment(rest)
		if !placeholderSecret(stripQuotes(value)) {
			return false, nil // 运维填过，尊重它
		}
		newLine := oldIndent + "secret" + sep + `"` + secret + `"`
		if comment != "" {
			newLine += "    " + strings.TrimSpace(comment)
		}
		lines[i] = newLine
		if err := os.WriteFile(path, []byte(strings.Join(lines, eol)), 0o644); err != nil {
			return false, fmt.Errorf("写入 %s 失败: %w", path, err)
		}
		return true, nil
	}
	return false, fmt.Errorf("config.yml 的 zlm 段里没有 secret 键：%s", path)
}

// iniScalar 读 ini 里某段某个键的原始值（去引号）。
// ⛔ 段名大小写不敏感、键名也大小写不敏感（ZLM 的 ini 两种写法都有）。
func iniScalar(path, section, key string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	cur := ""
	for _, line := range splitLines(string(raw), detectEOL(string(raw))) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			cur = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			continue
		}
		if cur != strings.ToLower(section) {
			continue
		}
		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(trimmed[:eq]), key) {
			continue
		}
		return strings.Trim(strings.TrimSpace(trimmed[eq+1:]), `"'`), nil
	}
	return "", nil
}
