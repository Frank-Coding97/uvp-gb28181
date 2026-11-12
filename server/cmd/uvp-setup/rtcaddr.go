package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// rtc.externIP 是 ZLM 给浏览器的「WebRTC 可见地址」。
//
// ⛔ 语义是**替换**不是追加：ZLM 的 WebRtcTransport::onCheckAnswer 里
// `m.addr.address = extern_ips.empty() ? 本机IP : extern_ips[0]` ——
// 填了一个错地址，连内网原本能直连的候选都会被换掉。所以这里的取值必须
// 是「对端真的能连到的地址」，不是「随便一个地址」。
//
// ⛔ 必须写**真实 IP**，不能用 ZLM 原生的 `externIP=$EXTERN_IP` 环境变量语法：
// `getServerConfig` 返回的是 ini 原文（WebApi.cpp 直接 dump mINI），平台会读到
// 字符串 `$EXTERN_IP`，再拼成 `stun:$EXTERN_IP:51013` 发给浏览器 ⇒ 对讲直接废。
const (
	rtcSection     = "rtc"
	rtcExternIPKey = "externIP"

	// metadataTimeout 是**单次**元数据探测的上限；整体另有 totalMetadataBudget。
	// 取值偏短是刻意的：云元数据要么在同一台宿主/链路本地、毫秒级返回，
	// 要么根本不存在。等它 3 秒只会让每次启动都变慢。
	metadataTimeout = 700 * time.Millisecond
)

// metadataEIP 是云厂商元数据里「公网 IP」的查询地址。
//
// 为什么需要它：云主机（弹性公网 IP）的网卡上只有内网地址 10.x，
// 公网 IP 是 NAT 映射出来的 —— 本机怎么读网卡都读不到它。
// 局域网部署的机器命不中这些地址（请求会快速失败），不影响主路径。
var metadataEIP = []metadataURL{
	{label: "阿里云", url: "http://100.100.100.200/latest/meta-data/eipv4"},
	{label: "腾讯云", url: "http://metadata.tencentyun.com/latest/meta-data/public-ipv4"},
	{label: "AWS", url: "http://169.254.169.254/latest/meta-data/public-ipv4"},
}

type metadataURL struct {
	label string
	url   string
}

// externIPResult 是探测结论。Source 只用于打印给人看，不参与任何判断。
type externIPResult struct {
	IP     string
	Source string
}

// detectInput 把外部依赖全部显式化，测试才能不起网络、不依赖本机网卡。
type detectInput struct {
	Explicit     string
	LocalIP      func() string
	HTTPClient   *http.Client
	MetadataURLs []metadataURL
	Timeout      time.Duration
}

// detectExternIP 按「显式配置 → 云元数据公网 IP → 本机出口网卡」取值。
//
// 三段对应三种部署形态，规则就这三条：
//  1. 局网部署：网卡上就是内网地址，填它 —— 浏览器与平台同侧，能直连；
//  2. 公网部署（机器直连公网）：网卡上就是公网地址，填它；
//  3. 公网部署（云主机弹性 IP）：网卡只有内网地址，只能从云元数据拿公网 IP。
//
// ⛔ 第 3 条必须先于第 1 条判断：云主机的网卡地址（10.x）对公网浏览器不可达，
// 先取网卡就会把「能用」变成「不能用」。
func detectExternIP(in detectInput) externIPResult {
	if ip := normalizeIPv4(in.Explicit); ip != "" {
		return externIPResult{IP: ip, Source: "显式配置"}
	}

	local := ""
	if in.LocalIP != nil {
		local = normalizeIPv4(in.LocalIP())
	}
	// 网卡本身就是公网地址 ⇒ 直连公网，元数据不用问了（省掉一次超时等待）。
	if local != "" && !isPrivateIPv4(net.ParseIP(local)) {
		return externIPResult{IP: local, Source: "本机网卡（公网）"}
	}

	if r := probeMetadata(in); r.IP != "" {
		return r
	}
	if local != "" {
		return externIPResult{IP: local, Source: "本机网卡"}
	}
	return externIPResult{}
}

