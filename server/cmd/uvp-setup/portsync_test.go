package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
		// ⛔ WebRTC 媒体是 UDP+TCP 两套监听：port(UDP) 与 tcpPort(TCP) 都必须落到规划段，
		//   漏掉 tcpPort 会让它留在 ZLM 出厂默认 8000 上（2026-10-12 客户实机踩到）。
		{"rtc", "port", "51008"},
		{"rtc", "tcpPort", "51008"},
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

// TestWriteEnvFileWritesZLMKeys 钉住「ZLM 端口也要落盘」。
//
// ⛔ 不写这几行，ctl 每次启动都会用默认值去探活：用户把 ZLM 挪到别的端口后，
// 进程明明在听新端口，status 却报「ZLM 未运行」。
func TestWriteEnvFileWritesZLMKeys(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "config.env")
	if err := writeEnvFile(envPath, testPorts(), nil); err != nil {
		t.Fatalf("writeEnvFile 失败: %v", err)
	}
	raw, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	want := []string{
		"UVP_ZLM_HTTP_PORT=51004",
		"UVP_ZLM_SSL_PORT=51007",
		"UVP_ZLM_RTSP_PORT=51005",
		"UVP_ZLM_RTMP_PORT=51006",
		"UVP_ZLM_RTC_PORT=51008",
		"UVP_ZLM_RTP_PROXY_PORT=51014",
		"UVP_ZLM_RTP_RANGE=51014-51063",
		"UVP_ZLM_SIGNALING_PORT=51009",
		"UVP_ZLM_SIGNALING_SSL_PORT=51010",
		"UVP_ZLM_SRT_PORT=51011",
		"UVP_ZLM_ONVIF_PORT=51012",
		"UVP_ZLM_ICE_PORT=51013",
	}
	for _, must := range want {
		if !strings.Contains(text, must+"\n") {
			t.Errorf("config.env 缺少 %q\n---\n%s", must, text)
		}
	}
}

// TestWriteEnvFileIsIdempotent 钉住「反复同步不长空行、不重复键」。
//
// ⛔ 真实缺陷（2026-10-19 发现）：保留段里本来就含首尾空行，而写回时又额外补一个
// 空行分隔 ⇒ 每启动一次就多一行。Windows 版每次启动都跑 ports-sync，
// 装完放几个月回来，config.env 开头会有几十行空白。
func TestWriteEnvFileIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "config.env")
	original := "# 注释\n\nUVP_HTTP_PORT=9999\n\nUVP_SIP_TRACE_ENCRYPTION_KEY=abc\n"
	if err := os.WriteFile(envPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var first []byte
	for i := 0; i < 5; i++ {
		if err := writeEnvFile(envPath, testPorts(), nil); err != nil {
			t.Fatalf("第 %d 次 writeEnvFile 失败: %v", i+1, err)
		}
		got, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = got
			continue
		}
		if string(got) != string(first) {
			t.Fatalf("第 %d 次同步的内容与第一次不同（不幂等）\n--- 第一次 ---\n%s\n--- 第 %d 次 ---\n%s",
				i+1, first, i+1, got)
		}
	}
	if strings.Count(string(first), "UVP_SIP_TRACE_ENCRYPTION_KEY") != 1 {
		t.Errorf("非托管键被重复写入：\n%s", first)
	}
}

// TestSyncZLMIniSoftKeys —— 缺「可选键」只告警，缺「硬键」必须阻断。
//
// ⛔ 判据不是"重要性"，而是**这一处写不进去会不会坏功能**：
//
//	· [rtc] enableTurn 是死开关（平台只下发 STUN）⇒ 缺了继续启动；
//	· [rtp_proxy] port_range 决定收流端口段 ⇒ 缺了必须停（否则规划与实际监听对不上）。
func TestSyncZLMIniSoftKeys(t *testing.T) {
	fixture := "[http]\r\nport=80\r\nsslport=443\r\n" +
		"[rtsp]\r\nport=554\r\n" +
		"[rtmp]\r\nport=1935\r\n" +
		"[rtc]\r\nport=8000\r\ntcpPort=8000\r\nsignalingPort=3000\r\n" +
		"signalingSslPort=3001\r\nicePort=3478\r\niceTcpPort=3478\r\n" +
		"[rtp_proxy]\r\nport=10000\r\nport_range=30000-35000\r\n" +
		"[srt]\r\nport=9000\r\n[onvif]\r\nport=3702\r\n"

	write := func(t *testing.T, body string) error {
		t.Helper()
		dst := filepath.Join(t.TempDir(), "config.ini")
		if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return syncZLMIni(dst, testPorts().zlm, "", "")
	}

	t.Run("缺 enableTurn / rtsp.sslport 不阻断", func(t *testing.T) {
		if err := write(t, fixture); err != nil {
			t.Fatalf("可选键缺失不该阻断启动: %v", err)
		}
	})

	t.Run("缺 rtp_proxy.port_range 必须阻断", func(t *testing.T) {
		err := write(t, strings.Replace(fixture, "port_range=30000-35000\r\n", "", 1))
		if err == nil {
			t.Fatal("port_range 缺失时必须报错（否则收流端口会跑出规划段）")
		}
		if !strings.Contains(err.Error(), "rtp_proxy") {
			t.Errorf("报错里没指出是 rtp_proxy：%v", err)
		}
	})
}

