package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testINI = "\r\n" +
	"[api]\r\n" +
	"secret=abc\r\n" +
	"\r\n" +
	"[rtc]\r\n" +
	"port=51008\r\n" +
	"tcpPort=51008\r\n" +
	"externIP=\r\n" +
	"interfaces=\r\n" +
	"icePort=51013\r\n" +
	"\r\n" +
	"[rtp_proxy]\r\n" +
	"port=51014\r\n"

func writeTestINI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(path, []byte(testINI), 0o644); err != nil {
		t.Fatalf("写测试 ini 失败: %v", err)
	}
	return path
}

// lookupExternIP 读 [rtc] externIP（断言用，独立于被测实现）。
func lookupExternIP(t *testing.T, path string) string {
	t.Helper()
	return iniValue(t, path, rtcSection, rtcExternIPKey)
}

func TestNormalizeIPv4(t *testing.T) {
	cases := []struct{ in, want string }{
		{"203.0.113.7\n", "203.0.113.7"},
		{`"203.0.113.7"`, "203.0.113.7"},
		{"  10.1.2.3  ", "10.1.2.3"},
		{"", ""},
		{"0.0.0.0", ""},
		{"127.0.0.1", ""},
		{"169.254.169.254", ""},
		{"fe80::1", ""},
		{"not-an-ip", ""},
		{"1.2.3.4:80", ""},
	}
	for _, tc := range cases {
		if got := normalizeIPv4(tc.in); got != tc.want {
			t.Errorf("normalizeIPv4(%q)=%q，期望 %q", tc.in, got, tc.want)
		}
	}
}

// 显式配置必须压过一切：现场已经填好的值不能被自动探测顶掉。
func TestDetectExternIPPrefersExplicit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("198.51.100.9"))
	}))
	defer srv.Close()

	got := detectExternIP(detectInput{
		Explicit:     "203.0.113.7",
		LocalIP:      func() string { return "10.0.0.5" },
		HTTPClient:   srv.Client(),
		MetadataURLs: []metadataURL{{label: "fake", url: srv.URL}},
	})
	if got.IP != "203.0.113.7" || got.Source != "显式配置" {
		t.Fatalf("期望显式值，得到 %+v", got)
	}
}

// 网卡本身就是公网地址 ⇒ 直连公网，不该再去问元数据（白等超时）。
func TestDetectExternIPPublicNICSkipsMetadata(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		_, _ = w.Write([]byte("198.51.100.9"))
	}))
	defer srv.Close()

	got := detectExternIP(detectInput{
		LocalIP:      func() string { return "203.0.113.7" },
		HTTPClient:   srv.Client(),
		MetadataURLs: []metadataURL{{label: "fake", url: srv.URL}},
	})
	if got.IP != "203.0.113.7" || !strings.Contains(got.Source, "公网") {
		t.Fatalf("期望用公网网卡地址，得到 %+v", got)
	}
	if hit {
		t.Fatal("网卡已是公网地址时不该请求元数据")
	}
}

// 云主机：网卡只有内网地址，浏览器可达的是元数据里的公网 IP。
// ⛔ 这条是本次改动的核心 —— 顺序反了（先取网卡）就会把公网访问打坏。
func TestDetectExternIPUsesMetadataWhenNICIsPrivate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("198.51.100.9\n"))
	}))
	defer srv.Close()

	got := detectExternIP(detectInput{
		LocalIP:      func() string { return "10.0.0.5" },
		HTTPClient:   srv.Client(),
		MetadataURLs: []metadataURL{{label: "fake", url: srv.URL}},
	})
	if got.IP != "198.51.100.9" || !strings.Contains(got.Source, "云元数据") {
		t.Fatalf("期望元数据公网 IP，得到 %+v", got)
	}
}

