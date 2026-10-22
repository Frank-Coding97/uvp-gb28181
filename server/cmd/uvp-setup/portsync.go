package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// managedEnvKeys 是 config.env 里由本工具**接管**的键。
//
// ⛔⛔ 只有这些键会被重写；**其余键（含注释行）必须原样搬回去**。
//
//	Linux 版在这里栽过：早期实现用 `> config.env` 全量覆盖、只写 4 个端口键，
//	而上一版的文档正教用户在 config.env 里加一行
//	`UVP_SIP_TRACE_ENCRYPTION_KEY=…` —— 用户照做、重启、密钥消失、
//	SIP 报文诊断永久 degraded，**全程零报错**。
//
// ⛔ ZLM 端口也必须落盘：ctl 启动时按 config.env 里的 UVP_ZLM_* 决定探测端口，
// 不写就会每次启动都回到默认值 —— 用户改了端口、ZLM 也听新端口，
// 而脚本仍拿默认值去探活 ⇒ 报「ZLM 未运行」。
var managedEnvKeys = []string{
	"UVP_HTTP_PORT", "UVP_REDIS_PORT", "UVP_HTTPS_PORT", "UVP_NGINX_HTTP_PORT",
	"UVP_USER_SET_UVP_HTTP_PORT", "UVP_USER_SET_UVP_REDIS_PORT",
	"UVP_USER_SET_UVP_HTTPS_PORT", "UVP_USER_SET_UVP_NGINX_HTTP_PORT",
	"UVP_ZLM_HTTP_PORT", "UVP_ZLM_SSL_PORT", "UVP_ZLM_RTSP_PORT", "UVP_ZLM_RTMP_PORT",
	"UVP_ZLM_RTC_PORT", "UVP_ZLM_RTP_PROXY_PORT", "UVP_ZLM_RTP_RANGE",
	"UVP_ZLM_SIGNALING_PORT", "UVP_ZLM_SIGNALING_SSL_PORT", "UVP_ZLM_SRT_PORT",
	"UVP_ZLM_ONVIF_PORT", "UVP_ZLM_ICE_PORT",
	// ⭐ 2026-10-20：ZLM 端口也支持「用户显式设置」标记。
	//   之前只有 4 个主端口有标记，ZLM 的 12 个键是"看到值就用" —— 两套语义并存，
	//   用户改一个 ZLM 端口在升级后会被新包默认值顶掉，而他改主端口却能保住。
	//   现在两套合并成一套：任何端口键只要带标记（或与上次落盘值不同，见 ctl 侧
	//   .port-baseline 判定）就是用户设置，升级后一律保留。
	"UVP_USER_SET_UVP_ZLM_HTTP_PORT", "UVP_USER_SET_UVP_ZLM_SSL_PORT",
	"UVP_USER_SET_UVP_ZLM_RTSP_PORT", "UVP_USER_SET_UVP_ZLM_RTMP_PORT",
	"UVP_USER_SET_UVP_ZLM_RTC_PORT", "UVP_USER_SET_UVP_ZLM_RTP_PROXY_PORT",
	"UVP_USER_SET_UVP_ZLM_RTP_RANGE", "UVP_USER_SET_UVP_ZLM_SIGNALING_PORT",
	"UVP_USER_SET_UVP_ZLM_SIGNALING_SSL_PORT", "UVP_USER_SET_UVP_ZLM_SRT_PORT",
	"UVP_USER_SET_UVP_ZLM_ONVIF_PORT", "UVP_USER_SET_UVP_ZLM_ICE_PORT",
}

// portHeaderPrefix 是 config.env 顶部「端口区」注释的前缀。
//
// ⛔ 幂等全靠它：这一块是程序自己写的，重写时必须先把自己上次写的那几行剔掉，
// 否则每启动一次就多一份副本（注释行不属于"托管键"，会被当用户内容保留）。
const portHeaderPrefix = "# [uvp-ports]"

