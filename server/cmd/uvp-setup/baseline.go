package main

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// splitSQL 按分号切分语句，跳过字符串字面量与注释里的分号。
//
// ⛔⛔ 本函数是 server/resource/database/sqlitebaseline/generate.py 的 split_sql()
// 的逐字移植（也是 deploy/standalone/uvp-gb28181-ctl.sh 内嵌的那份），
// **三处必须是同一条规则**：
//   - 直接 split(";") 会在种子数据上炸掉：菜单标题、带查询串的回调 URL 里都有分号。
//   - 逐行累积 + sqlite3.complete_statement 也不行：前一句的尾随注释会被算进下一句。
//
// ⇒ 任何改动都必须让 baseline_test.go 的「与 Python 切分指纹逐字节一致」继续成立
//
//	（该用例直接用 python3 跑 generate.py 的 split_sql 对拍）。
func splitSQL(text string) []string {
	statements := []string{}
	var buffer strings.Builder
	var quote byte
	i, n := 0, len(text)

	for i < n {
		ch := text[i]

		if quote == 0 && ch == '-' && strings.HasPrefix(text[i:], "--") {
			nl := strings.IndexByte(text[i:], '\n')
			if nl < 0 {
				i = n
			} else {
				i += nl + 1
			}
			continue
		}
		if quote == 0 && ch == '/' && strings.HasPrefix(text[i:], "/*") {
			end := strings.Index(text[i:], "*/")
			if end < 0 {
				i = n
			} else {
				i += end + 2
			}
			continue
		}

		if quote != 0 {
			if ch == '\\' && quote == '\'' {
				buffer.WriteByte(ch)
				i++
				if i < n {
					buffer.WriteByte(text[i])
					i++
				}
				continue
			}
			if ch == quote {
				if i+1 < n && text[i+1] == quote { // '' 转义
					buffer.WriteByte(ch)
					buffer.WriteByte(text[i+1])
					i += 2
					continue
				}
				quote = 0
			}
			buffer.WriteByte(ch)
			i++
			continue
		}

		if ch == '\'' || ch == '"' {
			quote = ch
			buffer.WriteByte(ch)
			i++
			continue
		}
		if ch == ';' {
			statements = append(statements, buffer.String())
			buffer.Reset()
			i++
			continue
		}
		buffer.WriteByte(ch)
		i++
	}

	tail := strings.TrimSpace(buffer.String())
	if tail != "" {
		statements = append(statements, tail)
	}
	out := make([]string, 0, len(statements))
	for _, s := range statements {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// splitDigest 与 Linux 版 ctl.sh 的 `UVP_DB_SPLIT_DIGEST=1` 分支**同格式**：
//
//	sha256("\n\x1e\n".join(stmts))
//
// 分隔符用 U+001E（Record Separator）：语句里不可能出现，拼接无歧义。
func splitDigest(stmts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(stmts, "\n\x1e\n")))
	return hex.EncodeToString(sum[:])
}

// 语句分相：与 ctl.sh 的 phase_of() 逐条对齐（只看前 40 个字符的大写前缀）。
const (
	phaseTable  = "table"
	phaseIndex  = "index"
	phaseSeed   = "seed"
	phasePragma = "pragma"
	phaseOther  = "other"
)

func phaseOf(statement string) string {
	head := strings.ToUpper(statement)
	if len(head) > 40 {
		head = head[:40]
	}
	switch {
	case strings.HasPrefix(head, "CREATE TABLE"):
		return phaseTable
	case strings.HasPrefix(head, "CREATE INDEX"), strings.HasPrefix(head, "CREATE UNIQUE INDEX"):
		return phaseIndex
	case strings.HasPrefix(head, "INSERT"):
		return phaseSeed
	case strings.HasPrefix(head, "PRAGMA"):
		return phasePragma
	default:
		return phaseOther
	}
}

var createObjectRe = regexp.MustCompile(`(?is)^CREATE\s+(?:UNIQUE\s+)?(TABLE|INDEX)\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([^"\s(]+)"?`)

// planStatements 算出「进度分母」与「哪些语句其实不会建出新对象」。
//
// ⛔⛔ 为什么分母不能直接等于语句条数：SQLite 里**表名和索引名都是库级唯一**，
//
//	而基线是从 MySQL 转来的、MySQL 的索引名只在**表内**唯一 ⇒ 同名索引会在多张表上
//	各出现一次（如 idx_deleted_at 在 5 张表上都有），这些
//	`CREATE INDEX IF NOT EXISTS` 会被 SQLite **静默跳过**。
//	若按条数计进度，进度条跑到 316/316 而结尾对账只有 307 个索引 ——
//	读者的第一反应是「对账写错了」，而不是「有 9 条压根没生效」。
//	⇒ 分母用「去重后真正会建出来的对象数」，并把被跳过的**显式报出来**。
func planStatements(stmts []string) (phases []string, counted []bool, dupes []string, total int) {
	phases = make([]string, len(stmts))
	counted = make([]bool, len(stmts))
	seen := map[string]bool{}
	for i, s := range stmts {
		phases[i] = phaseOf(s)
		counted[i] = true
		if phases[i] != phaseTable && phases[i] != phaseIndex {
			continue
		}
		m := createObjectRe.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		key := strings.ToUpper(m[1]) + "\x00" + m[2]
		if seen[key] {
			counted[i] = false
			dupes = append(dupes, m[2])
			continue
		}
		seen[key] = true
	}
	for _, c := range counted {
		if c {
			total++
		}
	}
	return phases, counted, dupes, total
}