// 局域网部署（或元数据不可用）：退回网卡地址，仍然可用。
func TestDetectExternIPFallsBackToLocalIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	got := detectExternIP(detectInput{
		LocalIP:      func() string { return "192.168.1.10" },
		HTTPClient:   srv.Client(),
		MetadataURLs: []metadataURL{{label: "fake", url: srv.URL}},
		Timeout:      2 * time.Second,
	})
	if got.IP != "192.168.1.10" || got.Source != "本机网卡" {
		t.Fatalf("期望退回网卡地址，得到 %+v", got)
	}
}

func TestDetectExternIPEmptyWhenNothingUsable(t *testing.T) {
	got := detectExternIP(detectInput{
		LocalIP:      func() string { return "" },
		MetadataURLs: []metadataURL{},
	})
	if got.IP != "" {
		t.Fatalf("期望空结果，得到 %+v", got)
	}
}

func TestSyncExternIPIniWritesAndIsIdempotent(t *testing.T) {
	path := writeTestINI(t)

	old, changed, err := syncExternIPIni(path, "203.0.113.7")
	if err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if old != "" || !changed {
		t.Fatalf("首次写入应 changed=true old=\"\"，得到 old=%q changed=%v", old, changed)
	}
	if got := lookupExternIP(t, path); got != "203.0.113.7" {
		t.Fatalf("写后值=%q", got)
	}

	// 幂等：同样再写一次不改文件（mtime 不变，避免 ZLM 误以为配置被外部改过）。
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	old, changed, err = syncExternIPIni(path, "203.0.113.7")
	if err != nil {
		t.Fatalf("二次写入失败: %v", err)
	}
	if changed || old != "203.0.113.7" {
		t.Fatalf("同值应不改动，得到 old=%q changed=%v", old, changed)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("值没变却重写了文件")
	}

	// 换 IP（云主机换 EIP）：更新并返回旧值，供上层打印。
	old, changed, err = syncExternIPIni(path, "198.51.100.9")
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if !changed || old != "203.0.113.7" {
		t.Fatalf("期望 old=203.0.113.7 changed=true，得到 old=%q changed=%v", old, changed)
	}
	if got := lookupExternIP(t, path); got != "198.51.100.9" {
		t.Fatalf("更新后值=%q", got)
	}
}

// ⛔ 只改 [rtc] 段内的 externIP：这份 ini 有重名段，全局替换会误伤其它段；
//   行尾也必须保持 CRLF（ZLM 的 ini 是 CRLF，整文件转 LF 会让人以为被别的工具动过）。
func TestSyncExternIPIniPreservesOtherSectionsAndEOL(t *testing.T) {
	path := writeTestINI(t)
	if _, _, err := syncExternIPIni(path, "203.0.113.7"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.HasSuffix(body, "\r\n") {
		t.Fatal("行尾被改成 LF 了")
	}
	for _, want := range []string{"secret=abc\r\n", "port=51008\r\n", "icePort=51013\r\n", "port=51014\r\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("其它键被改动了，缺少 %q", want)
		}
	}
}

// 键不存在时**报错而不是新增**：新增会掩盖"这不是我们以为的那份 ini"。
func TestSyncExternIPIniMissingKeyIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(path, []byte("[rtc]\r\nport=8000\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := syncExternIPIni(path, "203.0.113.7"); err == nil {
		t.Fatal("缺 externIP 键时应报错")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "externIP") {
		t.Fatal("不该凭空新增键")
	}
}

// --dry-run 只打印，不碰文件（排障时先看探测结果再决定）。
func TestRunRTExternIPDryRunDoesNotWrite(t *testing.T) {
	path := writeTestINI(t)
	if err := runRTExternIP([]string{"--ini", path, "--ip", "203.0.113.7", "--dry-run"}); err != nil {
		t.Fatalf("rc=%v", err)
	}
	if got := lookupExternIP(t, path); got != "" {
		t.Fatalf("dry-run 不应改文件，实际=%q", got)
	}
}

func TestRunRTExternIPRequiresIni(t *testing.T) {
	if err := runRTExternIP(nil); err == nil {
		t.Fatal("缺 --ini 应报错")
	}
}