// markerKeys 是「允许出现 UVP_USER_SET_ 标记」的全部键，顺序与 writeEnvFile 的输出一致。
//
// ⛔ 必须与 ctl.sh 里 read_port_env/port_is_user_set 的候选键表**逐字对应**：
// 少一个键，那一路的"用户改过"就在升级时丢掉（静默回到新默认值）。
var markerKeys = []string{
	"UVP_HTTP_PORT", "UVP_REDIS_PORT", "UVP_HTTPS_PORT", "UVP_NGINX_HTTP_PORT",
	"UVP_ZLM_HTTP_PORT", "UVP_ZLM_SSL_PORT", "UVP_ZLM_RTSP_PORT", "UVP_ZLM_RTMP_PORT",
	"UVP_ZLM_RTC_PORT", "UVP_ZLM_RTP_PROXY_PORT", "UVP_ZLM_RTP_RANGE",
	"UVP_ZLM_SIGNALING_PORT", "UVP_ZLM_SIGNALING_SSL_PORT", "UVP_ZLM_SRT_PORT",
	"UVP_ZLM_ONVIF_PORT", "UVP_ZLM_ICE_PORT",
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
	baselinePath := fs.String("baseline", "", "上次落盘值记录文件（留空=不写）；ctl 用它判断用户是否手改过端口")
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
	// ⛔ 基线必须**最后**写：前面任何一步失败都不能留下"我已经写过这些端口"的记录，
	//   否则下次启动会把这次没真正生效的值当成用户设置，永久钉死。
	if *baselinePath != "" {
		if err := writeBaselineFile(*baselinePath, ports); err != nil {
			return err
		}
	}
	return nil
}

// writeBaselineFile 记下「本次实际落盘的全部端口值」，供 ctl 下次判断用户是否手改过。
//
// ⛔⛔ 为什么需要它（这是"所有端口都能改"的关键一环）：
//
//	UVP_USER_SET_ 标记是唯一可靠的"这是用户意愿"证据，但它要求客户**手写两行**
//	（一个值 + 一个标记）。改一个端口就要记两条，漏一条就静默失效、还会被下次
//	启动覆盖回去。要求现场运维记住这个，等于把坑留给客户。
//
//	⇒ 反过来做：程序把自己上次写进去的值记在 config/.port-baseline 里。
//	下次启动时「文件里的值 ≠ 基线里的值」即说明有人手改过 ⇒ 采纳并自动补标记
//	（补完即使基线文件被删也不会退回默认值）。
//
//	老包没有这个文件 ⇒ 首启一律按新默认值走 ⇒ 与既有"升级后跟着新端口规划"
//	的语义**完全一致**，不会让存量实例停在旧端口上。
func writeBaselineFile(path string, ports portSet) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("创建基线目录失败: %w", err)
	}
	lines := []string{
		"# 由 uvp-setup ports-sync 自动维护：记录上次落盘的端口值。",
		"# ⛔ 不要手改本文件 —— 手工改端口请改 config.env，本文件只是用来判断「谁改过」。",
	}
	lines = append(lines, zlmEnvLines(ports.zlm)...)
	// 主端口放最后无所谓，但顺序与 config.env 保持一致，肉眼对账最快。
	lines = append(lines, fmt.Sprintf("UVP_HTTP_PORT=%d", ports.http))
	lines = append(lines, fmt.Sprintf("UVP_REDIS_PORT=%d", ports.redis))
	lines = append(lines, fmt.Sprintf("UVP_HTTPS_PORT=%d", ports.https))
	lines = append(lines, fmt.Sprintf("UVP_NGINX_HTTP_PORT=%d", ports.nginxHTTP))
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(resolved, []byte(body), 0o644); err != nil {
		return fmt.Errorf("写入端口基线失败: %w", err)
	}
	return nil
}

