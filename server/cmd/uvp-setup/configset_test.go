package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 设备接入地址两段的最小配置骨架（缩进与 config.example.yml 一致）。
//
// ⛔ 刻意让 sample_ingress 与 sample_storage 都带 base_url：
//
//	"按段名定位"是这套读改写的核心难点，同名键在不同段下必须互不干扰。
const deviceAccessFixture = `server:
  name: uvp
gb28181:
  sip:
    id: "34020000002000000001"
  sample_ingress:
    base_url: ""                 # 推荐:完整基址
    host: ""
    port: 0
    require_tls: false
  sample_storage:
    base_url: ""                 # 设备可达基址
  record_query:
    timezone: "Asia/Shanghai"
`

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("写临时配置失败: %v", err)
	}
	return path
}

// configLine 返回段内某个键的整行（已 trim），独立于被测实现做断言。
func configLine(t *testing.T, path, section, key string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	start, indent := -1, 0
	for i, line := range lines {
		if strings.TrimSpace(line) == section+":" {
			start, indent = i, leadingSpace(line)
			break
		}
	}
	if start < 0 {
		t.Fatalf("没找到段落 %s:", section)
	}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if leadingSpace(lines[i]) <= indent {
			break
		}
		if strings.HasPrefix(trimmed, key+":") {
			return trimmed
		}
	}
	return ""
}

func TestConfigSetFillsEmptyValueAndKeepsComment(t *testing.T) {
	path := writeTempConfig(t, deviceAccessFixture)
	if err := runConfigSet([]string{"--yml", path, "--section", "sample_ingress", "--key", "base_url",
		"--value", "http://10.0.0.68:51002", "--if-empty"}); err != nil {
		t.Fatalf("config-set 失败: %v", err)
	}
	got := configLine(t, path, "sample_ingress", "base_url")
	if got != `base_url: "http://10.0.0.68:51002" # 推荐:完整基址` {
		t.Fatalf("期望写入并保留行尾注释，实得 %q", got)
	}
	// ⛔ 另一个同名键必须纹丝不动：写错段会让"抓拍回传"变成"扫码配网"的地址。
	if other := configLine(t, path, "sample_storage", "base_url"); other != `base_url: ""                 # 设备可达基址` {
		t.Fatalf("sample_storage.base_url 被误改: %q", other)
	}
}

func TestConfigSetIfEmptyKeepsExistingValue(t *testing.T) {
	body := strings.Replace(deviceAccessFixture,
		`base_url: ""                 # 推荐:完整基址`,
		`base_url: "https://vms.example.com:51000"`, 1)
	path := writeTempConfig(t, body)

	if err := runConfigSet([]string{"--yml", path, "--section", "sample_ingress", "--key", "base_url",
		"--value", "http://10.0.0.68:51002", "--if-empty"}); err != nil {
		t.Fatalf("config-set 失败: %v", err)
	}
	// ⛔ 这是首启兜底的生命线：现场改过的公网地址绝不能被重启覆盖回内网 IP。
	if got := configLine(t, path, "sample_ingress", "base_url"); !strings.Contains(got, "https://vms.example.com:51000") {
		t.Fatalf("--if-empty 不该覆盖已有值，实得 %q", got)
	}
}

func TestConfigSetWithoutIfEmptyForcesOverwrite(t *testing.T) {
	body := strings.Replace(deviceAccessFixture,
		`base_url: ""                 # 推荐:完整基址`,
		`base_url: "https://vms.example.com:51000"`, 1)
	path := writeTempConfig(t, body)

	if err := runConfigSet([]string{"--yml", path, "--section", "sample_ingress", "--key", "base_url",
		"--value", "http://127.0.0.1:51002"}); err != nil {
		t.Fatalf("config-set 失败: %v", err)
	}
	if got := configLine(t, path, "sample_ingress", "base_url"); !strings.Contains(got, `"http://127.0.0.1:51002"`) {
		t.Fatalf("强制覆盖没生效，实得 %q", got)
	}
}

// 段在、键不在：必须**明确报错**而不是"插一行进去"。
// ⛔ 静默补键会让"配置看起来对了、后端其实没读到"更难查（包不完整就该当场暴露）。
func TestConfigSetRejectsMissingKey(t *testing.T) {
	body := `gb28181:
  sample_ingress:
    host: ""
    port: 0
  record_query:
    timezone: "Asia/Shanghai"
`
	path := writeTempConfig(t, body)
	err := runConfigSet([]string{"--yml", path, "--section", "sample_ingress", "--key", "base_url",
		"--value", "http://10.0.0.68:51002"})
	if err == nil {
		t.Fatal("键不存在时必须非零退出")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("报错信息要点名是哪个键，实得 %v", err)
	}
}

func TestConfigSetKeepsCRLF(t *testing.T) {
	path := writeTempConfig(t, strings.ReplaceAll(deviceAccessFixture, "\n", "\r\n"))
	if err := runConfigSet([]string{"--yml", path, "--section", "sample_storage", "--key", "base_url",
		"--value", "http://10.0.0.68:51001", "--if-empty"}); err != nil {
		t.Fatalf("config-set 失败: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\n") {
		t.Fatalf("写回把 CRLF 洗成了 LF（会让人误以为配置被别的工具动过）")
	}
}

func TestConfigSetRejectsUnknownSection(t *testing.T) {
	path := writeTempConfig(t, deviceAccessFixture)
	err := runConfigSet([]string{"--yml", path, "--section", "no_such_section", "--key", "base_url",
		"--value", "http://10.0.0.68:51002"})
	if err == nil {
		t.Fatal("段不存在时必须非零退出，而不是静默补一段")
	}
}
