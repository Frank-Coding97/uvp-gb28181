package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// managedEnvKeys 是 config.env 里由本工具**接管**的键。
//
// ⛔⛔ 只有这 4 个端口键 + 它们的 UVP_USER_SET_ 标记会被重写；**其余键（含注释行）
//
//	必须原样搬回去**。Linux 版在这里栽过：早期实现用 `> config.env` 全量覆盖、
//	只写 4 个端口键，而上一版的文档正教用户在 config.env 里加一行
//	`UVP_SIP_TRACE_ENCRYPTION_KEY=…` —— 用户照做、重启、密钥消失、
//	SIP 报文诊断永久 degraded，**全程零报错**。
var managedEnvKeys = []string{
	"UVP_HTTP_PORT", "UVP_REDIS_PORT", "UVP_HTTPS_PORT", "UVP_NGINX_HTTP_PORT",
	"UVP_USER_SET_UVP_HTTP_PORT", "UVP_USER_SET_UVP_REDIS_PORT",
	"UVP_USER_SET_UVP_HTTPS_PORT", "UVP_USER_SET_UVP_NGINX_HTTP_PORT",
}

type portSet struct {
	http      int
	redis     int
	https     int
	nginxHTTP int
	zlm       zlmPorts
}

type zlmPorts struct {
	http         int
	ssl          int
	rtsp         int
	rtmp         int
	rtc          int
	signaling    int
	signalingSSL int
	srt          int
	onvif        int
	ice          int
	rtpProxy     int
	rtpRange     string
	// ffmpegBin 留空表示不改。Windows 包的 config.ini 是从 Linux 那份直接搬过来的，
	// 里面写着 `bin=/usr/bin/ffmpeg` —— 在 Windows 上**永远解不开**。
	// 改成相对路径 ./ffmpeg.exe 后：客户往 vendor\zlm\ 丢一个 ffmpeg.exe 就能启用
	// 依赖 ffmpeg 的功能（通道抓拍走 ZLM 的 getSnap）。留空=保持原样。
	ffmpegBin string
}

func (p portSet) managed() map[string]int {
	return map[string]int{
		"UVP_HTTP_PORT":       p.http,
		"UVP_REDIS_PORT":      p.redis,
		"UVP_HTTPS_PORT":      p.https,
		"UVP_NGINX_HTTP_PORT": p.nginxHTTP,
	}
}

