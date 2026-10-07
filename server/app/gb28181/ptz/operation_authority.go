package ptz

import (
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

// RequiresDeviceAuthorization reports whether operations need durable epoch bindings.
func (s *Service) RequiresDeviceAuthorization() bool {
	return s != nil && (s.intents != nil || s.barrier != nil)
}

func requiresPTZIntent(op gbmodels.GbPTZOperation) bool {
	return op.DeviceEpoch != nil || op.DeviceIntentID != nil
}

func targetRequiresPTZIntent(target Target) bool {
	return target.DeviceEpoch != 0
}

func operationMatchesTarget(op gbmodels.GbPTZOperation, target Target) bool {
	if !targetRequiresPTZIntent(target) && !requiresPTZIntent(op) {
		return true
	}
	return op.DeviceEpoch != nil && op.DeviceIntentID != nil && target.DeviceEpoch > 0 && *op.DeviceEpoch == target.DeviceEpoch &&
		op.DeviceID == target.DeviceID && op.DeviceCode == target.DeviceCode &&
		((op.TargetScope == gbmodels.ControlTargetScopeDevice && op.TargetCode == target.DeviceCode) ||
			(op.TargetScope != gbmodels.ControlTargetScopeDevice && op.ChannelID == target.ChannelID && op.ChannelCode == target.ChannelCode))
}

func ptzIntentIdentity(op gbmodels.GbPTZOperation) (playauth.DeviceOperationIntentIdentity, error) {
	if op.DeviceEpoch == nil || *op.DeviceEpoch <= 0 || op.DeviceIntentID == nil || *op.DeviceIntentID == "" {
		return playauth.DeviceOperationIntentIdentity{}, playauth.ErrDeviceIntentUnavailable
	}
	scope, targetPK, code, err := playauth.PTZIntentTarget(op)
	if err != nil {
		return playauth.DeviceOperationIntentIdentity{}, err
	}
	return playauth.DeviceOperationIntentIdentity{OperationID: *op.DeviceIntentID, DevicePK: int64(op.DeviceID), DeviceCode: op.DeviceCode,
		DeviceEpoch: *op.DeviceEpoch, TargetScope: scope, TargetPK: targetPK, TargetCode: code, Kind: "ptz"}, nil
}
