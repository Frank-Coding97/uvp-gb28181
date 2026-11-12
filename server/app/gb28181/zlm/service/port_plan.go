package service

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// 部署端口规划文件（config/config.env）里管 WebRTC 的两个键。
//
// ⛔⛔ 端口配置的真实来源在部署包，不在 ZLM 的 config.ini：`uvp-gb28181-ctl.sh`
// 每次启动都会按这两个值重写 config.ini 里的 [rtc] 段（port / tcpPort 取同一个
// 值，icePort / iceTcpPort 取同一个值）。所以「页面上能改端口」的前提是**把新值
// 写回这里**，否则下一次启动就被改回去 —— 那是假改，而且用户会以为自己改过。
const (
	portPlanRTCKey = "UVP_ZLM_RTC_PORT"
	portPlanICEKey = "UVP_ZLM_ICE_PORT"

	// 用户显式改过端口的标记前缀。ctl.sh 的 port_is_user_set() 认这条（判据①），
	// 另一条判据是「值 ≠ 上次落盘基线」—— 但老包没有基线文件，只靠②会漏，
	// 下次启动被默认值覆盖且不留痕迹。
	portPlanMarkerPrefix = "UVP_USER_SET_"
)

var (
	// ErrPortPlanUnavailable 找不到/无法使用部署端口规划文件（例如容器编排由外部注入端口）。
	ErrPortPlanUnavailable = errors.New("port_plan_unavailable")

	// ErrPortPlanPersistFailed 端口已下发给 ZLM，但写回端口规划失败。
	// 这种状态下重启会退回旧端口，必须让调用方看到，不能当成功。
	ErrPortPlanPersistFailed = errors.New("port_plan_persist_failed")
)

// PortPlan 把「页面上改的端口」落盘到部署端口规划。
//
// 只认 config.env 的行首 `KEY=VALUE`（与 ctl.sh 的 sed 读取方式一致），其余行
// 原样保留 —— 这个文件里有 SIP、Redis、HTTPS 等一堆别的键，重写丢一行就是事故。
type PortPlan struct {
	path string
}

// NewPortPlan envPath 为 config.env 的绝对路径。
func NewPortPlan(envPath string) *PortPlan { return &PortPlan{path: envPath} }

// Path 端口规划文件路径（报错与日志用）。
func (p *PortPlan) Path() string {
	if p == nil {
		return ""
	}
	return p.path
}

// PortConflict 新端口与端口规划里别的键撞车。
type PortConflict struct {
	Port  int
	Key   string
	Value string
}

// FindConflict 检查新端口是否已被端口规划里的其它键占用（0 表示该项不改）。
//
// ⛔ 必须在**下发 ZLM 之前**调用：端口规划是重启时真正生效的那份配置，
// 撞车不会在保存时炸，而是下次启动 `Listen on ... failed: address already in use`
// ⇒ ZLM 直接退出。用户当时看到的却是"保存成功"。
//
// 两类键都要看：
//   - `*_PORT=51004` 单端口；
//   - `*_RANGE=51014-51063` 段（rtc.port 填进 RTP 收流段是最容易犯的一种）。
func (p *PortPlan) FindConflict(rtcPort, icePort int) (*PortConflict, error) {
	if p == nil || p.path == "" {
		return nil, fmt.Errorf("%w: 未配置端口规划文件路径", ErrPortPlanUnavailable)
	}
	raw, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: 未找到 config/config.env —— 当前部署方式（如容器编排）不支持在页面改端口，请改部署侧的端口规划", ErrPortPlanUnavailable)
		}
		return nil, fmt.Errorf("%w: %v", ErrPortPlanPersistFailed, err)
	}
	targets := make([]int, 0, 2)
	for _, port := range []int{rtcPort, icePort} {
		if port > 0 {
			targets = append(targets, port)
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || strings.HasPrefix(key, portPlanMarkerPrefix) {
			continue
		}
		// 只跳过**本次要覆盖**的键。反例：只改 rtc.port 时 UVP_ZLM_ICE_PORT
		// 仍是有效约束 —— 把媒体端口填成 STUN 端口就是同协议同端口，ZLM 起不来。
		if (rtcPort > 0 && key == portPlanRTCKey) || (icePort > 0 && key == portPlanICEKey) {
			continue
		}
		for _, port := range targets {
			if portInPlanValue(key, value, port) {
				return &PortConflict{Port: port, Key: key, Value: value}, nil
			}
		}
	}
	return nil, nil
}

