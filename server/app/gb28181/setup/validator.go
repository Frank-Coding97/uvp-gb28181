package setup

import (
	"fmt"
	"net"
	"strings"
)

type ValidationError struct {
	Fields map[string]string `json:"fields"`
	cause  error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid SIP configuration: %d field(s)", len(e.Fields))
}

func (e *ValidationError) Unwrap() error { return e.cause }

func ValidateSIPConfigRequest(req SaveSIPConfigRequest, hasExistingPassword bool) error {
	fields := make(map[string]string)
	if !req.DeploymentMode.Valid() {
		fields["deploymentMode"] = "must be lan or public"
	}
	if !validIPv4(req.ListenIP, true) {
		fields["listenIp"] = "must be a valid IPv4 address"
	}
	allowDynamicAdvertise := req.DeploymentMode == DeploymentLAN && req.ListenIP == wildcardIPv4 && req.AdvertiseIP == ""
	if !allowDynamicAdvertise && !validIPv4(req.AdvertiseIP, false) {
		fields["advertiseIp"] = "must be a concrete non-loopback IPv4 address"
	}
	if req.HookIP != "" && !validMediaIP(req.HookIP) {
		fields["hookIp"] = "must be a concrete IP address"
	}
	// SDP IP是设备往回推RTP 的目标地址,必填且必须是设备可达的具体地址。
	//
	// ⛔⛔ 为什么这里必须拒绝回环/未指定地址,而不是像 stream_ip 那样"留空沿用现有策略":
	// 该字段失效的表征是**信令全成功但没有画面** —— INVITE 被接受、SDP 正常协商、
	// 10 秒后 play_timeout、ZLM 侧零 RTP 包。现场根本看不出是配置问题。
	// 所以宁可保存时报错,也不让用户走到那一步。参考 wvp-GB28181-pro:
	// 它的 sdp-ip 兜底是 `media.ip`(裸机 127.0.0.1 / Docker 容器名),
	// 两种部署下都不可用 —— 差别只是"填了才对",填错的表现和我们一样静默。
	if reason := sdpIPRejectionReason(req.SDPIP); reason != "" {
		fields["sdpIp"] = reason
	}
	if req.StreamIP != "" && !validMediaHost(req.StreamIP) {
		fields["streamIp"] = "must be an IP address or domain without scheme, port or path"
	}
	if req.Port < 1 || req.Port > 65535 {
		fields["port"] = "must be between 1 and 65535"
	}
	if len(req.ServerID) != 20 || !asciiDigits(req.ServerID) {
		fields["serverId"] = "must contain exactly 20 digits"
	}
	if len(req.Domain) != 10 || !asciiDigits(req.Domain) {
		fields["domain"] = "must contain exactly 10 digits"
	}
	var cause error
	if req.Password == nil {
		// nil = 前端不改密码.只有首次配置 (hasExistingPassword=false) 时才要求必填.
		if !hasExistingPassword {
			fields["password"] = "is required"
			cause = ErrSIPPasswordRequired
		}
	} else if reason := checkPasswordStrength(*req.Password); reason != "" {
		fields["password"] = reason
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields, cause: cause}
	}
	return nil
}

func validIPv4(value string, allowWildcard bool) bool {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil || value != ip.To4().String() {
		return false
	}
	if ip.IsUnspecified() {
		return allowWildcard
	}
	return !ip.IsLoopback()
}

func asciiDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// checkPasswordStrength 返回空字符串表示密码合规,否则返回给前端展示的原因.
// 规则:
//   - 长度 >= 8
//   - 至少 3 类字符(大写/小写/数字/特殊)
//   - 不在常见弱口令黑名单
//
// 原因:GB28181 SIP 注册暴力破解频发,弱口令是主要入口.
var commonWeakPasswords = map[string]struct{}{
	"12345678":      {},
	"123456789":     {},
	"1234567890":    {},
	"12345678901":   {},
	"123456789012":  {},
	"password":      {},
	"passw0rd":      {},
	"password123":   {},
	"qwerty":        {},
	"qwerty123":     {},
	"admin":         {},
	"admin123":      {},
	"admin1234":     {},
	"administrator": {},
	"root":          {},
	"root123":       {},
	"sipserver":     {},
	"gb28181":       {},
	"12345678a":     {},
	"abc12345":      {},
	"111111111111":  {},
	"000000000000":  {},
	"aaaaaaaaaaaa":  {},
	"iloveyou":      {},
}

