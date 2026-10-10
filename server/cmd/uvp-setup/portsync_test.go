package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPorts() portSet {
	return portSet{
		http: 51002, redis: 51003, https: 51000, nginxHTTP: 51001,
		zlm: zlmPorts{
			http: 51004, ssl: 51007, rtsp: 51005, rtmp: 51006, rtc: 51008,
			signaling: 51009, signalingSSL: 51010, srt: 51011, onvif: 51012,
			ice: 51013, rtpProxy: 51014, rtpRange: "51014-51063",
		},
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("源文件不存在（%s），跳过", src)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("拷贝失败: %v", err)
	}
}

// yamlValue 取「段落内」某个键的值（用于断言，独立于被测实现）。
func yamlValue(t *testing.T, path, section, key string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == section+":" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("没找到段落 %s:", section)
	}
	indent := leadingSpace(lines[start])
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if leadingSpace(lines[i]) <= indent {
			break
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), key+":"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// TestSyncYAMLPortsIsSectionScoped 钉住「只改该改的那 5 个键」。
//
// ⛔⛔ `port` 这个键在 httpserver / redis / gormv2.mysql / gormv2.sqlite 下重名 ——
//
//	全局替换会把**数据库端口**一起改掉，症状是「装完启动不了、报数据库连不上」。
func TestSyncYAMLPortsIsSectionScoped(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "server", "config", "config.example.yml")
	dir := t.TempDir()
	dst := filepath.Join(dir, "config.yml")
	copyFile(t, src, dst)

	before := yamlValue(t, dst, "mysql", "port")
	if before == "" {
		t.Fatalf("示例配置里没找到 mysql.port，用例前提不成立")
	}

	if err := syncYAMLPorts(dst, testPorts()); err != nil {
		t.Fatalf("syncYAMLPorts 失败: %v", err)
	}

	want := map[string][2]string{
		"httpserver": {"port", ":51002"},
		"redis":      {"port", "51003"},
		"zlm":        {"httpport", "51004"},
		"media":      {"hookport", "51002"},
	}
	for section, kv := range want {
		if got := yamlValue(t, dst, section, kv[0]); got != kv[1] {
			t.Errorf("%s.%s = %q，期望 %q", section, kv[0], got, kv[1])
		}
	}
	if got := yamlValue(t, dst, "zlm", "rtpport"); got != "51014" {
		t.Errorf("zlm.rtpport = %q，期望 51014", got)
	}
	if after := yamlValue(t, dst, "mysql", "port"); after != before {
		t.Errorf("mysql.port 被误改：%q → %q（必须按段落定位）", before, after)
	}
}

// TestWriteEnvFilePreservesForeignKeys 钉住「非托管键原样保留」。
//
// ⛔ 踩过的真事：Linux 版早期实现用 `> config.env` 全量覆盖、只写端口键，
//
//	而文档正教用户在 config.env 里加 `UVP_SIP_TRACE_ENCRYPTION_KEY=…` ——
//	用户照做、重启、密钥消失、SIP 报文诊断永久 degraded，全程零报错。
func TestWriteEnvFilePreservesForeignKeys(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "config.env")
	original := "# 用户加的行\nUVP_SIP_TRACE_ENCRYPTION_KEY=abc123\nUVP_HTTP_PORT=9999\n"
	if err := os.WriteFile(envPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvFile(envPath, testPorts(), map[string]bool{"UVP_HTTPS_PORT": true}); err != nil {
		t.Fatalf("writeEnvFile 失败: %v", err)
	}
	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, must := range []string{
		"# 用户加的行",
		"UVP_SIP_TRACE_ENCRYPTION_KEY=abc123",
		"UVP_HTTP_PORT=51002",
		"UVP_REDIS_PORT=51003",
		"UVP_HTTPS_PORT=51000",
		"UVP_NGINX_HTTP_PORT=51001",
		"UVP_USER_SET_UVP_HTTPS_PORT=1",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("config.env 缺少 %q\n---\n%s", must, text)
		}
	}
	// 旧值必须被顶掉，且只有「用户显式设过的」键才有标记。
	if strings.Contains(text, "UVP_HTTP_PORT=9999") {
		t.Error("旧的端口值没被覆盖")
	}
	if strings.Contains(text, "UVP_USER_SET_UVP_HTTP_PORT") {
		t.Error("没有标记的键不该被写成用户设置")
	}
}