// probeMetadata 并发查各家元数据，取第一个命中。
//
// 并发而不是串行：三家都不在的时候串行要等 3 倍超时，而启动路径上多等
// 2 秒没有任何收益 —— 元数据是「有就毫秒返回、没有就超时」的类型。
func probeMetadata(in detectInput) externIPResult {
	urls := in.MetadataURLs
	if urls == nil {
		urls = metadataEIP
	}
	if len(urls) == 0 {
		return externIPResult{}
	}
	client := in.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: metadataTimeout}
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = metadataTimeout * 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	type hit struct {
		ip    string
		label string
	}
	out := make(chan hit, len(urls))
	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func(u metadataURL) {
			defer wg.Done()
			if ip := fetchMetadataIP(ctx, client, u.url); ip != "" {
				out <- hit{ip: ip, label: u.label}
			}
		}(u)
	}
	go func() {
		wg.Wait()
		close(out)
	}()

	for h := range out {
		return externIPResult{IP: h.ip, Source: "云元数据（" + h.label + "）"}
	}
	return externIPResult{}
}

func fetchMetadataIP(ctx context.Context, client *http.Client, url string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	// 这些接口正常只会返回一行 IP；限长是为了避免被异常响应拖住。
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	return normalizeIPv4(string(body))
}

// normalizeIPv4 把候选文本收敛成合法 IPv4 字符串；不合法返回空。
//
// ⛔ 必须排掉 0.0.0.0 / 127.x / 169.254.x：元数据接口在某些环境会返回这些
// 占位值，直接写进 ini 就是把候选地址换成"连不上"。
func normalizeIPv4(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "\"'")
	if s == "" {
		return ""
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	v4 := ip.To4()
	if v4 == nil {
		return ""
	}
	if v4.IsUnspecified() || v4.IsLoopback() || v4.IsLinkLocalUnicast() {
		return ""
	}
	return v4.String()
}

// isPrivateIPv4 判断是否私有/保留地址段（RFC1918 + 100.64/10 CGNAT）。
func isPrivateIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	switch {
	case v4[0] == 10:
		return true
	case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
		return true
	case v4[0] == 192 && v4[1] == 168:
		return true
	// 100.64.0.0/10：运营商级 NAT 与部分云内网都是这一段。
	case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
		return true
	}
	return false
}

// defaultRouteIPv4 取「去外网时内核会选哪张网卡」的地址。
//
// 用 UDP connect 而不是真发包：connect 只让内核做一次路由决策，
// 不发任何报文，也不依赖外网真的可达。
func defaultRouteIPv4() string {
	conn, err := net.DialTimeout("udp4", "1.1.1.1:80", 300*time.Millisecond)
	if err == nil {
		defer func() { _ = conn.Close() }()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			if ip := normalizeIPv4(addr.IP.String()); ip != "" {
				return ip
			}
		}
	}
	return enumIPv4()
}

