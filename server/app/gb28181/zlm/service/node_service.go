package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ErrNodeNotFound 节点不存在
var ErrNodeNotFound = errors.New("node not found")

// ErrNodeNotInMaintenance 删除节点前必须先进维护态
var ErrNodeNotInMaintenance = errors.New("node must be in maintenance state to delete")

// ZLMProbe 节点连通性探测 + 配置下发抽象
// 由 zlm.Client 实现(适配器在 bootstrap 注入),测试用 mock。
type ZLMProbe interface {
	// GetServerConfig 探测连通性 + 拿当前 ZLM 配置
	GetServerConfig(ctx context.Context, n *node.Node) (map[string]string, error)
	// ApplyConfigForNode 写 Hook + mediaServerId 等
	ApplyConfigForNode(ctx context.Context, n *node.Node, t MediaTuning) error
}

// MediaTuning 平台级 hook / 媒体调优参数(从 yaml gb28181.media.* 来)
// 跟 gbconfig.MediaConfig 字段一致,但解耦避免 service 直依赖 config 包。
type MediaTuning struct {
	HookHost                string
	HookPort                int
	StreamNoneReaderTimeout int
	RTPServerTimeout        int
}

// NodeDTO 对外暴露的节点视图(剥掉 secret)
type NodeDTO struct {
	ID              int64             `json:"id"`
	Name            string            `json:"name"`
	Host            string            `json:"host"`
	APIPort         int               `json:"apiPort"`
	MediaServerUUID string            `json:"mediaServerUUID"`
	Weight          int               `json:"weight"`
	Tags            map[string]string `json:"tags,omitempty"`
	State           node.State        `json:"state"`
	RTPPortStart    int               `json:"rtpPortStart"`
	RTPPortEnd      int               `json:"rtpPortEnd"`
	Stats           node.Stats        `json:"stats"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

// CreateNodeReq 新建节点入参
type CreateNodeReq struct {
	Name         string            `json:"name" binding:"required"`
	Host         string            `json:"host" binding:"required"`
	APIPort      int               `json:"apiPort" binding:"required"`
	APISecret    string            `json:"apiSecret" binding:"required"`
	Weight       int               `json:"weight"`
	Tags         map[string]string `json:"tags"`
	RTPPortStart int               `json:"rtpPortStart"`
	RTPPortEnd   int               `json:"rtpPortEnd"`
}

// UpdateNodeReq 更新节点入参(可选字段用指针)
type UpdateNodeReq struct {
	Name         *string           `json:"name,omitempty"`
	APISecret    *string           `json:"apiSecret,omitempty"`
	Weight       *int              `json:"weight,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	RTPPortStart *int              `json:"rtpPortStart,omitempty"`
	RTPPortEnd   *int              `json:"rtpPortEnd,omitempty"`
}

// NodeService 节点 CRUD + 状态切换
type NodeService struct {
	registry *node.Registry
	probe    ZLMProbe
	tuning   MediaTuning
}

// NewNodeService 构造
func NewNodeService(reg *node.Registry, probe ZLMProbe, tuning MediaTuning) *NodeService {
	return &NodeService{registry: reg, probe: probe, tuning: tuning}
}

func toDTO(n *node.Node) *NodeDTO {
	if n == nil {
		return nil
	}
	return &NodeDTO{
		ID:              n.ID,
		Name:            n.Name,
		Host:            n.Host,
		APIPort:         n.APIPort,
		MediaServerUUID: n.MediaServerUUID,
		Weight:          n.Weight,
		Tags:            n.Tags,
		State:           n.State,
		RTPPortStart:    n.RTPPortStart,
		RTPPortEnd:      n.RTPPortEnd,
		Stats:           n.Stats,
		CreatedAt:       n.CreatedAt,
		UpdatedAt:       n.UpdatedAt,
	}
}

// List 全部节点
func (s *NodeService) List(_ context.Context) ([]*NodeDTO, error) {
	nodes := s.registry.List()
	out := make([]*NodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toDTO(n))
	}
	return out, nil
}

