package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ErrNodeNotFound 节点不存在
var ErrNodeNotFound = errors.New("node not found")

// ErrNodeNotInMaintenance 删除节点前必须先进维护态
var ErrNodeNotInMaintenance = errors.New("node must be in maintenance state to delete")

var ErrConfigConvergenceInProgress = errors.New("node config convergence in progress")

var ErrNodeConfigChanged = errors.New("node config changed during convergence")

// ZLMProbe 节点连通性探测 + 配置下发抽象
// 由 zlm.Client 实现(适配器在 bootstrap 注入),测试用 mock。
type ZLMProbe interface {
	// GetServerConfig 探测连通性 + 拿当前 ZLM 配置
	GetServerConfig(ctx context.Context, n *node.Node) (map[string]string, error)
	// ApplyConfigForNode 写 Hook + mediaServerId 等
	ApplyConfigForNode(ctx context.Context, n *node.Node, t MediaTuning) error
	// KickSessions 驱逐节点全部会话(filter 留给 client 内部填 nil),返回被踢数
	KickSessions(ctx context.Context, n *node.Node) (int, error)
	// RestartServer 重启 ZLM 服务;graceMS 为接口预留,当前 ZLM 不支持,仍传透便于将来扩展
	RestartServer(ctx context.Context, n *node.Node, graceMS int) error
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
	ID                int64             `json:"id"`
	Name              string            `json:"name"`
	Host              string            `json:"host"`
	ReceiveHost       string            `json:"receiveHost"`
	PlaybackHost      string            `json:"playbackHost"`
	APIPort           int               `json:"apiPort"`
	MediaServerUUID   string            `json:"mediaServerUUID"`
	Weight            int               `json:"weight"`
	Tags              map[string]string `json:"tags,omitempty"`
	State             node.State        `json:"state"`
	RTPPortStart      int               `json:"rtpPortStart"`
	RTPPortEnd        int               `json:"rtpPortEnd"`
	Stats             node.Stats        `json:"stats"`
	NearCapacity      bool              `json:"nearCapacity"` // T3.4: port_usage>=80% 或 cpu>=80%,UI 黄色高亮
	AutoOnDemandReady bool              `json:"autoOnDemandReady"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

// CreateNodeReq 新建节点入参
type CreateNodeReq struct {
	Name         string            `json:"name" binding:"required"`
	Host         string            `json:"host" binding:"required"`
	ReceiveHost  string            `json:"receiveHost"`
	PlaybackHost string            `json:"playbackHost"`
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
	ReceiveHost  *string           `json:"receiveHost,omitempty"`
	PlaybackHost *string           `json:"playbackHost,omitempty"`
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
	applyMu  sync.Mutex
	applying map[int64]struct{}
	logger   *zap.Logger
}

// NewNodeService 构造
func NewNodeService(reg *node.Registry, probe ZLMProbe, tuning MediaTuning) *NodeService {
	return &NodeService{
		registry: reg,
		probe:    probe,
		tuning:   tuning,
		applying: make(map[int64]struct{}),
		logger:   zap.NewNop(),
	}
}

func (s *NodeService) SetLogger(logger *zap.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

func (s *NodeService) toDTO(n *node.Node) *NodeDTO {
	if n == nil {
		return nil
	}
	return &NodeDTO{
		ID:                n.ID,
		Name:              n.Name,
		Host:              n.Host,
		ReceiveHost:       n.ReceiveHost,
		PlaybackHost:      n.PlaybackHost,
		APIPort:           n.APIPort,
		MediaServerUUID:   n.MediaServerUUID,
		Weight:            n.Weight,
		Tags:              n.Tags,
		State:             n.State,
		RTPPortStart:      n.RTPPortStart,
		RTPPortEnd:        n.RTPPortEnd,
		Stats:             n.Stats,
		NearCapacity:      n.IsNearCapacity(),
		AutoOnDemandReady: s.registry.IsAutoOnDemandReady(n.ID),
		CreatedAt:         n.CreatedAt,
		UpdatedAt:         n.UpdatedAt,
	}
}

// List 全部节点
func (s *NodeService) List(_ context.Context) ([]*NodeDTO, error) {
	nodes := s.registry.List()
	out := make([]*NodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, s.toDTO(n))
	}
	return out, nil
}

// Get 单个
func (s *NodeService) Get(_ context.Context, id int64) (*NodeDTO, error) {
	n, ok := s.registry.Get(id)
	if !ok {
		return nil, ErrNodeNotFound
	}
	return s.toDTO(n), nil
}

// Create 新建:probe → 生成 UUID → 入库 → ApplyConfigForNode 写 UUID 到 ZLM
//
// 失败回滚:probe 或 Apply 失败,DB 不应残留节点行。
// validateNodeFields 服务端统一校验节点字段:API 调用可绕过前端约束;
// 无效端口范围会让 PortUsage 返回 0,使节点绕过容量剔除进入调度
func validateNodeFields(host string, apiPort, weight, rtpStart, rtpEnd int, apiSecret string) error {
	if strings.TrimSpace(host) == "" || strings.ContainsAny(host, " \t") || strings.Contains(host, "://") {
		return fmt.Errorf("host 非法: %q", host)
	}
	if apiPort <= 0 || apiPort > 65535 {
		return fmt.Errorf("apiPort 非法: %d", apiPort)
	}
	if weight < 0 || weight > 100 {
		return fmt.Errorf("weight 需在 0-100 之间: %d", weight)
	}
	if rtpStart <= 0 || rtpEnd <= 0 || rtpStart >= rtpEnd {
		return fmt.Errorf("RTP 端口范围非法: start=%d end=%d,要求 0 < start < end", rtpStart, rtpEnd)
	}
	if rtpEnd > 65535 {
		return fmt.Errorf("RTP 端口上限超出 65535: %d", rtpEnd)
	}
	if strings.TrimSpace(apiSecret) == "" {
		return errors.New("apiSecret 不能为空")
	}
	return nil
}

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
	if err := validateNodeFields(req.Host, req.APIPort, weight, rtpStart, rtpEnd, req.APISecret); err != nil {
		return nil, err
	}

	tmp := &node.Node{
		Name:            req.Name,
		Host:            req.Host,
		ReceiveHost:     req.ReceiveHost,
		PlaybackHost:    req.PlaybackHost,
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
	if err := s.ConvergeNodeConfig(ctx, added.ID); err != nil {
		_ = s.registry.Delete(ctx, added.ID)
		return nil, fmt.Errorf("写 ZLM 配置失败,已回滚: %w", err)
	}

	return s.toDTO(added), nil
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
	if req.ReceiveHost != nil {
		cur.ReceiveHost = *req.ReceiveHost
	}
	if req.PlaybackHost != nil {
		cur.PlaybackHost = *req.PlaybackHost
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
	// 合并后整体校验,防 API 绕过前端约束写入倒置端口范围/异常权重
	if err := validateNodeFields(cur.Host, cur.APIPort, cur.Weight, cur.RTPPortStart, cur.RTPPortEnd, cur.APISecret); err != nil {
		return nil, err
	}
	if err := s.registry.Update(ctx, *cur); err != nil {
		return nil, err
	}
	got, _ := s.registry.Get(id)
	return s.toDTO(got), nil
}

type ConfigApplyResult struct {
	NodeID int64
	Name   string
	Ready  bool
	Err    error
}

// ApplyActiveConfigs converges every active node through the same verified
// configuration path used by node creation and heartbeat recovery.
func (s *NodeService) ApplyActiveConfigs(ctx context.Context) []ConfigApplyResult {
	nodes := s.registry.ListActive()
	results := make([]ConfigApplyResult, len(nodes))
	var wg sync.WaitGroup
	for index, current := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := ConfigApplyResult{NodeID: current.ID, Name: current.Name}
			result.Err = s.ConvergeNodeConfig(ctx, current.ID)
			result.Ready = result.Err == nil && s.registry.IsAutoOnDemandReady(current.ID)
			results[index] = result
		}()
	}
	wg.Wait()
	return results
}

func (s *NodeService) ConvergeNodeConfig(ctx context.Context, nodeID int64) error {
	current, ok := s.beginConfigConvergence(nodeID)
	if !ok {
		if s.registry.IsAutoOnDemandReady(nodeID) {
			return nil
		}
		return ErrConfigConvergenceInProgress
	}
	return s.applyClaimedConfig(ctx, current)
}

// ScheduleConfigConvergence is non-blocking and de-duplicates per node. A
// failed apply leaves readiness false so the next heartbeat retries it.
func (s *NodeService) ScheduleConfigConvergence(nodeID int64) bool {
	current, ok := s.beginConfigConvergence(nodeID)
	if !ok {
		return false
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.applyClaimedConfig(ctx, current); err != nil {
			s.logger.Warn("GB28181 ZLM 节点配置恢复失败",
				zap.Int64("nodeId", current.ID), zap.String("name", current.Name), zap.Error(err))
			return
		}
		s.logger.Info("GB28181 ZLM 节点配置已恢复",
			zap.Int64("nodeId", current.ID), zap.String("name", current.Name))
	}()
	return true
}

func (s *NodeService) beginConfigConvergence(nodeID int64) (*node.Node, bool) {
	current, ok := s.registry.Get(nodeID)
	if !ok || !current.IsActive() || s.registry.IsAutoOnDemandReady(nodeID) {
		return nil, false
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	if _, exists := s.applying[nodeID]; exists {
		return nil, false
	}
	s.applying[nodeID] = struct{}{}
	s.registry.SetAutoOnDemandReady(nodeID, false)
	return current, true
}

func (s *NodeService) applyClaimedConfig(ctx context.Context, current *node.Node) error {
	defer func() {
		s.applyMu.Lock()
		delete(s.applying, current.ID)
		s.applyMu.Unlock()
	}()
	if err := s.probe.ApplyConfigForNode(ctx, current, s.tuning); err != nil {
		return err
	}
	latest, ok := s.registry.Get(current.ID)
	if !ok {
		return ErrNodeNotFound
	}
	if !latest.IsActive() || latest.Host != current.Host || latest.APIPort != current.APIPort ||
		latest.APISecret != current.APISecret || latest.MediaServerUUID != current.MediaServerUUID {
		s.registry.SetAutoOnDemandReady(current.ID, false)
		return ErrNodeConfigChanged
	}
	if !s.registry.SetAutoOnDemandReady(current.ID, true) {
		return ErrNodeNotFound
	}
	return nil
}

// Delete 删除前必须 state=maintenance 且已排空流量:
// 维护态允许旧流自然结束,但节点上仍有活跃会话/媒体源时删除会让
// 注册表与监控中留下无法关联的媒体会话
func (s *NodeService) Delete(ctx context.Context, id int64) error {
	cur, ok := s.registry.Get(id)
	if !ok {
		return ErrNodeNotFound
	}
	if cur.State != node.StateMaintenance {
		return ErrNodeNotInMaintenance
	}
	if cur.Stats.SessionCount > 0 || cur.Stats.MediaSourceCount > 0 {
		return fmt.Errorf("节点仍有 %d 个会话 / %d 个媒体源,请等待排空后再删除", cur.Stats.SessionCount, cur.Stats.MediaSourceCount)
	}
	return s.registry.Delete(ctx, id)
}

// SetMaintenance 切到维护态
func (s *NodeService) SetMaintenance(ctx context.Context, id int64) error {
	return s.setState(ctx, id, node.StateMaintenance)
}

// Activate 切回 active
//
// 关键:同时把 LastHeartbeatAt 重置为 now,给 ZLM 一个 90s 宽限期
// 让真实心跳上报。否则刚 Activate 完 Watcher 下一个 Tick 看到旧的
// LastHeartbeatAt 又把它标回 offline。
func (s *NodeService) Activate(ctx context.Context, id int64) error {
	cur, ok := s.registry.Get(id)
	if !ok {
		return ErrNodeNotFound
	}
	cur.State = node.StateActive
	cur.Stats.LastHeartbeatAt = time.Now()
	if err := s.registry.Update(ctx, *cur); err != nil {
		return err
	}
	// Update 不带 Stats(Stats 只在内存),需单独 reset 内存里的 LastHeartbeatAt
	s.registry.UpdateStats(cur.MediaServerUUID, cur.Stats)
	return nil
}

func (s *NodeService) setState(ctx context.Context, id int64, state node.State) error {
	cur, ok := s.registry.Get(id)
	if !ok {
		return ErrNodeNotFound
	}
	cur.State = state
	return s.registry.Update(ctx, *cur)
}

// KickAllSessions 驱逐节点全部会话,返回被踢的会话数
//
// 用于"节点隔离前清场"或"应急断流",不改节点状态(状态切换由 SetMaintenance 单独负责)。
// 节点不存在 → ErrNodeNotFound;ZLM 不可达 → 透传错误。
func (s *NodeService) KickAllSessions(ctx context.Context, id int64) (int, error) {
	cur, ok := s.registry.Get(id)
	if !ok {
		return 0, ErrNodeNotFound
	}
	return s.probe.KickSessions(ctx, cur)
}

// Restart 重启 ZLM 服务
//
// graceMS:接口预留(当前 ZLM /restartServer 不支持 grace shutdown,立即重启);
// 仍透传以便将来 ZLM 升级后直接接入。节点不存在 → ErrNodeNotFound。
func (s *NodeService) Restart(ctx context.Context, id int64, graceMS int) error {
	cur, ok := s.registry.Get(id)
	if !ok {
		return ErrNodeNotFound
	}
	return s.probe.RestartServer(ctx, cur, graceMS)
}