// runPortsSync 把「生效端口」落盘到三处：config.env（持久化）、config.yml（后端读）、
// config.ini（ZLM 真源）。缺一就会出现**静默错配**（Redis 听 A、后端连 B）。
func runPortsSync(args []string) error {
	fs := newFlagSet("ports-sync")
	envPath := fs.String("env", "", "config.env 路径（写入生效端口）")
	ymlPath := fs.String("yml", "", "config.yml 路径（同步后端读取的端口键）")
	iniPath := fs.String("ini", "", "ZLM config.ini 路径（同步监听端口与身份）")
	secret := fs.String("secret", "", "ZLM API secret（留空=不改）")
	mediaServerID := fs.String("media-server-id", "", "ZLM mediaServerId（留空=不改）")
	userSet := fs.String("user-set", "", "用户显式设置过的键名（逗号分隔），用于写 UVP_USER_SET_ 标记")

	http := fs.Int("http", 0, "后端 HTTP 端口")
	redis := fs.Int("redis", 0, "Redis 端口")
	https := fs.Int("https", 0, "nginx HTTPS 端口")
	nginxHTTP := fs.Int("nginx-http", 0, "nginx HTTP 端口")
	zHTTP := fs.Int("zlm-http", 0, "ZLM HTTP 端口")
	zSSL := fs.Int("zlm-ssl", 0, "ZLM HTTP SSL 端口")
	zRTSP := fs.Int("zlm-rtsp", 0, "ZLM RTSP 端口")
	zRTMP := fs.Int("zlm-rtmp", 0, "ZLM RTMP 端口")
	zRTC := fs.Int("zlm-rtc", 0, "ZLM WebRTC 媒体端口")
	zSig := fs.Int("zlm-signaling", 0, "ZLM WebRTC 信令端口")
	zSigSSL := fs.Int("zlm-signaling-ssl", 0, "ZLM WebRTC 信令 SSL 端口")
	zSRT := fs.Int("zlm-srt", 0, "ZLM SRT 端口")
	zONVIF := fs.Int("zlm-onvif", 0, "ZLM ONVIF 端口")
	zICE := fs.Int("zlm-ice", 0, "ZLM STUN/TURN 端口")
	zRTP := fs.Int("zlm-rtp-proxy", 0, "ZLM RTP 端口段起点")
	zRTPRange := fs.String("zlm-rtp-range", "", "ZLM RTP 端口段（如 51014-51063）")
	zFFmpeg := fs.String("zlm-ffmpeg", "", "ZLM [ffmpeg] bin 的可执行文件路径（留空=不改；Windows 包传 ./ffmpeg.exe）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ports := portSet{
		http: *http, redis: *redis, https: *https, nginxHTTP: *nginxHTTP,
		zlm: zlmPorts{
			http: *zHTTP, ssl: *zSSL, rtsp: *zRTSP, rtmp: *zRTMP, rtc: *zRTC,
			signaling: *zSig, signalingSSL: *zSigSSL, srt: *zSRT, onvif: *zONVIF,
			ice: *zICE, rtpProxy: *zRTP, rtpRange: *zRTPRange, ffmpegBin: *zFFmpeg,
		},
	}
	markers := map[string]bool{}
	for _, name := range strings.Split(*userSet, ",") {
		if n := strings.TrimSpace(name); n != "" {
			markers[n] = true
		}
	}

	if *envPath != "" {
		if err := writeEnvFile(*envPath, ports, markers); err != nil {
			return err
		}
		note("端口已写入 %s", *envPath)
	}
	if *ymlPath != "" {
		if err := syncYAMLPorts(*ymlPath, ports); err != nil {
			return err
		}
	}
	if *iniPath != "" {
		if err := syncZLMIni(*iniPath, ports.zlm, *secret, *mediaServerID); err != nil {
			return err
		}
	}
	return nil
}

// writeEnvFile 重写 config.env：托管的 4 个端口键 + 标记，其余**原样保留**。
func writeEnvFile(path string, ports portSet, markers map[string]bool) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("创建 config 目录失败: %w", err)
	}
	var preserved []string
	if raw, err := os.ReadFile(resolved); err == nil {
		eol := detectEOL(string(raw))
		for _, line := range splitLines(string(raw), eol) {
			managed := false
			for _, key := range managedEnvKeys {
				if strings.HasPrefix(line, key+"=") {
					managed = true
					break
				}
			}
			if !managed {
				preserved = append(preserved, line)
			}
		}
	}
	var out []string
	for _, key := range []string{"UVP_HTTP_PORT", "UVP_REDIS_PORT", "UVP_HTTPS_PORT", "UVP_NGINX_HTTP_PORT"} {
		out = append(out, fmt.Sprintf("%s=%d", key, ports.managed()[key]))
	}
	// ⛔ 只给「用户显式设过的」写标记：没标记的老包值一律按新默认值处理，
	//   这正好与「升级后跟着新端口规划走」的诉求一致。
	for _, key := range []string{"UVP_HTTP_PORT", "UVP_REDIS_PORT", "UVP_HTTPS_PORT", "UVP_NGINX_HTTP_PORT"} {
		if markers[key] {
			out = append(out, "UVP_USER_SET_"+key+"=1")
		}
	}
	if len(preserved) > 0 {
		out = append(out, "")
		out = append(out, preserved...)
	}
	body := strings.Join(out, "\r\n")
	if !strings.HasSuffix(body, "\r\n") {
		body += "\r\n"
	}
	if err := os.WriteFile(resolved, []byte(body), 0o644); err != nil {
		return fmt.Errorf("写入 config.env 失败: %w", err)
	}
	return nil
}

