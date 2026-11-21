package main

import (
	"fmt"
	"os"
	"strings"
)

// runINIFill 把**发布模板**里有、而**现场持久副本**里没有的键补进去。
//
//	uvp-setup ini-fill --template <发布包 vendor/zlm/config.ini> --target <config/zlm.ini> [--dry-run]
//
// ⛔⛔ 为什么需要它（2026-11-20，与 config/zlm.ini 持久化配套）：
//
//	单机版以前直接让 ZLM 读发布目录里的 config.ini —— 升级整目录换新，现场调过的
//	值全被重置（客户原话："每次在线升级 rtc.externIP 都会被重置"）。现在改成
//	「持久副本 + 随包模板」，升级不再动现场那份；但代价是**新版本新增的键永远
//	到不了老现场**（ZLM 缺键用内置默认值，而新版本改动常常正是要改默认值）。
//
//	更硬的一条：ports-sync 对端口这类键是**硬失败**（键找不到就返回 error，
//	ctl 直接 fail）。老现场那份 ini 若缺一个新键，升级后平台会起不来 ——
//	"补齐缺失键"就是给这条链路兜底。
//
// 语义刻意保守：
//   - **只增不改**：目标里已有的键一个都不碰（哪怕值和模板不同 —— 那是现场设置）；
//   - 按「段名 + 第几次出现」配对（这份 ini 有重名段 [general]），键名忽略大小写；
//   - 目标整段缺失时，段头和键一起追加到文件末尾；
//   - 没有可补的键就**不写文件**（避免每次启动都改 mtime）。
func runINIFill(args []string) error {
	fs := newFlagSet("ini-fill")
	tplPath := fs.String("template", "", "发布包自带的 config.ini（模板）")
	tgtPath := fs.String("target", "", "现场持久副本 config/zlm.ini")
	dryRun := fs.Bool("dry-run", false, "只打印要补哪些键，不改文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*tplPath) == "" || strings.TrimSpace(*tgtPath) == "" {
		return fmt.Errorf("--template 与 --target 都必填")
	}
	tplResolved, err := resolvePath(*tplPath)
	if err != nil {
		return err
	}
	tgtResolved, err := resolvePath(*tgtPath)
	if err != nil {
		return err
	}

	tplRaw, err := os.ReadFile(tplResolved)
	if err != nil {
		return fmt.Errorf("读取 ZLM 模板失败: %w", err)
	}
	tgtRaw, err := os.ReadFile(tgtResolved)
	if err != nil {
		return fmt.Errorf("读取 ZLM 配置失败: %w", err)
	}
	eol := detectEOL(string(tgtRaw))
	tplLines := splitLines(string(tplRaw), detectEOL(string(tplRaw)))
	tgtLines := splitLines(string(tgtRaw), eol)

	out, added := fillMissingINIKeys(tplLines, tgtLines)
	if len(added) == 0 {
		return nil
	}
	if *dryRun {
		note("可补齐 %d 个新键：%s", len(added), summarizeINIKeys(added))
		return nil
	}
	if err := os.WriteFile(tgtResolved, []byte(strings.Join(out, eol)), 0o644); err != nil {
		return fmt.Errorf("写入 ZLM 配置失败: %w", err)
	}
	// ⛔ 必须说出来：这次升级带了新的配置项，现场那份被补过 —— 排查时才不会
	//   把"我没改过这个键，它怎么冒出来了"当成异常。
	note("ZLM 配置已从模板补齐 %d 个新键：%s", len(added), summarizeINIKeys(added))
	return nil
}

// summarizeINIKeys 让日志保持一行：老现场整份配置缺失时可能有上百个键，
// 全打出来只会把终端刷满，真正要看的"补齐了没有"反而被冲走。
func summarizeINIKeys(keys []string) string {
	const limit = 6
	if len(keys) <= limit {
		return strings.Join(keys, ", ")
	}
	return strings.Join(keys[:limit], ", ") + fmt.Sprintf(" 等 %d 项", len(keys))
}

// iniSpan 描述一段 [section]：header 是段头行号，(header, end) 是段体。
type iniSpan struct {
	name   string
	header int
	end    int
}

// scanINISpans 按出现顺序扫出所有段。⛔ 不能用通用 ini 解析库：这份文件有
// 重名段（[general] 出现两次），标准库会直接报重复段错误（曾经实测踩过）。
func scanINISpans(lines []string) []iniSpan {
	var spans []iniSpan
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			if len(spans) > 0 {
				spans[len(spans)-1].end = i
			}
			spans = append(spans, iniSpan{
				name:   strings.ToLower(strings.Trim(strings.TrimSpace(t), "[]")),
				header: i,
				end:    len(lines),
			})
		}
	}
	return spans
}

// iniSpanKey 是「段名 + 第几次出现」：重名段靠它区分。
func iniSpanKey(name string, occurrence int) string {
	return fmt.Sprintf("%s#%d", name, occurrence)
}

