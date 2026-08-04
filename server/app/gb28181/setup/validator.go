package setup

import (
	"fmt"
	"net"
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
//   - 长度 >= 12
//   - 至少 3 类字符(大写/小写/数字/特殊)
//   - 不在常见弱口令黑名单
//
// 原因:GB28181 SIP 注册暴力破解频发,弱口令是主要入口.
var commonWeakPasswords = map[string]struct{}{
	"12345678":     {},
	"123456789":    {},
	"1234567890":   {},
	"12345678901":  {},
	"123456789012": {},
	"password":     {},
	"passw0rd":     {},
	"password123":  {},
	"qwerty":       {},
	"qwerty123":    {},
	"admin":        {},
	"admin123":     {},
	"admin1234":    {},
	"administrator": {},
	"root":         {},
	"root123":      {},
	"sipserver":    {},
	"gb28181":      {},
	"12345678a":    {},
	"abc12345":     {},
	"111111111111": {},
	"000000000000": {},
	"aaaaaaaaaaaa": {},
	"iloveyou":     {},
}

func checkPasswordStrength(password string) string {
	if len(password) < 12 {
		return "长度至少 12 位"
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
	if len(password) < 12 {
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
