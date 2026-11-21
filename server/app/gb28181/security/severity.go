package security

import "strings"

// EventSeverity 是风险事件的展示等级。
//
// ⛔ 这份判定是**筛选真源**：台账页的「风险等级」下拉走服务端过滤，如果只把规则
// 写在 web/src/views/gb28181/security/securityFormatting.ts，翻页时会出现
// 「筛高危却翻出中危」的错位。前端显示等级也优先取后端下发的 severity，本地判定
// 只作为快照（/snapshot 不带 severity）的兜底。
//
// ⛔ 改这里的规则必须同步 isHighRiskSecurityReason（前端兜底），并跑
// security/severity_test.go 与 securityFormatting.test.ts 两边的用例。
type EventSeverity string

const (
	SeverityHigh   EventSeverity = "high"
	SeverityMedium EventSeverity = "medium"
	SeverityLow    EventSeverity = "low"
)

// EventSeverityFor 与前端 mapEvent 的判据保持一致：
// 已封禁或命中高危原因 → 高危；已拒绝 → 中危；其余（已记录/放行等）→ 低危。
func EventSeverityFor(action Action, reason Reason) EventSeverity {
	if action == ActionBan || IsHighRiskReason(string(reason)) {
		return SeverityHigh
	}
	if action == ActionDrop {
		return SeverityMedium
	}
	return SeverityLow
}

// IsHighRiskReason 覆盖"设备 ID 枚举"以及鉴权类（digest/nonce）失败。
func IsHighRiskReason(reason string) bool {
	return reason == string(ReasonRegisterEnumeration) ||
		strings.Contains(reason, "nonce") ||
		strings.Contains(reason, "digest")
}

// Valid 用于校验外部传入的 severity 参数，避免把拼错的等级静默当成"不过滤"。
func (s EventSeverity) Valid() bool {
	switch s {
	case "", SeverityHigh, SeverityMedium, SeverityLow:
		return true
	default:
		return false
	}
}