// indexINISpans 把段按 name#occurrence 建索引。
func indexINISpans(spans []iniSpan) map[string]iniSpan {
	out := make(map[string]iniSpan, len(spans))
	seen := map[string]int{}
	for _, s := range spans {
		out[iniSpanKey(s.name, seen[s.name])] = s
		seen[s.name]++
	}
	return out
}

// iniKeysIn 返回段体内所有键（小写，用于忽略大小写比较）。
func iniKeysIn(lines []string, s iniSpan) map[string]bool {
	keys := map[string]bool{}
	for i := s.header + 1; i < s.end; i++ {
		if key, _, ok := iniScalarLine(lines[i]); ok {
			keys[strings.ToLower(key)] = true
		}
	}
	return keys
}

// iniScalarLine 解析一行 `key=value`，返回**原样**键名与去空白后的值。
// ⛔ 键名不能顺手小写：写回时要保持模板里的写法（ZLM 的 ini 是给现场看的，
//   把 tcpPort 改成 tcpport 只会让人以为配置文件被谁动过）。
func iniScalarLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return "", "", false
	}
	m := iniKeyRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(m[2]), strings.TrimSpace(line[idx+1:]), true
}

// fillMissingINIKeys 只增不改地把模板里缺的键补进目标，返回新行与补了哪些键。
func fillMissingINIKeys(template, target []string) ([]string, []string) {
	tgtIndex := indexINISpans(scanINISpans(target))
	inserts := map[int][]string{} // 段头行号 → 要追加到该段末尾的行
	var added []string

	seen := map[string]int{}
	for _, tplSpan := range scanINISpans(template) {
		occ := seen[tplSpan.name]
		seen[tplSpan.name]++
		tgtSpan, ok := tgtIndex[iniSpanKey(tplSpan.name, occ)]
		if !ok {
			continue // 整段缺失由下面的 tail 分支处理
		}
		existing := iniKeysIn(target, tgtSpan)
		// 模板里每个键前面那几行注释是它的说明书（这份 ini 的注释是中英双语的
		// 配置手册）⇒ 补键时一并带过去，否则新补的键在文件里孤零零一行，
		// 现场只能去别处查它是什么意思。空行会把注释块截断（图省事也够用）。
		var pending []string
		for i := tplSpan.header + 1; i < tplSpan.end; i++ {
			line := template[i]
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
				pending = append(pending, line)
				continue
			}
			key, value, ok := iniScalarLine(line)
			if !ok {
				pending = nil
				continue
			}
			if existing[strings.ToLower(key)] {
				pending = nil
				continue
			}
			existing[strings.ToLower(key)] = true
			inserts[tgtSpan.header] = append(inserts[tgtSpan.header], append(append([]string{}, pending...), key+"="+value)...)
			added = append(added, "["+tplSpan.name+"] "+key)
			pending = nil
		}
	}

	// 目标里完全没有的段：整段（段头 + 键值）追加到文件末尾。
	known := map[string]bool{}
	for _, s := range scanINISpans(target) {
		known[s.name] = true
	}
	var tail []string
	seen = map[string]int{}
	for _, tplSpan := range scanINISpans(template) {
		occ := seen[tplSpan.name]
		seen[tplSpan.name]++
		if known[tplSpan.name] || occ > 0 {
			continue // 同名段在目标里出现过（或已是第二次出现）：不整段补
		}
		body := []string{"[" + tplSpan.name + "]"}
		for i := tplSpan.header + 1; i < tplSpan.end; i++ {
			if key, value, ok := iniScalarLine(template[i]); ok {
				body = append(body, key+"="+value)
				added = append(added, "["+tplSpan.name+"] "+key)
			}
		}
		if len(body) > 1 {
			tail = append(tail, body...)
		}
	}

	out := rebuildWithInserts(target, inserts)
	if len(tail) > 0 {
		if n := len(out); n > 0 && strings.TrimSpace(out[n-1]) == "" {
			out = out[:n-1]
		}
		out = append(out, tail...)
		out = append(out, "")
	}
	return out, added
}

// rebuildWithInserts 在每段**最后一个非空行之后**插入补充键。
func rebuildWithInserts(lines []string, inserts map[int][]string) []string {
	if len(inserts) == 0 {
		return lines
	}
	spans := scanINISpans(lines)
	insertAt := map[int][]string{}
	for _, s := range spans {
		extra, ok := inserts[s.header]
		if !ok {
			continue
		}
		last := s.header
		for i := s.header + 1; i < s.end; i++ {
			if strings.TrimSpace(lines[i]) != "" {
				last = i
			}
		}
		insertAt[last] = append(insertAt[last], extra...)
	}
	out := make([]string, 0, len(lines)+8)
	for i, line := range lines {
		out = append(out, line)
		if extra := insertAt[i]; len(extra) > 0 {
			out = append(out, extra...)
		}
	}
	return out
}
