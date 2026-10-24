package main

import (
	"fmt"
	"os"
	"strings"
)

// runConfigSet 把 config.yml 里某个段落下的标量键改成新值，**逐字保留**其余内容
// （缩进、行尾注释、空行、CRLF）。
//
//	uvp-setup config-set --yml config/config.yml --section qr_provision --key base_url \
//	    --value http://10.0.0.68:51002 --if-empty
//
// ⛔⛔ 为什么需要 --if-empty 而不是"脚本先 config-get 再判断空不空"：
//
//	两次进程之间隔着一次读，中间任何异常（段缺失、键缺失、文件被换）都会让调用方
//	把"读不到"当成"就是空的"，然后覆盖掉现场配好的公网地址 —— 而这类地址一旦被
//	覆盖，表现是"设备连不上但平台零报错"。判断与写入必须在**同一次读改写**里。
//
// ⛔ 不带 --if-empty = 强制覆盖，只给运维手工排障用；首启兜底脚本一律带它。
//
// ⛔ 找不到段落或键时**非零退出**，不自动新增：config.yml 缺段说明包不完整，
//
//	静默补一段只会让"配置看起来对了、后端其实没读到"这种故障更难查。
func runConfigSet(args []string) error {
	fs := newFlagSet("config-set")
	ymlPath := fs.String("yml", "", "config.yml 路径")
	section := fs.String("section", "", "段落名（可嵌套，任意缩进）")
	key := fs.String("key", "", "键名")
	value := fs.String("value", "", "新值（按字符串写入，带双引号）")
	ifEmpty := fs.Bool("if-empty", false, "仅当当前值为空时才写入（首启兜底用）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*ymlPath) == "" || strings.TrimSpace(*section) == "" || strings.TrimSpace(*key) == "" {
		return fmt.Errorf("config-set: 需要 --yml / --section / --key")
	}
	resolved, err := resolvePath(*ymlPath)
	if err != nil {
		return err
	}

	current, found, err := readConfigString(resolved, *section, *key)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("config.yml 里没找到 %s.%s（包不完整？拒绝新增未知键）", *section, *key)
	}
	if *ifEmpty && strings.TrimSpace(current) != "" {
		// 已有的值一律不碰：这是"现场配过就尊重现场"的唯一实现点。
		fmt.Printf("%s.%s 已有值，保持不动\n", *section, *key)
		return nil
	}
	if strings.TrimSpace(current) == strings.TrimSpace(*value) {
		return nil
	}

	raw, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("读取 config.yml 失败（%s）: %w", resolved, err)
	}
	text := string(raw)
	eol := detectEOL(text)
	lines := splitLines(text, eol)
	start, indent := findSection(lines, *section)
	if start < 0 {
		return fmt.Errorf("config.yml 里找不到段落 %s", *section)
	}
	end := sectionEnd(lines, start, indent)

	target := -1
	childIndent := -1
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if childIndent < 0 {
			childIndent = leadingSpace(lines[i])
		}
		if m := yamlScalarRe.FindStringSubmatch(lines[i]); m != nil && m[2] == *key {
			target = i
			childIndent = len(m[1])
			break
		}
	}
	if target < 0 {
		// readConfigString 已经确认过"键存在"，走到这里说明两份正则对同一行判断不一致
		// （例如键名带引号）。此时宁可报错，也不要在段里插一行"看起来像"的配置。
		return fmt.Errorf("config.yml 里 %s 段下未能定位 %s 行，拒绝盲目插入", *section, *key)
	}
	// ⛔ 保留行尾注释：现场运维正是靠那些注释理解配置的，
	//   而本工具存在的意义就是"只改这一行、别动别的字节"。
	m := yamlScalarRe.FindStringSubmatch(lines[target])
	_, comment := splitValueAndComment(m[3])
	lines[target] = strings.Repeat(" ", childIndent) + *key + ": " + quoteYAMLString(*value)
	if comment != "" {
		lines[target] += " " + comment
	}

	if err := os.WriteFile(resolved, []byte(strings.Join(lines, eol)), 0o644); err != nil {
		return fmt.Errorf("写回 config.yml 失败（%s）: %w", resolved, err)
	}
	fmt.Printf("%s.%s = %s\n", *section, *key, *value)
	return nil
}

// quoteYAMLString 输出 YAML 双引号字符串。
//
// ⛔ 反斜杠与双引号必须转义（YAML 双引号标量的规则）；地址里通常不会有它们，
//
//	但"通常"不是理由 —— 漏转义会写出一个解析失败的 config.yml，后端直接起不来。
//	⚠️ readConfigString 只剥引号、不做反转义，所以本工具读回自己写的值时，
//	含反斜杠的值会原样带回转义形式。这对 URL 无影响（URL 不含反斜杠），
//	而含反斜杠的场景（Windows 路径）本工具不负责。
func quoteYAMLString(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}