// TestSyncZLMIniIsSectionScoped 钉住「ZLM config.ini 按段落 + 正确键名改」。
//
// ⛔ 三个必守点见 syncZLMIni 的注释：同名键跨段重名、WebRTC 信令键名不叫 port、
//
//	RTP 段的真源是 [rtp_proxy]。
func TestSyncZLMIniIsSectionScoped(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "deploy", "standalone", "bin", "zlm", "config.ini")
	dir := t.TempDir()
	dst := filepath.Join(dir, "config.ini")
	copyFile(t, src, dst)

	rtmpBefore := iniValue(t, dst, "rtmp", "port")
	if err := syncZLMIni(dst, testPorts().zlm, "uvp-secret-test", "uvp-media-server-0001"); err != nil {
		t.Fatalf("syncZLMIni 失败: %v", err)
	}

	checks := []struct{ section, key, want string }{
		{"api", "secret", "uvp-secret-test"},
		{"general", "mediaServerId", "uvp-media-server-0001"},
		{"http", "port", "51004"},
		{"http", "sslport", "51007"},
		{"rtsp", "port", "51005"},
		{"rtmp", "port", "51006"},
		{"rtc", "signalingPort", "51009"},
		{"rtc", "signalingSslPort", "51010"},
		{"rtc", "enableTurn", "0"},
		{"rtp_proxy", "port", "51014"},
		{"rtp_proxy", "port_range", "51014-51063"},
		{"srt", "port", "51011"},
		{"onvif", "port", "51012"},
		{"rtc", "icePort", "51013"},
		{"rtc", "iceTcpPort", "51013"},
	}
	for _, c := range checks {
		if got := iniValue(t, dst, c.section, c.key); got != c.want {
			t.Errorf("[%s] %s = %q，期望 %q", c.section, c.key, got, c.want)
		}
	}
	if rtmpBefore != "51006" {
		// 只作信息输出：示例配置里 RTMP 原本可能是别的值。
		t.Logf("rtmp.port 原值 %q", rtmpBefore)
	}
}

// TestSyncZLMIniFFmpegBin 钉住 Windows 包的 [ffmpeg] bin 改写规则。
//
// ⛔ 背景：Windows 包的 config.ini 直接从 Linux 那份搬过来，里面写着
//
//	`bin=/usr/bin/ffmpeg` —— 在 Windows 上**永远解不开**（r19 起一直如此，
//	等于依赖 ffmpeg 的通道抓拍在这条交付线上从来没可用过）。
//	Windows 出包时传 `--zlm-ffmpeg ./ffmpeg.exe` 把它改成相对路径。
//
// ⛔⛔ 反向断言同样重要：**留空必须一个字节都不动**。
//
//	如果留空被当成"写成空串"，Linux 绿色包的 config.ini 会被打成 `bin=`，
//	那样连"宿主机的 ffmpeg"这条退路都没了 —— 而这个破坏只在出包后才看得见。
func TestSyncZLMIniFFmpegBin(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "deploy", "standalone", "bin", "zlm", "config.ini")

	t.Run("传值则只改 [ffmpeg] bin", func(t *testing.T) {
		dir := t.TempDir()
		dst := filepath.Join(dir, "config.ini")
		copyFile(t, src, dst)

		z := testPorts().zlm
		z.ffmpegBin = "./ffmpeg.exe"
		if err := syncZLMIni(dst, z, "", ""); err != nil {
			t.Fatalf("syncZLMIni 失败: %v", err)
		}
		if got := iniValue(t, dst, "ffmpeg", "bin"); got != "./ffmpeg.exe" {
			t.Errorf("[ffmpeg] bin = %q，期望 ./ffmpeg.exe", got)
		}
		// 同段其它键不能被碰。
		if got := iniValue(t, dst, "ffmpeg", "restart_sec"); got != "0" {
			t.Errorf("[ffmpeg] restart_sec 被误改：%q", got)
		}
	})

	t.Run("留空则完全不改", func(t *testing.T) {
		dir := t.TempDir()
		dst := filepath.Join(dir, "config.ini")
		copyFile(t, src, dst)
		before := iniValue(t, dst, "ffmpeg", "bin")

		z := testPorts().zlm
		z.ffmpegBin = ""
		if err := syncZLMIni(dst, z, "", ""); err != nil {
			t.Fatalf("syncZLMIni 失败: %v", err)
		}
		if after := iniValue(t, dst, "ffmpeg", "bin"); after != before {
			t.Errorf("[ffmpeg] bin 在留空时被改了：%q → %q", before, after)
		}
		if before == "" {
			t.Error("用例前提不成立：示例 config.ini 里本来就找不到 [ffmpeg] bin")
		}
	})
}

func iniValue(t *testing.T, path, section, key string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "["+section+"]" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("没找到段 [%s]", section)
	}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			break
		}
		if rest, ok := strings.CutPrefix(trimmed, key+"="); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
