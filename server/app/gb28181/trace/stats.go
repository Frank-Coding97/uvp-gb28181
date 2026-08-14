package trace

// SessionStats is the aggregate used by the SIP trace workbench cards.
type SessionStats struct {
	Total        uint64 `json:"total"`
	Anomaly      uint64 `json:"anomaly"`
	RegisterFail uint64 `json:"registerFail"`
	PlayStuck    uint64 `json:"playStuck"`
	// InvitePending is kept temporarily for API compatibility. It mirrors PlayStuck.
	InvitePending uint64 `json:"invitePending"`
	// Truncated 为 true 时统计基于候选上限内的样本,非全量精确计数
	Truncated bool `json:"truncated"`
}