// syncYAMLPorts 只改「后端真正读的那 5 个端口键」，且**限定在所属段落内**。
//
// ⛔ 必须按段落定位：`port` 这个键在 httpserver / redis / zlm / gormv2.* 多段下重名，
//
//	全局替换会把数据库的端口也改掉。找不到段落或键就**不猜**（返回未命中）。
func syncYAMLPorts(path string, ports portSet) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("读取 config.yml 失败: %w", err)
	}
	eol := detectEOL(string(raw))
	lines := splitLines(string(raw), eol)
	edits := []struct {
		section string
		key     string
		value   string
	}{
		{"httpserver", "port", fmt.Sprintf(":%d", ports.http)}, // 后端要求带冒号
		{"redis", "port", strconv.Itoa(ports.redis)},
		{"zlm", "httpport", strconv.Itoa(ports.zlm.http)},
		{"zlm", "rtpport", strconv.Itoa(ports.zlm.rtpProxy)},
		{"media", "hookport", strconv.Itoa(ports.http)}, // ZLM hook 回调指向本机后端
	}
	changed := 0
	var missed []string
	for _, e := range edits {
		ok, err := replaceYAMLScalar(lines, e.section, e.key, e.value)
		if err != nil {
			return err
		}
		if ok {
			changed++
		} else {
			missed = append(missed, e.section+"."+e.key)
		}
	}
	if len(missed) > 0 {
		return fmt.Errorf("config.yml 里没找到这些键（段落内替换失败，不猜）：%s", strings.Join(missed, ", "))
	}
	if changed == 0 {
		note("config.yml 端口配置无变化")
		return nil
	}
	if err := os.WriteFile(resolved, []byte(strings.Join(lines, eol)), 0o644); err != nil {
		return fmt.Errorf("写入 config.yml 失败: %w", err)
	}
	note("端口已同步进 config.yml（%d 处）", changed)
	return nil
}

var yamlKeyRe = regexp.MustCompile(`^(?P<ind>\s*)(?P<key>[A-Za-z_][\w]*)\s*:\s*.*?(?P<trail>\s*)$`)

// replaceYAMLScalar 在 section 段内把 key 的值换成 value（保留缩进）。
// 段落边界：下一个「缩进 <= 段名缩进」的非空非注释行。
func replaceYAMLScalar(lines []string, section, key, value string) (bool, error) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == section+":" {
			start = i
			break
		}
	}
	if start < 0 {
		return false, nil
	}
	indentOf := leadingSpace(lines[start])
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if leadingSpace(lines[i]) <= indentOf {
			end = i
			break
		}
	}
	for i := start + 1; i < end; i++ {
		m := yamlKeyRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if m[2] != key {
			continue
		}
		lines[i] = m[1] + key + ": " + value
		return true, nil
	}
	return false, nil
}

// zlmIniEdit 描述 config.ini 里一处要改的键（段 + 键 + 值）。
type zlmIniEdit struct {
	section string
	key     string
	value   string
}

