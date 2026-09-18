package playauth

import gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"

// Compare immutable command inputs only; result and scheduling state may
// legitimately advance while a private recovery receipt is being retained.
func samePTZCommand(a, b gbmodels.GbPTZOperation) bool {
	triggerEqual := a.TriggerOperationID == nil && b.TriggerOperationID == nil ||
		a.TriggerOperationID != nil && b.TriggerOperationID != nil && *a.TriggerOperationID == *b.TriggerOperationID
	return a.OperationID == b.OperationID && a.IdempotencyKey == b.IdempotencyKey &&
		a.DeviceID == b.DeviceID && a.DeviceCode == b.DeviceCode && a.ChannelID == b.ChannelID && a.ChannelCode == b.ChannelCode &&
		a.CmdType == b.CmdType && a.Action == b.Action && a.PayloadJSON == b.PayloadJSON && a.SN == b.SN &&
		a.ProfileVersion == b.ProfileVersion && a.ProfileCharset == b.ProfileCharset &&
		a.TargetScope == b.TargetScope && a.TargetCode == b.TargetCode && a.ScopeKey == b.ScopeKey &&
		a.ResponseRequired == b.ResponseRequired && a.MaxAttempts == b.MaxAttempts &&
		a.ActorID == b.ActorID && a.ActorDeptID == b.ActorDeptID && triggerEqual
}
