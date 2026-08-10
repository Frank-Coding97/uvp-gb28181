package trace

// SessionStats is the aggregate used by the SIP trace workbench cards.
type SessionStats struct {
	Total         uint64 `json:"total"`
	Anomaly       uint64 `json:"anomaly"`
	RegisterFail  uint64 `json:"registerFail"`
	InvitePending uint64 `json:"invitePending"`
}