func checkPasswordStrength(password string) string {
	if len(password) < 8 {
		return "长度至少 8 位"
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for i := 0; i < len(password); i++ {
		c := password[i]
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		case c > 32 && c < 127:
			hasSpecial = true
		}
	}
	categories := 0
	if hasUpper {
		categories++
	}
	if hasLower {
		categories++
	}
	if hasDigit {
		categories++
	}
	if hasSpecial {
		categories++
	}
	if categories < 3 {
		return "需同时包含大写字母、小写字母、数字、特殊字符中的至少 3 类"
	}
	if _, banned := commonWeakPasswords[password]; banned {
		return "该密码在常见弱口令列表中,SIP 注册接口易被暴力破解"
	}
	// 检测简单顺序: 12345678..., abcdefgh... 之类
	if isSequential(password) {
		return "密码过于规律,请使用更随机的组合"
	}
	return ""
}

// isSequential 判断是否为纯顺序序列 (12345678... 或 abcdefgh...).
// 只当整个密码由单一顺序构成时返回 true,不影响随机密码里偶然出现的短顺序片段.
func isSequential(password string) bool {
	if len(password) < 8 {
		return false
	}
	ascending := true
	descending := true
	for i := 1; i < len(password); i++ {
		if password[i] != password[i-1]+1 {
			ascending = false
		}
		if password[i] != password[i-1]-1 {
			descending = false
		}
		if !ascending && !descending {
			return false
		}
	}
	return true
}

func validMediaIP(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && !ip.IsUnspecified() && !ip.IsMulticast()
}

// sdpIPRejectionReason 返回空字符串表示 sdpIp 可用,否则返回给用户看的原因。
//
// 三类拒绝,分别对应三种"配了但设备推不上流"的形态:
//
//   - 空:字段必填,不填就没有可下发给设备的收流地址。
//   - 回环 (127.0.0.0/8、::1)/ 未指定 (0.0.0.0、::):设备拿到后会把 RTP 推向
//     平台自己或广播地址,必然收不到流。
//   - 单一标签主机名(如 Docker 容器名 polaris-media):设备侧无法解析,
//     连"推向自己"都做不到,直接丢弃。
//
// 刻意接受的是**多标签域名**(如 media.example.com):GB28181 允许 SDP 写域名,
// WVP 也支持(`MediaConfig.getSdpIp` 会做 DNS 解析)。
func sdpIPRejectionReason(value string) string {
	host := strings.TrimSpace(value)
	if host == "" {
		return "is required:设备按此地址回推RTP 流,不填则无法点播"
	}
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsUnspecified():
			return "must not be 0.0.0.0 or :: —设备无法向该地址回推RTP 流"
		case ip.IsLoopback():
			return "must not be a loopback address such as 127.0.0.1 —设备会把RTP 推向平台自身,导致注册成功但没有画面"
		}
		return ""
	}
	if len(strings.Split(strings.TrimSuffix(host, "."), ".")) < 2 {
		return "must be an IP address or a resolvable domain, not a single host label such as a container name —设备无法解析该名称"
	}
	if !validMediaHost(host) {
		return "must be an IP address or domain without scheme, port or path"
	}
	return ""
}

func validMediaHost(value string) bool {
	if net.ParseIP(value) != nil {
		return validMediaIP(value)
	}
	if len(value) > 253 || value == "" {
		return false
	}
	value = strings.TrimSuffix(value, ".")
	if value == "" {
		return false
	}
	if asciiDigits(strings.ReplaceAll(value, ".", "")) {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
