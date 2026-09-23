package traffic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
)

type NodeUUIDResolver interface {
	IDForUUID(string) (int64, bool)
}

type FlowService struct {
	repo        *GormRepository
	attribution *AttributionResolver
	nodes       NodeUUIDResolver
	now         func() time.Time
}

func NewFlowService(repo *GormRepository, attribution *AttributionResolver, nodes NodeUUIDResolver, now func() time.Time) *FlowService {
	if now == nil {
		now = time.Now
	}
	return &FlowService{repo: repo, attribution: attribution, nodes: nodes, now: now}
}

func (s *FlowService) CollectFlow(ctx context.Context, report handler.FlowReport) error {
	if s == nil || s.repo == nil || s.attribution == nil || s.nodes == nil {
		return errors.New("traffic flow service unavailable")
	}
	nodeID, ok := s.nodes.IDForUUID(report.MediaServerID)
	if !ok {
		return fmt.Errorf("unknown media server: %s", report.MediaServerID)
	}
	attribution, err := s.attribution.Resolve(nodeID, report.App, report.Stream)
	if err != nil {
		return err
	}
	direction := DirectionUpstream
	if report.Player {
		direction = DirectionDownstream
	}
	businessKey := ""
	if !report.Player {
		businessKey, ok, err = s.repo.ActiveBusinessKey(ctx, nodeID, report.App, report.Stream, direction)
		if err != nil {
			return err
		}
	}
	if businessKey == "" {
		if report.ID == "" {
			return errors.New("flow report session id 不能为空")
		}
		businessKey = fmt.Sprintf("flow:%d:%s:%s", nodeID, report.ID, direction)
	}
	at := s.now().UTC()
	_, err = s.repo.Apply(ctx, ApplyRequest{
		BusinessKey: businessKey, NodeID: nodeID, MediaServerUUID: report.MediaServerID, ZLMSessionID: report.ID,
		Direction: direction, DeviceCode: attribution.DeviceCode, ChannelCode: attribution.ChannelCode,
		OwnerDeptID: attribution.OwnerDeptID, MediaKind: attribution.MediaKind,
		Schema: report.Schema, VHost: report.VHost, App: report.App, Stream: report.Stream,
		AbsoluteBytes: report.TotalBytes, At: at, SettleAt: at, DurationSeconds: report.Duration,
	})
	return err
}