// TestSyncZLMIniKeepsCRLF 钉住「行尾原样保留」。
//
// ⛔ ZLM 的 config.ini 是 CRLF；改成 LF 虽仍能读，但会让现场误判"配置被别的工具动过"，
// 且下一次按 \r 写回的正则（历史实现）就对不上了。
func TestSyncZLMIniKeepsCRLF(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "config.ini")
	body := "[http]\r\nport=80\r\nsslport=443\r\n[rtsp]\r\nport=554\r\nsslport=0\r\n" +
		"[rtmp]\r\nport=1935\r\nsslport=0\r\n" +
		"[rtc]\r\nport=8000\r\ntcpPort=8000\r\nsignalingPort=3000\r\nsignalingSslPort=3001\r\n" +
		"icePort=3478\r\niceTcpPort=3478\r\nenableTurn=1\r\n" +
		"[rtp_proxy]\r\nport=10000\r\nport_range=30000-35000\r\n[srt]\r\nport=9000\r\n[onvif]\r\nport=3702\r\n"
	if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncZLMIni(dst, testPorts().zlm, "", ""); err != nil {
		t.Fatalf("syncZLMIni 失败: %v", err)
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\n") {
		t.Error("同步后出现了裸 LF —— 行尾被改写了")
	}
}

// notPortKeys 含 "port" 字样、但**不是端口**的键名。
//
// ⛔ 用显式排除而不是"以 port 结尾"这种字面规则：`on_flow_report` 也以 port 结尾
//
//	（re-**port**），`tcpPort`/`sslport` 却不一定在词边界上 —— 两头的正则都会错。
//	这里宁可枚举：ZLM 升版新增了这类键，本用例会变红并把它指名道姓报出来，
//	比"悄悄漏检一个真端口"划算得多。
var notPortKeys = map[string]bool{
	"on_flow_report":     true, // [hook] 流量上报阈值，不是端口
	"icetransportpolicy": true, // [rtc] 0=all / 1=relay
	"rtptransporttype":   true, // [rtsp] -1=both
	"port_range":         true, // 区间，由别的用例判
}

// iniPortKeys 扫出所有**单值端口键**，返回 "section.key" → 值。
func iniPortKeys(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	out := map[string]string{}
	section := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		lower := strings.ToLower(key)
		if notPortKeys[lower] || !strings.Contains(lower, "port") {
			continue
		}
		out[section+"."+key] = strings.TrimSpace(value)
	}
	return out
}

// TestSyncZLMIniFactoryPortsAllLandInPlannedRange 用包内**出厂 config.ini** 当夹具，
//
//	同步一遍后要求每一个监听端口键都等于"这次传进去的端口"之一（0 = 该协议关闭，放行）。
//
// ⛔⛔ 为什么要用出厂文件而不是手写小夹具（2026-10-12 客户实机踩到的形状）：
//
//	同步器长期只改 [rtc] port，漏了同段的 tcpPort ⇒ 后者一直停在 ZLM 出厂默认
//	**8000**（规划段外）。8000 是极常见的冲突端口，被占住时 ZLM 启动 `rtcSrv_tcp->
//	start(8000)` 抛异常、只打一行 `Listen on :: 8000 failed: address already in use`
//	就退出，而原因行还会被退出时几十行 `~EventPoller` 析构日志挤出 tail 窗口 ——
//	现场多半误判成"缺依赖"。手写夹具只能覆盖"我记得的那些键"，出厂文件才能覆盖
//	**所有**键：以后 ZLM 升版多一个 port 类键，这条会直接变红。
//
// ⛔ 判据刻意不写死 51000-51064：写死就变成"端口规划常量"的第二次维护（且改规划
//
//	还要同步改这里）。真正的诉求是"**没有键被落下**"，所以拿本次传入的端口集合比对
//	—— 落下的键会留在出厂默认值上，必然不在集合里。
func TestSyncZLMIniFactoryPortsAllLandInPlannedRange(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "deploy", "standalone", "bin", "zlm", "config.ini")
	dst := filepath.Join(t.TempDir(), "config.ini")
	copyFile(t, src, dst)

	z := testPorts().zlm

	// 前提校验：出厂夹具里确实有停在出厂默认上的端口键，否则这个用例什么都没证明。
	if got := iniValue(t, dst, "rtc", "tcpPort"); got == "" {
		t.Fatal("出厂 config.ini 里没有 [rtc] tcpPort —— 夹具前提不成立")
	}

	if err := syncZLMIni(dst, z, "", ""); err != nil {
		t.Fatalf("syncZLMIni 失败: %v", err)
	}

	allowed := map[string]bool{"0": true}
	for _, p := range []int{
		z.http, z.ssl, z.rtsp, z.rtmp, z.rtc, z.signaling, z.signalingSSL,
		z.srt, z.onvif, z.ice, z.rtpProxy,
	} {
		allowed[strconv.Itoa(p)] = true
	}

	keys := iniPortKeys(t, dst)
	if len(keys) == 0 {
		t.Fatal("一个 port 键都没扫到 —— 扫描判据失效")
	}
	var offenders []string
	for key, value := range keys {
		if !allowed[value] {
			offenders = append(offenders, key+"="+value)
		}
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("这些端口键没有被同步（仍停在出厂默认或写错值），收流/播放会静默指向错误端口：\n  %s",
			strings.Join(offenders, "\n  "))
	}
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
