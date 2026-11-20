package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 模板里有、现场那份没有的键要补进去，而且**已有值一个字都不能改**。
func TestFillMissingINIKeysAddsOnlyMissing(t *testing.T) {
	template := []string{
		"[api]", "secret=SHIPPED", "",
		"[rtc]", "port=51008", "tcpPort=51008", "externIP=", "icePort=51013", "",
	}
	target := []string{
		"[api]", "secret=ONSITE", "",
		"[rtc]", "port=51008", "",
	}

	out, added := fillMissingINIKeys(template, target)
	body := strings.Join(out, "\n")
	if !strings.Contains(body, "secret=ONSITE") {
		t.Fatalf("把现场值改掉了：\n%s", body)
	}
	for _, want := range []string{"tcpPort=51008", "externIP=", "icePort=51013"} {
		if !strings.Contains(body, want) {
			t.Fatalf("缺键没补上 %q：\n%s", want, body)
		}
	}
	if len(added) != 3 {
		t.Fatalf("added=%v（期望 3 项）", added)
	}
	// 补在市 [rtc] 段内：必须出现在下一个段头之前。
	if strings.Index(body, "externIP=") > strings.Index(body, "[rtc]")+len(body) {
		t.Fatal("位置异常")
	}
}

// 重名段（[general] 出现两次）不能把第一个段里已有的键当成"第二个段也缺"。
func TestFillMissingINIKeysHandlesDuplicateSections(t *testing.T) {
	template := []string{
		"[general]", "enableVhost=0", "",
		"[general]", "mediaServerId=shipped", "",
	}
	target := []string{
		"[general]", "enableVhost=1", "",
		"[general]", "check_nvidia_dev=1", "",
	}
	out, added := fillMissingINIKeys(template, target)
	body := strings.Join(out, "\n")
	if len(added) != 1 || !strings.Contains(added[0], "mediaServerId") {
		t.Fatalf("added=%v（只该补第二个 [general] 的 mediaServerId）", added)
	}
	if strings.Count(body, "enableVhost=") != 1 {
		t.Fatalf("重名段的键被重复补：\n%s", body)
	}
	if !strings.Contains(body, "enableVhost=1") {
		t.Fatal("现场值被改掉")
	}
}

// 模板里整段都是新的 ⇒ 段头 + 键值一起追加到末尾，且不动已有内容。
func TestFillMissingINIKeysAppendsUnknownSection(t *testing.T) {
	template := []string{"[api]", "secret=x", "", "[srt]", "port=51011", "timeoutSec=5", ""}
	target := []string{"[api]", "secret=y", ""}
	out, added := fillMissingINIKeys(template, target)
	body := strings.Join(out, "\n")
	if !strings.Contains(body, "[srt]") || !strings.Contains(body, "timeoutSec=5") {
		t.Fatalf("新段没追加：\n%s", body)
	}
	if len(added) != 2 {
		t.Fatalf("added=%v", added)
	}
	if !strings.HasPrefix(body, "[api]\nsecret=y") {
		t.Fatalf("原有内容被重排：\n%s", body)
	}
}

func TestRunINIFillRequiresBothPaths(t *testing.T) {
	if err := runINIFill([]string{"--template", "x"}); err == nil {
		t.Fatal("缺 --target 应报错")
	}
}

// ⛔ CRLF 必须原样保留（ZLM 的 ini 是 CRLF）；没有可补的键时**不写文件**。
func TestRunINIFillPreservesEOLAndSkipsNoopWrite(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "template.ini")
	tgt := filepath.Join(dir, "zlm.ini")
	if err := os.WriteFile(tpl, []byte("[rtc]\r\nexternIP=\r\nicePort=51013\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tgt, []byte("[rtc]\r\nexternIP=62.234.152.39\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runINIFill([]string{"--template", tpl, "--target", tgt}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\r\nicePort=51013\r\n") {
		t.Fatalf("补键后行尾不是 CRLF：%q", raw)
	}
	if !strings.Contains(string(raw), "externIP=62.234.152.39") {
		t.Fatalf("现场值被改掉：%q", raw)
	}

	// 再跑一次：已无缺失 ⇒ 不写文件（mtime 不变，避免无谓改写）。
	before, err := os.Stat(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if err := runINIFill([]string{"--template", tpl, "--target", tgt}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("没有缺失键却重写了文件")
	}
}

func TestRunINIFillDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	tpl := filepath.Join(dir, "template.ini")
	tgt := filepath.Join(dir, "zlm.ini")
	if err := os.WriteFile(tpl, []byte("[rtc]\nport=1\nicePort=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tgt, []byte("[rtc]\nport=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runINIFill([]string{"--template", tpl, "--target", tgt, "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(tgt)
	if strings.Contains(string(raw), "icePort") {
		t.Fatal("dry-run 不应改文件")
	}
}
