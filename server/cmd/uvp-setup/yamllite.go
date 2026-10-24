package main

import (
	"regexp"
	"strings"
)

// 这一组小工具只服务「按段落读写 config.yml 里的单个标量」。
//
// ⛔ 为什么不引 yaml 库：绿色包里改配置必须**逐字保留**原有排版（缩进、行尾注释、
// 空行、CRLF），任何 Marshal 回去的写法都会把注释和顺序洗掉 —— 而现场运维
// 正是靠那些注释理解配置的。这里只做"定位 + 替换一行"，其余字节不动。

// findSection 返回段落头所在行号与缩进宽度（section 可嵌套，任意缩进都认）。
// 找不到返回 -1。
func findSection(lines []string, section string) (start, indent int) {
	// 与 ctl 里原来的 `^(\s*)<section>\s*:\s*(#.*)?$` 逐字等价：
	// 只认「整行只有 section: [+ 注释]」，带值的同名键（如 `zlm: xxx`）不算段落头。
	re := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(section) + `\s*:\s*(#.*)?$`)
	for i, line := range lines {
		if m := re.FindStringSubmatch(line); m != nil {
			return i, len(m[1])
		}
	}
	return -1, 0
}

// sectionEnd 返回段落结束行号（遇到缩进 <= 段名缩进的非空非注释行为止）。
func sectionEnd(lines []string, start, indent int) int {
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if leadingSpace(lines[i]) <= indent {
			return i
		}
	}
	return len(lines)
}

// cutComment 砍掉**引号外**的行尾注释。
//
// ⛔ 顺序不能与 stripQuotes 反：口令里带 # 是合法的（客户完全可能设成 P#ssw0rd），
// 先剥引号就会把它当成注释起点、把口令截断 —— 表现是"重置成了半个口令"。
// （实现与 resetadmin.go 的 cutYAMLComment 同一套规则，那边是"读单个标量"的入口。）

// splitValueAndComment 把 `"v"   # 注释` 拆成值和注释（注释含 # 本身）。
func splitValueAndComment(raw string) (value, comment string) {
	quote := byte(0)
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '#':
			return raw[:i], raw[i:]
		}
	}
	return raw, ""
}