// portInPlanValue 判断某个端口是否落在这条规划键的取值里。
func portInPlanValue(key, value string, port int) bool {
	switch {
	case strings.HasSuffix(key, "_PORT"):
		n, err := strconv.Atoi(strings.TrimSpace(value))
		return err == nil && n == port
	case strings.HasSuffix(key, "_RANGE"):
		// 解析规则只留一份（node.ParseRTPPortRange），别在这里再写一遍。
		start, end, ok := node.ParseRTPPortRange(value)
		return ok && port >= start && port <= end
	default:
		return false
	}
}

// PersistWebRTCPorts 把 WebRTC 媒体端口与 STUN 端口写回端口规划，传 0 表示该项不变。
//
// 写入顺序刻意是「ZLM 回读成功之后」（见 ConfigService.Update）：先证明 ZLM 接受
// 了新值，再让它成为下次启动的规划；反过来会留下「规划改了、ZLM 没接受」的
// 状态，重启后端口变了但用户以为没改成功，更难排查。
func (p *PortPlan) PersistWebRTCPorts(rtcPort, icePort int) error {
	if p == nil || p.path == "" {
		return fmt.Errorf("%w: 未配置端口规划文件路径", ErrPortPlanUnavailable)
	}
	values := map[string]string{}
	if rtcPort > 0 {
		values[portPlanRTCKey] = strconv.Itoa(rtcPort)
	}
	if icePort > 0 {
		values[portPlanICEKey] = strconv.Itoa(icePort)
	}
	if len(values) == 0 {
		return nil
	}

	raw, err := os.ReadFile(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: 未找到 config/config.env —— 当前部署方式（如容器编排）不支持在页面改端口，请改部署侧的端口规划", ErrPortPlanUnavailable)
		}
		return fmt.Errorf("%w: %v", ErrPortPlanPersistFailed, err)
	}

	next := rewritePortPlan(string(raw), values)
	if next == string(raw) {
		// 值没变化（例如把端口改回原值再保存），不碰文件 mtime。
		return nil
	}

	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(p.path); statErr == nil {
		mode = info.Mode().Perm()
	}
	// 同目录临时文件 + rename：ctl.sh 可能在启动时同时读这个文件，
	// 直接截断重写会让它读到半个文件。
	tmp := p.path + ".tmp-portplan"
	if err := os.WriteFile(tmp, []byte(next), mode); err != nil {
		return fmt.Errorf("%w: 写入临时文件失败: %v", ErrPortPlanPersistFailed, err)
	}
	if err := os.Rename(tmp, p.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("%w: 替换文件失败: %v", ErrPortPlanPersistFailed, err)
	}
	return nil
}

// rewritePortPlan 只改给定 KEY 的行并补「用户改过」标记，其余行逐字保留。
//
// 键不存在时**追加**而不是静默跳过：跳过等于"看起来改了、其实没落盘"，
// 下次启动照样回退，且页面上毫无痕迹。
func rewritePortPlan(raw string, values map[string]string) string {
	lines := strings.Split(raw, "\n")
	// Split 总会给出一个尾部空串（文件以换行结尾时）。先摘掉，最后统一补一个换行，
	// 否则每保存一次就多出一行空行。
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	seen := make(map[string]bool, len(values))
	marked := make(map[string]bool, len(values))
	out := make([]string, 0, len(lines)+2*len(values))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		replaced := false
		for key, value := range values {
			if strings.HasPrefix(trimmed, key+"=") {
				out = append(out, key+"="+value)
				seen[key] = true
				replaced = true
				break
			}
			if strings.HasPrefix(trimmed, portPlanMarkerPrefix+key+"=") {
				marked[key] = true
			}
		}
		if !replaced {
			out = append(out, line)
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !seen[key] {
			out = append(out, key+"="+values[key])
		}
	}
	for _, key := range keys {
		if !marked[key] {
			out = append(out, portPlanMarkerPrefix+key+"=1")
		}
	}
	return strings.Join(out, "\n") + "\n"
}