// syncZLMIni 同步 ZLM 的 config.ini。
//
// ⛔⛔ 三个必须守住的点（Linux 版全踩过）：
//  1. **按段落定位**：`port` 在 [http]/[rtsp]/[rtmp]/[rtc]/[srt]/[onvif] 里重名，
//     全局替换会把 RTSP 改成 HTTP 的端口。WebRTC 的键名还**不叫 port**
//     （是 signalingPort / signalingSslPort），按 `port` 找会漏。
//  2. **RTP 段的真源是 [rtp_proxy] 的 port / port_range**，不是 [rtp]。
//  3. **enableTurn 强制为 0**：开着 TURN 会占掉 Linux 的临时端口区（Windows 上同样
//     是与系统预留段冲突的来源），而绿色包的 WebRTC 一律走 P2P。
func syncZLMIni(path string, z zlmPorts, secret, mediaServerID string) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("读取 ZLM config.ini 失败: %w", err)
	}
	eol := detectEOL(string(raw))
	lines := splitLines(string(raw), eol)

	edits := []zlmIniEdit{}
	if secret != "" {
		edits = append(edits, zlmIniEdit{"api", "secret", secret})
	}
	if mediaServerID != "" {
		edits = append(edits, zlmIniEdit{"general", "mediaServerId", mediaServerID})
	}
	if z.http > 0 {
		edits = append(edits,
			zlmIniEdit{"http", "port", strconv.Itoa(z.http)},
			zlmIniEdit{"http", "sslport", strconv.Itoa(z.ssl)},
		)
	}
	if z.rtsp > 0 {
		edits = append(edits, zlmIniEdit{"rtsp", "port", strconv.Itoa(z.rtsp)})
	}
	if z.rtmp > 0 {
		edits = append(edits, zlmIniEdit{"rtmp", "port", strconv.Itoa(z.rtmp)})
	}
	if z.rtc > 0 {
		edits = append(edits,
			zlmIniEdit{"rtc", "port", strconv.Itoa(z.rtc)},
			zlmIniEdit{"rtc", "signalingPort", strconv.Itoa(z.signaling)},
			zlmIniEdit{"rtc", "signalingSslPort", strconv.Itoa(z.signalingSSL)},
			zlmIniEdit{"rtc", "icePort", strconv.Itoa(z.ice)},
			zlmIniEdit{"rtc", "iceTcpPort", strconv.Itoa(z.ice)},
			zlmIniEdit{"rtc", "enableTurn", "0"},
		)
	}
	if z.rtpProxy > 0 {
		edits = append(edits, zlmIniEdit{"rtp_proxy", "port", strconv.Itoa(z.rtpProxy)})
	}
	if z.rtpRange != "" {
		edits = append(edits, zlmIniEdit{"rtp_proxy", "port_range", z.rtpRange})
	}
	if z.srt > 0 {
		edits = append(edits, zlmIniEdit{"srt", "port", strconv.Itoa(z.srt)})
	}
	if z.onvif > 0 {
		edits = append(edits, zlmIniEdit{"onvif", "port", strconv.Itoa(z.onvif)})
	}
	if z.ffmpegBin != "" {
		edits = append(edits, zlmIniEdit{"ffmpeg", "bin", z.ffmpegBin})
	}

	applied, missed := 0, []string{}
	for _, e := range edits {
		ok, err := replaceINIScalar(lines, e.section, e.key, e.value)
		if err != nil {
			return err
		}
		if ok {
			applied++
		} else {
			missed = append(missed, e.section+"."+e.key)
		}
	}
	if len(missed) > 0 {
		// ⛔ 不静默：漏改一个键的后果是「平台按 A 端口调 ZLM、ZLM 听 B 端口」，
		//   所有推流/取流静默失败，且现场看不出是哪一步没生效。
		return fmt.Errorf("ZLM config.ini 里没找到这些键（%d 处）：%s", len(missed), strings.Join(missed, ", "))
	}
	if err := os.WriteFile(resolved, []byte(strings.Join(lines, eol)), 0o644); err != nil {
		return fmt.Errorf("写入 ZLM config.ini 失败: %w", err)
	}
	note("ZLM config.ini 已同步（%d 处：端口 + 身份 + 关闭 TURN）", applied)
	return nil
}

var iniKeyRe = regexp.MustCompile(`^(?P<ind>\s*)(?P<key>[A-Za-z_][\w]*)\s*=`)

// replaceINIScalar 在 [section] 段内把 key 的值换成 value（保留缩进与行尾）。
func replaceINIScalar(lines []string, section, key, value string) (bool, error) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "["+section+"]" {
			start = i
			break
		}
	}
	if start < 0 {
		return false, nil
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			end = i
			break
		}
	}
	for i := start + 1; i < end; i++ {
		m := iniKeyRe.FindStringSubmatch(lines[i])
		if m == nil || m[2] != key {
			continue
		}
		lines[i] = m[1] + key + "=" + value
		return true, nil
	}
	return false, nil
}

func leadingSpace(s string) int {
	n := 0
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		n++
	}
	return n
}

// detectEOL：配置文件的行尾必须**原样保留** —— ZLM 的 config.ini 是 CRLF，
// 整文件改成 LF 后 ZLM 仍然能读，但会让人误以为"配置被别的工具动过"。
func detectEOL(s string) string {
	if strings.Contains(s, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func splitLines(s, eol string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	// 文件末尾的换行会产生一个空尾巴元素；写回时会原样还原。
	return lines
}