// enumIPv4 是 defaultRouteIPv4 的兜底：按接口名排除虚拟网卡后取第一个全局 IPv4。
//
// ⛔ 必须排掉 docker/br-/veth 这些：容器网桥会拿到 172.17.x，
// 填进 externIP 等于把浏览器指到一个只有宿主机自己认识的地址。
func enumIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualIface(ifi.Name) {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip := normalizeIPv4(ipnet.IP.String()); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func isVirtualIface(name string) bool {
	n := strings.ToLower(name)
	for _, prefix := range []string{
		"lo", "docker", "br-", "virbr", "veth", "tun", "tap",
		"zt", "wg", "kube", "flannel", "dummy", "vmnet", "utun",
	} {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// runRTExternIP 是 `uvp-setup rtc-externip` 的入口。
//
// 失败**不返回 error 也要说清楚**：跨网段对讲是附加能力，探测失败不该拦住
// 平台启动（那会把"对讲不通"升级成"平台起不来"），但也不能静默 —— 否则
// 现场只会看到"对讲点了没反应"，毫无线索。
func runRTExternIP(args []string) error {
	fs := newFlagSet("rtc-externip")
	iniPath := fs.String("ini", "", "ZLM config.ini 路径")
	explicit := fs.String("ip", "", "显式指定的对外地址（留空=自动探测）")
	dryRun := fs.Bool("dry-run", false, "只打印探测结果，不改文件")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*iniPath) == "" {
		return fmt.Errorf("--ini 必填")
	}
	resolved, err := resolvePath(*iniPath)
	if err != nil {
		return err
	}

	res := detectExternIP(detectInput{
		Explicit: *explicit,
		LocalIP:  defaultRouteIPv4,
	})
	if res.IP == "" {
		warn("没探测到本机对外地址（网卡上没有可用 IPv4）—— 跨网段对讲需手工设置 rtc.externIP")
		return nil
	}
	note("WebRTC 对外地址 %s（来源：%s）", res.IP, res.Source)
	if *dryRun {
		return nil
	}

	old, changed, err := syncExternIPIni(resolved, res.IP)
	if err != nil {
		return err
	}
	if !changed {
		note("rtc.externIP 已是该值，未改动")
		return nil
	}
	if old == "" {
		note("已写入 rtc.externIP=%s", res.IP)
	} else {
		// ⛔ 覆盖了旧值必须说出来：页面里手填过一个地址的人，重启后看到
		//   "自己改的值又变了"但没有任何提示，只会认为是程序在乱改配置。
		//   想钉死自己的值就写到 config.env 的 UVP_ZLM_EXTERN_IP。
		note("已更新 rtc.externIP：%s → %s（如需固定，请设 config.env 的 UVP_ZLM_EXTERN_IP）", old, res.IP)
	}
	return nil
}

// syncExternIPIni 把 ip 写进 config.ini 的 [rtc] externIP，返回旧值与是否改动。
//
// ⛔ 按段落定位（不能用 ini 解析库：这份 ini 有重名段 [general]）；
//   键不存在时**报错而不是新增** —— 新增会掩盖"配置文件根本不是我们以为的那份"。
func syncExternIPIni(path, ip string) (old string, changed bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("读取 ZLM config.ini 失败: %w", err)
	}
	eol := detectEOL(string(raw))
	lines := splitLines(string(raw), eol)

	old, found := readINIScalar(lines, rtcSection, rtcExternIPKey)
	if !found {
		return "", false, fmt.Errorf("ZLM config.ini 里没有 [%s] %s（不新建键，避免写错文件）", rtcSection, rtcExternIPKey)
	}
	if old == ip {
		return old, false, nil
	}
	ok, err := replaceINIScalar(lines, rtcSection, rtcExternIPKey, ip)
	if err != nil {
		return old, false, err
	}
	if !ok {
		return old, false, fmt.Errorf("ZLM config.ini 里没有 [%s] %s（不新建键，避免写错文件）", rtcSection, rtcExternIPKey)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, eol)), 0o644); err != nil {
		return old, false, fmt.Errorf("写入 ZLM config.ini 失败: %w", err)
	}
	return old, true, nil
}

// readINIScalar 读 [section] 段内 key 的值（不跨段、不解析转义）。
func readINIScalar(lines []string, section, key string) (string, bool) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "["+section+"]" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			return "", false
		}
		m := iniKeyRe.FindStringSubmatch(lines[i])
		if m == nil || m[2] != key {
			continue
		}
		idx := strings.Index(lines[i], "=")
		if idx < 0 {
			return "", false
		}
		return strings.TrimSpace(lines[i][idx+1:]), true
	}
	return "", false
}