// writeEnvFile 重写 config.env：托管的端口键 + 标记，其余**原样保留**。
func writeEnvFile(path string, ports portSet, markers map[string]bool) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("创建 config 目录失败: %w", err)
	}
	// ⛔ 行尾必须跟着**已有文件**走：Linux 上要是被写成 CRLF，
	//   ctl 的 `sed -n 's/^UVP_HTTP_PORT=//p'` 会把 \r 一起带回变量里，
	//   后续 `[ "$port" -gt 0 ]` 直接报 integer expression expected。
	eol := "\n"
	if runtime.GOOS == "windows" {
		eol = "\r\n"
	}
	var preserved []string
	if raw, err := os.ReadFile(resolved); err == nil {
		eol = detectEOL(string(raw))
		for _, line := range splitLines(string(raw), eol) {
			// ⛔ 自己写的标题块必须跳过，否则**每次启动都会多一份**：
			//   它以 `#` 开头 ⇒ 不属于"托管键行" ⇒ 会被当作用户内容保留，
			//   再叠加新写的一份，几百次启动后文件就没法看了。
			if strings.HasPrefix(line, portHeaderPrefix) {
				continue
			}
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
	// ⛔ 必须削掉首尾空行：本函数**每次启动**都会重写这个文件，
	//   保留空行就等于"每启动一次多一行"，几百次之后没人愿意再看它。
	preserved = trimBlankEdges(preserved)

	out := []string{
		portHeaderPrefix + " ============================================================",
		portHeaderPrefix + " 端口（全部可改：改值 → ./uvp-gb28181-ctl.sh restart 生效）",
		portHeaderPrefix + " ⛔ 不用手写 UVP_USER_SET_ 标记：程序会对比上次落盘值，",
		portHeaderPrefix + "    发现你改过就自动记住（并在下次启动补上标记）。",
		// ⛔ 这两行必须与打包时写进 config.env 的说明**同样存在**：
		//   本函数每次启动都会重写整个说明区，少写一句就等于「客户重启一次，
		//   那条最容易踩的规则就消失了」。而 RTP 段与 proxy 只改一个是最常见的改法。
		portHeaderPrefix + " ⛔ UVP_ZLM_RTP_RANGE 与 UVP_ZLM_RTP_PROXY_PORT 必须同一起点；",
		portHeaderPrefix + "    只改其中一个，程序会按你改的那个自动拉齐另一个。",
		portHeaderPrefix + " ⛔ SIP 端口不在本文件（引导页 → 存数据库），默认 = RTP 段尾 + 1。",
		portHeaderPrefix + " ⛔ 1024 以下是特权端口，本包以普通用户运行，绑不上。",
		portHeaderPrefix + " ============================================================",
	}
	out = append(out, portHeaderPrefix+" ---- 管理页 / 后端 / Redis ----")
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
	out = append(out, portHeaderPrefix+" ---- 流媒体（ZLMediaKit）----")
	out = append(out, zlmEnvLines(ports.zlm)...)
	// ⭐ ZLM 段的标记紧跟在值之后（与主端口同一形态）：
	//   用户改过一个媒体端口，升级后必须保住 —— 否则"我明明改过 SRT 端口"
	//   会在下一次换包时静默回到默认值，而现场只会看到"推流又不行了"。
	for _, key := range markerKeys[4:] {
		if markers[key] {
			out = append(out, "UVP_USER_SET_"+key+"=1")
		}
	}
	if len(preserved) > 0 {
		out = append(out, "")
		out = append(out, preserved...)
	}
	body := strings.Join(out, eol) + eol
	if err := os.WriteFile(resolved, []byte(body), 0o644); err != nil {
		return fmt.Errorf("写入 config.env 失败: %w", err)
	}
	return nil
}

// zlmEnvLines 把 ZLM 的生效端口写成 UVP_ZLM_* 键。
//
// ⛔ 顺序与 ctl 里的变量声明顺序一致（HTTP/SSL/RTSP/…），肉眼对账最快。
func zlmEnvLines(z zlmPorts) []string {
	return []string{
		fmt.Sprintf("UVP_ZLM_HTTP_PORT=%d", z.http),
		fmt.Sprintf("UVP_ZLM_SSL_PORT=%d", z.ssl),
		fmt.Sprintf("UVP_ZLM_RTSP_PORT=%d", z.rtsp),
		fmt.Sprintf("UVP_ZLM_RTMP_PORT=%d", z.rtmp),
		fmt.Sprintf("UVP_ZLM_RTC_PORT=%d", z.rtc),
		fmt.Sprintf("UVP_ZLM_RTP_PROXY_PORT=%d", z.rtpProxy),
		fmt.Sprintf("UVP_ZLM_RTP_RANGE=%s", z.rtpRange),
		fmt.Sprintf("UVP_ZLM_SIGNALING_PORT=%d", z.signaling),
		fmt.Sprintf("UVP_ZLM_SIGNALING_SSL_PORT=%d", z.signalingSSL),
		fmt.Sprintf("UVP_ZLM_SRT_PORT=%d", z.srt),
		fmt.Sprintf("UVP_ZLM_ONVIF_PORT=%d", z.onvif),
		fmt.Sprintf("UVP_ZLM_ICE_PORT=%d", z.ice),
	}
}

func trimBlankEdges(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
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
	// soft=true：键**不存在**时只告警、不阻断启动。
	// 判据是「这处改动影响不影响收流/可用性」：
	//   · enableTurn 是死开关（平台只下发 STUN），缺了不改变任何可用性；
	//   · rtsp/rtmp 的 sslport 客户可能自己删过，缺了也不影响规划段。
	// 其余键（端口、port_range）缺一个就是同步逻辑失效 —— 必须停。
	soft bool
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
//  4. **[http] dirMenu 强制为 0**：那是「能不能浏览目录」，开着等于把录像库挂在
//     HTTP 上给人随便翻（详见下面那处 edits 的注释）。
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
		edits = append(edits, zlmIniEdit{section: "api", key: "secret", value: secret})
	}
	if mediaServerID != "" {
		edits = append(edits, zlmIniEdit{section: "general", key: "mediaServerId", value: mediaServerID})
	}
	// ⛔⛔ [http] dirMenu 必须强制为 0 —— 关掉 ZLM 的「目录浏览」。
	//   开着的时候，ZLM 的 HTTP 口会把 rootPath(./www) 整个目录列成一个网页，
	//   点进去就能逐个下载；而 ./www 正是**录像、截图、HLS 片段的落盘目录**。
	//   [http] allow_ip_range 出厂就放行 10/172.16-31/192.168 私有段 ⇒
	//   同网段任何人**不用登录平台**就能把全部历史录像翻一遍。
	//   这类风险的特点：平台页面一切正常、日志里也查不到任何痕迹，
	//   只能靠出厂默认值挡（客户不会主动去关一个他没听说过的开关）。
	// ⛔ 为什么用 soft 而不是硬失败：ZLM 取这个键是「字符串转 bool」，
	//   **缺键/空串本身就等于关闭**（HttpFileManager.cpp 里 `if (!dirMenu) return false`），
	//   所以"没找到键"不是故障；把它写成硬失败，等于让一处纯加固把整个平台
	//   卡在启动阶段（与 enableTurn 同一个理由）。
	edits = append(edits, zlmIniEdit{section: "http", key: "dirMenu", value: "0", soft: true})
	if z.http > 0 {
		edits = append(edits,
			zlmIniEdit{section: "http", key: "port", value: strconv.Itoa(z.http)},
			zlmIniEdit{section: "http", key: "sslport", value: strconv.Itoa(z.ssl)},
		)
	}
	if z.rtsp > 0 {
		edits = append(edits,
			zlmIniEdit{section: "rtsp", key: "port", value: strconv.Itoa(z.rtsp)},
			// RTSP/RTMP over TLS 一律不启用：规划段只给一条明文口，
			// 客户若自己配了证书可把这行删掉（soft：缺键不影响启动）。
			zlmIniEdit{section: "rtsp", key: "sslport", value: "0", soft: true},
		)
	}
	if z.rtmp > 0 {
		edits = append(edits,
			zlmIniEdit{section: "rtmp", key: "port", value: strconv.Itoa(z.rtmp)},
			zlmIniEdit{section: "rtmp", key: "sslport", value: "0", soft: true},
		)
	}
	if z.rtc > 0 {
		edits = append(edits,
			zlmIniEdit{section: "rtc", key: "port", value: strconv.Itoa(z.rtc)},
			// ⛔⛔ WebRTC 媒体是 UDP + TCP **两套**监听：`port` 走 UDP、`tcpPort` 走 TCP。
			//   两个都得写（同号、不同协议栈，不冲突；与 icePort/iceTcpPort 同一个道理）。
			//   只改 port ⇒ tcpPort 留在 ZLM 出厂默认 **8000**（规划段外），
			//   目标机上 8000 被别的程序占住时，ZLM 启动 `rtcSrv_tcp->start(8000)`
			//   抛异常、只打一行 `Start server failed: Listen on :: 8000 failed:
			//   address already in use` 就退出 —— 而原因行还会被退出时那几十行
			//   `~EventPoller` 析构日志挤出 tail 窗口，现场极易误判成"缺依赖"。
			//   （2026-10-12 客户 Linux 包实机踩到；Linux 侧同步器同一处也漏，已一并修。）
			zlmIniEdit{section: "rtc", key: "tcpPort", value: strconv.Itoa(z.rtc)},
			zlmIniEdit{section: "rtc", key: "signalingPort", value: strconv.Itoa(z.signaling)},
			zlmIniEdit{section: "rtc", key: "signalingSslPort", value: strconv.Itoa(z.signalingSSL)},
			zlmIniEdit{section: "rtc", key: "icePort", value: strconv.Itoa(z.ice)},
			zlmIniEdit{section: "rtc", key: "iceTcpPort", value: strconv.Itoa(z.ice)},
			// ⛔ TURN 是**死功能**（平台只下发 STUN），关掉只是省掉一段临时端口区。
			//   ⇒ 键不存在时只告警、不阻断启动：为一个没人用的中继拦住整个平台上线不划算。
			// ⚠️ 将来真要开中继，**改的就是这一行**（ctl.sh 里已没有 ZLM_ENABLE_TURN 变量），
			//   同时还要把 [rtc] port_range 挪出 Linux 临时端口区并放开防火墙，
			//   只把这里改成 "1" 等于把 port_range 埋回临时端口段。详见 ctl.sh 的同名注释。
			zlmIniEdit{section: "rtc", key: "enableTurn", value: "0", soft: true},
		)
	}
	if z.rtpProxy > 0 {
		edits = append(edits, zlmIniEdit{section: "rtp_proxy", key: "port", value: strconv.Itoa(z.rtpProxy)})
	}
	if z.rtpRange != "" {
		edits = append(edits, zlmIniEdit{section: "rtp_proxy", key: "port_range", value: z.rtpRange})
	}
	if z.srt > 0 {
		edits = append(edits, zlmIniEdit{section: "srt", key: "port", value: strconv.Itoa(z.srt)})
	}
	if z.onvif > 0 {
		edits = append(edits, zlmIniEdit{section: "onvif", key: "port", value: strconv.Itoa(z.onvif)})
	}
	if z.ffmpegBin != "" {
		edits = append(edits, zlmIniEdit{section: "ffmpeg", key: "bin", value: z.ffmpegBin})
	}

	applied, missed, softMissed := 0, []string{}, []string{}
	for _, e := range edits {
		ok, err := replaceINIScalar(lines, e.section, e.key, e.value)
		if err != nil {
			return err
		}
		if ok {
			applied++
			continue
		}
		if e.soft {
			softMissed = append(softMissed, "["+e.section+"] "+e.key)
			continue
		}
		missed = append(missed, e.section+"."+e.key)
	}
	if len(missed) > 0 {
		// ⛔ 不静默：漏改一个键的后果是「平台按 A 端口调 ZLM、ZLM 听 B 端口」，
		//   所有推流/取流静默失败，且现场看不出是哪一步没生效。
		return fmt.Errorf("ZLM config.ini 里没找到这些键（%d 处）：%s", len(missed), strings.Join(missed, ", "))
	}
	if err := os.WriteFile(resolved, []byte(strings.Join(lines, eol)), 0o644); err != nil {
		return fmt.Errorf("写入 ZLM config.ini 失败: %w", err)
	}
	note("ZLM config.ini 已同步（%d 处：端口 + 身份 + 关闭 TURN + 关闭目录浏览）", applied)
	if len(softMissed) > 0 {
		// ⛔ 走 stderr：ctl 会把 stdout 归到「明细」里，这类"要人看一眼"的提示
		//   必须能跳出来（与 shell 侧 `det` 的缩进宽度一致）。
		fmt.Fprintf(os.Stderr, "%s! ZLM config.ini 里没有这些可选键，已跳过：%s\n",
			indent, strings.Join(softMissed, ", "))
	}
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
