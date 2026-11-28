package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"uvplatform.com/uvp-gb28181/internal/httpsconfig"
)

// 贴近真实 config.yml：zlm / media 是**嵌套**在 gb28181 下的（不是顶级段）。
const yamlFixture = `httpserver:
  port: ":51002"
redis:
  indexdb: 3
  port: 51003
mysql:
  port: 3306
server:
  demoaccount:
    defaultpassword: "123456"
gb28181:
  zlm:
    httpport: 51004
    secret: "CHANGE_ME"           # ZLM API secret(部署时填 ZLM 实际 secret)
    rtpport: 51014
  media:
    hookport: 51002
`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigScalar(t *testing.T) {
	path := writeTemp(t, "config.yml", yamlFixture)
	cases := []struct {
		section, key, want string
	}{
		{"demoaccount", "defaultpassword", "123456"}, // 嵌套段（server: → demoaccount:）
		{"redis", "indexdb", "3"},
		{"redis", "port", "51003"},
		{"zlm", "httpport", "51004"},
		// 带行尾注释 + 引号：必须剥掉注释再剥引号
		{"zlm", "secret", "CHANGE_ME"},
	}
	for _, c := range cases {
		got, ok, err := readConfigString(path, c.section, c.key)
		if err != nil {
			t.Fatalf("%s.%s: %v", c.section, c.key, err)
		}
		if !ok {
			t.Errorf("%s.%s 没找到", c.section, c.key)
			continue
		}
		if got != c.want {
			t.Errorf("%s.%s = %q，期望 %q", c.section, c.key, got, c.want)
		}
	}

	// ⛔ 段落作用域：mysql.port 不能被 redis 的 port 顶掉。
	if got, ok, _ := readConfigString(path, "mysql", "port"); !ok || got != "3306" {
		t.Errorf("mysql.port = %q，期望 3306（段落定位失效）", got)
	}
	// 不存在的段落/键必须返回 ok=false（调用方据此报错，而不是拿空串当"配置就是空"）。
	if _, ok, _ := readConfigString(path, "nosuch", "key"); ok {
		t.Error("不存在的段落必须返回 false")
	}
	if _, ok, _ := readConfigString(path, "redis", "nosuch"); ok {
		t.Error("不存在的键必须返回 false")
	}
}

// TestConfigScalarQuotedHash —— 口令里带 # 时不能被当成注释截断。
//
// ⛔ 踩点：`strip_quotes` 与 `cut_comment` 的顺序反了会把 P#ssw0rd 截成 P。
func TestConfigScalarQuotedHash(t *testing.T) {
	path := writeTemp(t, "config.yml",
		"server:\n  demoaccount:\n    defaultpassword: \"P#ssw0rd\"  # 出厂口令\n")
	got, ok, err := readConfigString(path, "demoaccount", "defaultpassword")
	if err != nil || !ok || got != "P#ssw0rd" {
		t.Fatalf("got %q ok=%v err=%v，期望 P#ssw0rd", got, ok, err)
	}
}

func TestSecretSyncReplacesPlaceholderAndKeepsComment(t *testing.T) {
	yml := writeTemp(t, "config.yml", yamlFixture)
	ini := writeTemp(t, "config.ini", "[api]\r\nsecret=S3CR3T-abc123\r\n[rtc]\r\nenableTurn=0\r\n")

	if err := runSecretSync([]string{"--ini", ini, "--yml", yml}); err != nil {
		t.Fatalf("secret-sync 失败: %v", err)
	}
	raw, err := os.ReadFile(yml)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `secret: "S3CR3T-abc123"    # ZLM API secret(部署时填 ZLM 实际 secret)`) {
		t.Errorf("secret 没被正确替换（或行尾注释被吞）：\n%s", text)
	}
	if !strings.Contains(text, "mysql:\n  port: 3306") {
		t.Errorf("别的段被改动了：\n%s", text)
	}
}

func TestSecretSyncDoesNotOverwriteRealValue(t *testing.T) {
	body := strings.Replace(yamlFixture, `"CHANGE_ME"`, `"REAL-SECRET"`, 1)
	yml := writeTemp(t, "config.yml", body)
	ini := writeTemp(t, "config.ini", "[api]\nsecret=OTHER\n")

	if err := runSecretSync([]string{"--ini", ini, "--yml", yml}); err != nil {
		t.Fatalf("secret-sync 失败: %v", err)
	}
	raw, _ := os.ReadFile(yml)
	if !strings.Contains(string(raw), `"REAL-SECRET"`) {
		t.Errorf("运维手工填过的值被覆盖了：\n%s", raw)
	}
	if strings.Contains(string(raw), `"OTHER"`) {
		t.Errorf("不该写入 config.ini 的值：\n%s", raw)
	}
}

func TestSecretSyncMissingZLMSecretIsSkipped(t *testing.T) {
	yml := writeTemp(t, "config.yml", yamlFixture)
	ini := writeTemp(t, "config.ini", "[rtc]\nenableTurn=0\n")
	if err := runSecretSync([]string{"--ini", ini, "--yml", yml}); err != nil {
		// 读不到 ZLM secret 时**不写空值**、也不算失败（后端会明确报"连不上 ZLM"）。
		t.Fatalf("不该报错: %v", err)
	}
	raw, _ := os.ReadFile(yml)
	if !strings.Contains(string(raw), `"CHANGE_ME"`) {
		t.Errorf("没有 ZLM secret 时不该改动 config.yml：\n%s", raw)
	}
}

func TestHTTPSStateFallsBackWhenMissing(t *testing.T) {
	root := t.TempDir()
	s, exists, err := httpsconfig.ReadSettingsFile(httpsconfig.SettingsPath(root))
	if err != nil {
		t.Fatalf("文件不存在不该报错: %v", err)
	}
	if exists || s.Enabled || s.Port != 0 {
		t.Fatalf("文件不存在时应返回空值，得到 exists=%v %+v", exists, s)
	}

	if err := os.MkdirAll(filepath.Join(root, "data", "https"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(httpsconfig.SettingsPath(root), []byte(`{"enabled":true,"port":15500}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, exists, err = httpsconfig.ReadSettingsFile(httpsconfig.SettingsPath(root))
	if err != nil || !exists {
		t.Fatalf("读到落盘状态失败: exists=%v err=%v", exists, err)
	}
	if !s.Enabled || s.Port != 15500 {
		t.Fatalf("读到 %+v，期望 enabled=true port=15500", s)
	}
}

// TestHTTPSStateRejectsInvalidFile —— 文件存在但内容非法必须报错。
//
// ⛔ 静默回退会让 nginx  listens 到 0/默认端口，而页面里显示"HTTPS 已开"，
// 两边表现不一致且没有任何日志。
func TestHTTPSStateRejectsInvalidFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data", "https"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(httpsconfig.SettingsPath(root), []byte(`{"enabled":true,"port":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := httpsconfig.ReadSettingsFile(httpsconfig.SettingsPath(root)); err == nil {
		t.Fatal("非法端口必须报错")
	}
}
