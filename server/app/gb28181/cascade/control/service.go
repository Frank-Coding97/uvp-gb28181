package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
)

var (
	ErrFeatureUnavailable  = errors.New("cascade control: feature unavailable")
	ErrControlUnauthorized = errors.New("cascade control: unauthorized")
	ErrInvalidControl      = errors.New("cascade control: invalid request")
)

type ProjectionStore interface {
	ProjectionSnapshot(context.Context, uint64) (*repository.ProjectionSnapshot, error)
}

type TargetLoader interface {
	Load(context.Context, uint64, uint64) (ptz.Target, error)
}

type Executor interface {
	Execute(context.Context, ptz.Target, ptz.Command) (gbmodels.GbPTZOperation, error)
}

type ForwardRequest struct {
	PlatformID uint64
	CallID     string
	Body       []byte
}

type TransportState string

const (
	TransportPending  TransportState = "pending"
	TransportAccepted TransportState = "accepted"
	TransportRejected TransportState = "rejected"
	TransportTimeout  TransportState = "timeout"
)

type BusinessState string

const (
	BusinessPending BusinessState = "pending"
	BusinessOK      BusinessState = "ok"
	BusinessError   BusinessState = "error"
	BusinessUnknown BusinessState = "unknown"
)

type ForwardResult struct {
	OperationID      string
	Action           string
	CallID           string
	UpstreamSN       int
	PublishedChannel string
	Transport        TransportState
	Business         BusinessState
}

type Service struct {
	projections ProjectionStore
	targets     TargetLoader
	executor    Executor
}

func NewService(projections ProjectionStore, targets TargetLoader, executor Executor) *Service {
	return &Service{projections: projections, targets: targets, executor: executor}
}

func (s *Service) Forward(ctx context.Context, request ForwardRequest) (ForwardResult, error) {
	if s == nil || s.projections == nil || s.targets == nil || s.executor == nil {
		return ForwardResult{}, ErrFeatureUnavailable
	}
	callID := strings.TrimSpace(request.CallID)
	if request.PlatformID == 0 || callID == "" || len(request.Body) == 0 {
		return ForwardResult{}, ErrInvalidControl
	}
	snapshot, err := s.projections.ProjectionSnapshot(ctx, request.PlatformID)
	if err != nil {
		return ForwardResult{}, err
	}
	if snapshot == nil || snapshot.Platform.ID != request.PlatformID || !snapshot.Platform.Enabled {
		return ForwardResult{}, fmt.Errorf("%w: platform disabled or unavailable", ErrControlUnauthorized)
	}
	if !snapshot.Platform.PTZEnabled {
		return ForwardResult{}, ErrFeatureUnavailable
	}

	control, _, _, target, err := s.resolveForwardTarget(ctx, snapshot, request)
	if err != nil {
		return ForwardResult{}, err
	}
	command := s.buildForwardCommand(request, callID, control, target)
	operation, err := s.executor.Execute(ctx, target, command)
	if err != nil {
		return ForwardResult{}, err
	}
	result := resultFromOperation(operation)
	result.Action = control.Command.Action
	result.CallID = callID
	result.UpstreamSN = control.SN
	result.PublishedChannel = control.DeviceID
	return result, nil
}

// resolveForwardTarget 解析上游 PTZ 指令并定位下游设备/通道与发送目标
func (s *Service) resolveForwardTarget(ctx context.Context, snapshot *repository.ProjectionSnapshot, request ForwardRequest) (manscdp.PTZControl, model.GbCascadeChannelProjection, model.GbCascadeDeviceProjection, ptz.Target, error) {
	var control manscdp.PTZControl
	upstreamProfile := protocol.ProfileFor(protocol.Version(snapshot.Platform.EffectiveVersion))
	parsed, err := manscdp.ParsePTZControlWithProfile(upstreamProfile, request.Body)
	if err != nil {
		if errors.Is(err, manscdp.ErrUnsupportedPTZInstruction) {
			return control, model.GbCascadeChannelProjection{}, model.GbCascadeDeviceProjection{}, ptz.Target{}, fmt.Errorf("%w: %v", ErrFeatureUnavailable, err)
		}
		return control, model.GbCascadeChannelProjection{}, model.GbCascadeDeviceProjection{}, ptz.Target{}, fmt.Errorf("%w: %v", ErrInvalidControl, err)
	}
	control = parsed
	channel, device, ok := resolveProjection(snapshot, control.DeviceID)
	if !ok {
		return control, channel, device, ptz.Target{}, fmt.Errorf("%w: shared channel %q not found", ErrControlUnauthorized, control.DeviceID)
	}
	if !channel.PTZAllowed {
		return control, channel, device, ptz.Target{}, fmt.Errorf("%w: channel %q PTZ permission disabled", ErrControlUnauthorized, control.DeviceID)
	}
	target, err := s.targets.Load(ctx, device.SourceDeviceID, channel.SourceChannelID)
	if err != nil {
		return control, channel, device, ptz.Target{}, err
	}
	return control, channel, device, target, nil
}

// buildForwardCommand 把上游指令转成下游 PTZ 命令
func (s *Service) buildForwardCommand(request ForwardRequest, callID string, control manscdp.PTZControl, target ptz.Target) ptz.Command {
	raw := control.Command.Raw
	profile := target.Profile
	return ptz.Command{
		CmdType:        manscdp.CmdDeviceControl,
		Action:         "cascade_" + control.Command.Action,
		IdempotencyKey: fmt.Sprintf("cascade:%d:%s:%d", request.PlatformID, callID, control.SN),
		Payload: map[string]interface{}{
			"platformId": request.PlatformID, "callId": callID, "upstreamSn": control.SN,
			"publishedChannelId": control.DeviceID, "rawPtz": raw,
		},
		Profile: profile,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildRawPTZControlWithProfile(profile, target.ChannelCode, sn, raw)
		},
	}
}

func resolveProjection(snapshot *repository.ProjectionSnapshot, publishedChannelID string) (
	model.GbCascadeChannelProjection, model.GbCascadeDeviceProjection, bool,
) {
	var channel model.GbCascadeChannelProjection
	for _, candidate := range snapshot.Channels {
		if candidate.PlatformID == snapshot.Platform.ID && candidate.Active && candidate.PublishedChannelID == publishedChannelID {
			channel = candidate
			break
		}
	}
	if channel.SourceChannelID == 0 {
		return model.GbCascadeChannelProjection{}, model.GbCascadeDeviceProjection{}, false
	}
	for _, candidate := range snapshot.Devices {
		if candidate.PlatformID == snapshot.Platform.ID && candidate.Active && candidate.ID == channel.DeviceProjectionID {
			return channel, candidate, true
		}
	}
	return model.GbCascadeChannelProjection{}, model.GbCascadeDeviceProjection{}, false
}

func resultFromOperation(operation gbmodels.GbPTZOperation) ForwardResult {
	result := ForwardResult{OperationID: operation.OperationID, Transport: TransportPending, Business: BusinessPending}
	switch operation.Status {
	case gbmodels.PTZOperationSent:
		result.Transport = TransportAccepted
	case gbmodels.PTZOperationAccepted:
		result.Transport = TransportAccepted
		result.Business = BusinessOK
	case gbmodels.PTZOperationRejected, gbmodels.PTZOperationCancelled:
		result.Transport = TransportRejected
		result.Business = BusinessError
	case gbmodels.PTZOperationTimeout:
		result.Transport = TransportTimeout
		result.Business = BusinessUnknown
	case gbmodels.PTZOperationUnknown:
		result.Business = BusinessUnknown
	}
	return result
}