// Get 单个
func (s *NodeService) Get(_ context.Context, id int64) (*NodeDTO, error) {
	n, ok := s.registry.Get(id)
	if !ok {
		return nil, ErrNodeNotFound
	}
	return toDTO(n), nil
}

// Create 新建:probe → 生成 UUID → 入库 → ApplyConfigForNode 写 UUID 到 ZLM
//
// 失败回滚:probe 或 Apply 失败,DB 不应残留节点行。
func (s *NodeService) Create(ctx context.Context, req CreateNodeReq) (*NodeDTO, error) {
	weight := req.Weight
	if weight == 0 {
		weight = 50
	}
	rtpStart := req.RTPPortStart
	if rtpStart == 0 {
		rtpStart = 30000
	}
	rtpEnd := req.RTPPortEnd
	if rtpEnd == 0 {
		rtpEnd = 35000
	}

	tmp := &node.Node{
		Name:            req.Name,
		Host:            req.Host,
		APIPort:         req.APIPort,
		APISecret:       req.APISecret,
		MediaServerUUID: uuid.NewString(),
		Weight:          weight,
		Tags:            req.Tags,
		State:           node.StateActive,
		RTPPortStart:    rtpStart,
		RTPPortEnd:      rtpEnd,
	}

	// 1. 先 probe 探测连通性
	if _, err := s.probe.GetServerConfig(ctx, tmp); err != nil {
		return nil, fmt.Errorf("ZLM 不可达 %s:%d: %w", req.Host, req.APIPort, err)
	}

	// 2. 入库 + 加内存(Registry.Add 内部 Repo.Create)
	added, err := s.registry.Add(ctx, *tmp)
	if err != nil {
		return nil, fmt.Errorf("入库失败: %w", err)
	}

	// 3. 把 mediaServerId + Hook 写到 ZLM(失败则回滚)
	if err := s.probe.ApplyConfigForNode(ctx, added, s.tuning); err != nil {
		_ = s.registry.Delete(ctx, added.ID)
		return nil, fmt.Errorf("写 ZLM 配置失败,已回滚: %w", err)
	}

	return toDTO(added), nil
}

// Update 更新可变字段
func (s *NodeService) Update(ctx context.Context, id int64, req UpdateNodeReq) (*NodeDTO, error) {
	cur, ok := s.registry.Get(id)
	if !ok {
		return nil, ErrNodeNotFound
	}
	if req.Name != nil {
		cur.Name = *req.Name
	}
	if req.APISecret != nil {
		cur.APISecret = *req.APISecret
	}
	if req.Weight != nil {
		cur.Weight = *req.Weight
	}
	if req.Tags != nil {
		cur.Tags = req.Tags
	}
	if req.RTPPortStart != nil {
		cur.RTPPortStart = *req.RTPPortStart
	}
	if req.RTPPortEnd != nil {
		cur.RTPPortEnd = *req.RTPPortEnd
	}
	if err := s.registry.Update(ctx, *cur); err != nil {
		return nil, err
	}
	got, _ := s.registry.Get(id)
	return toDTO(got), nil
}

// Delete 删除前必须 state=maintenance,流量 0 检查留到 M2 LocationMap 之后
func (s *NodeService) Delete(ctx context.Context, id int64) error {
	cur, ok := s.registry.Get(id)
	if !ok {
		return ErrNodeNotFound
	}
	if cur.State != node.StateMaintenance {
		return ErrNodeNotInMaintenance
	}
	return s.registry.Delete(ctx, id)
}

// SetMaintenance 切到维护态
func (s *NodeService) SetMaintenance(ctx context.Context, id int64) error {
	return s.setState(ctx, id, node.StateMaintenance)
}

// Activate 切回 active
func (s *NodeService) Activate(ctx context.Context, id int64) error {
	return s.setState(ctx, id, node.StateActive)
}

func (s *NodeService) setState(ctx context.Context, id int64, state node.State) error {
	cur, ok := s.registry.Get(id)
	if !ok {
		return ErrNodeNotFound
	}
	cur.State = state
	return s.registry.Update(ctx, *cur)
}
