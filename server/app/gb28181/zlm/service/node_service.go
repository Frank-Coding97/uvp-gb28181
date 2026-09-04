package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
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

// ErrExternalStateUncertain means that ZLM accepted (or may have accepted) a
// configuration change, but the post-write readback could not prove the
// effective state. Callers must not report the node as ready in this case.
var ErrExternalStateUncertain = errors.New("external_state_uncertain")

// ErrRollbackUncertain means that an update failed and at least one of the
// external ZLM or local snapshot rollback steps could not be verified.
// The node is deliberately kept out of admission when this is returned.
var ErrRollbackUncertain = errors.New("rollback_uncertain")

// ErrRestartPending means a node already has a restart operation waiting for
// its offline/heartbeat/reconciliation lifecycle to finish.
var ErrRestartPending = errors.New("restart already pending")

const (
	maxNodeRecoveryReason   = 255
	endpointRecoveryPending = "endpoint convergence pending"
	endpointRecoveryFailed  = "endpoint convergence failed"
)

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
	HookBaseURL             string
	HookRequireTLS          bool
	HookHost                string
	HookPort                int
	StreamNoneReaderTimeout int
	RTPServerTimeout        int
}

// NodeDTO 对外暴露的节点视图(剥掉 secret)
type NodeDTO struct {
	ID                  int64             `json:"id"`
	Revision            uint64            `json:"revision"`
	Name                string            `json:"name"`
	Host                string            `json:"host"`
	ReceiveHost         string            `json:"receiveHost"`
	PlaybackHost        string            `json:"playbackHost"`
	APIPort             int               `json:"apiPort"`
	MediaServerUUID     string            `json:"mediaServerUUID"`
	Weight              int               `json:"weight"`
	Tags                map[string]string `json:"tags,omitempty"`
	State               node.State        `json:"state"`
	RecoveryRequired    bool              `json:"recoveryRequired"`
	RecoveryReason      string            `json:"recoveryReason,omitempty"`
	RecoveryFingerprint string            `json:"recoveryFingerprint,omitempty"`
	RTPPortStart        int               `json:"rtpPortStart"`
	RTPPortEnd          int               `json:"rtpPortEnd"`
	Stats               node.Stats        `json:"stats"`
	NearCapacity        bool              `json:"nearCapacity"` // T3.4: port_usage>=80% 或 cpu>=80%,UI 黄色高亮
	AutoOnDemandReady   bool              `json:"autoOnDemandReady"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
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

// NodeProbeServerConfig 是新增向导可安全回显的 ZLM 配置子集。
// 原始配置包含 api.secret 等敏感字段，不得直接穿透到控制器。
type NodeProbeServerConfig struct {
	HTTPPort     int  `json:"httpPort"`
	HTTPSPort    int  `json:"httpsPort"`
	RTSPPort     int  `json:"rtspPort"`
	RTSPSPort    int  `json:"rtspsPort"`
	RTMPPort     int  `json:"rtmpPort"`
	RTMPSPort    int  `json:"rtmpsPort"`
	RTPProxyPort int  `json:"rtpProxyPort"`
	ONVIFPort    int  `json:"onvifPort"`
	RTSPEnabled  bool `json:"rtspEnabled"`
	RTMPEnabled  bool `json:"rtmpEnabled"`
	HLSEnabled   bool `json:"hlsEnabled"`
	TSEnabled    bool `json:"tsEnabled"`
	FMP4Enabled  bool `json:"fmp4Enabled"`
}

// NodeProbeResult 是候选节点的只读探测结果，不代表节点已经登记。
type NodeProbeResult struct {
	Online        bool                  `json:"online"`
	MediaServerID string                `json:"mediaServerId"`
	ServerConfig  NodeProbeServerConfig `json:"serverConfig"`
}

// UpdateNodeReq 更新节点入参(可选字段用指针)
type UpdateNodeReq struct {
	Name         *string           `json:"name,omitempty"`
	Host         *string           `json:"host,omitempty"`
	ReceiveHost  *string           `json:"receiveHost,omitempty"`
	PlaybackHost *string           `json:"playbackHost,omitempty"`
	APIPort      *int              `json:"apiPort,omitempty"`
	APISecret    *string           `json:"apiSecret,omitempty"`
	Weight       *int              `json:"weight,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	RTPPortStart *int              `json:"rtpPortStart,omitempty"`
	RTPPortEnd   *int              `json:"rtpPortEnd,omitempty"`
}

// NodeService 节点 CRUD + 状态切换
type NodeService struct {
	registry       *node.Registry
	probe          ZLMProbe
	tuning         MediaTuning
	applyMu        sync.Mutex
	applying       map[int64]struct{}
	locksMu        sync.Mutex
	locks          map[int64]*sync.Mutex
	impactMu       sync.RWMutex
	impactProvider NodeImpactProvider
	logger         *zap.Logger
	restart        *RestartCoordinator
}

// NewNodeService 构造
func NewNodeService(reg *node.Registry, probe ZLMProbe, tuning MediaTuning) *NodeService {
	s := &NodeService{
		registry: reg,
		probe:    probe,
		tuning:   tuning,
		applying: make(map[int64]struct{}),
		locks:    make(map[int64]*sync.Mutex),
		logger:   zap.NewNop(),
	}
	s.restart = NewRestartCoordinator(reg)
	s.restart.SetConverger(s.ConvergeNodeConfig)
	return s
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
		ID:                  n.ID,
		Revision:            n.Revision,
		Name:                n.Name,
		Host:                n.Host,
		ReceiveHost:         n.ReceiveHost,
		PlaybackHost:        n.PlaybackHost,
		APIPort:             n.APIPort,
		MediaServerUUID:     n.MediaServerUUID,
		Weight:              n.Weight,
		Tags:                cloneTags(n.Tags),
		State:               n.State,
		RecoveryRequired:    n.RecoveryRequired,
		RecoveryReason:      publicNodeRecoveryReason(n),
		RecoveryFingerprint: publicNodeRecoveryFingerprint(n),
		RTPPortStart:        n.RTPPortStart,
		RTPPortEnd:          n.RTPPortEnd,
		Stats:               n.Stats,
		NearCapacity:        n.IsNearCapacity(),
		AutoOnDemandReady:   s.registry.IsAutoOnDemandReady(n.ID),
		CreatedAt:           n.CreatedAt,
		UpdatedAt:           n.UpdatedAt,
	}
}

func cloneTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		out[k] = v
	}
	return out
}

func cloneNode(n *node.Node) *node.Node {
	if n == nil {
		return nil
	}
	out := *n
	out.Tags = cloneTags(n.Tags)
	return &out
}

type nodeRecoveryState struct {
	required    bool
	reason      string
	fingerprint string
}

func boundedNodeRecoveryReason(reason string) string {
	runes := []rune(strings.TrimSpace(reason))
	if len(runes) > maxNodeRecoveryReason {
		runes = runes[:maxNodeRecoveryReason]
	}
	return string(runes)
}

func publicNodeRecoveryReason(n *node.Node) string {
	if n == nil || !n.RecoveryRequired {
		return ""
	}
	reason := boundedNodeRecoveryReason(n.RecoveryReason)
	switch reason {
	case endpointRecoveryPending,
		endpointRecoveryFailed + ": external state uncertain",
		endpointRecoveryFailed + ": node config changed",
		endpointRecoveryFailed + ": external apply failed",
		"rollback failed: external state uncertain",
		"rollback failed: external apply failed",
		"rollback state changed concurrently":
		return reason
	default:
		// Recovery metadata can be loaded from an older or manually edited row;
		// never reflect arbitrary text (which may contain credentials) through
		// the DTO boundary.
		return "recovery required"
	}
}

func publicNodeRecoveryFingerprint(n *node.Node) string {
	if n == nil || !n.RecoveryRequired || len(n.RecoveryFingerprint) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(n.RecoveryFingerprint); err != nil {
		return ""
	}
	return n.RecoveryFingerprint
}

func endpointRecoveryFingerprint(n *node.Node) string {
	if n == nil {
		return ""
	}
	// The fingerprint is an opaque operation marker. It may identify the
	// candidate host/port, but must never be derived from or reveal APISecret.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%d\x00%s\x00%d\x00%s", n.ID, n.Revision, n.Host, n.APIPort, uuid.NewString())))
	return hex.EncodeToString(sum[:])
}

func pendingEndpointRecovery(candidate *node.Node) nodeRecoveryState {
	return nodeRecoveryState{
		required:    true,
		reason:      endpointRecoveryPending,
		fingerprint: endpointRecoveryFingerprint(candidate),
	}
}

func failedEndpointRecovery(candidate *node.Node, err error) nodeRecoveryState {
	reason := endpointRecoveryFailed
	if err != nil {
		// Keep the durable reason classified and bounded. External client errors
		// may include URLs, request bodies, or credentials in forms that cannot
		// be safely redacted for persistence.
		switch {
		case errors.Is(err, ErrExternalStateUncertain):
			reason += ": external state uncertain"
		case errors.Is(err, ErrNodeConfigChanged):
			reason += ": node config changed"
		default:
			reason += ": external apply failed"
		}
	}
	return nodeRecoveryState{
		required:    true,
		reason:      boundedNodeRecoveryReason(reason),
		fingerprint: endpointRecoveryFingerprint(candidate),
	}
}

func (s *NodeService) nodeLock(id int64) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if lock, ok := s.locks[id]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	s.locks[id] = lock
	return lock
}

// redactNodeError prevents a node API secret from crossing the service
// boundary in errors returned by a typed client or an upstream HTTP helper.
func redactNodeError(err error, n *node.Node) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if n != nil && n.APISecret != "" {
		message = strings.ReplaceAll(message, n.APISecret, "***")
		message = strings.ReplaceAll(message, url.QueryEscape(n.APISecret), "***")
	}
	return redactedNodeError{err: err, message: message}
}

type redactedNodeError struct {
	err     error
	message string
}

func (e redactedNodeError) Error() string { return e.message }
func (e redactedNodeError) Unwrap() error { return e.err }

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

func buildCreateCandidate(req CreateNodeReq) (*node.Node, error) {
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

	return &node.Node{
		Name:            req.Name,
		Host:            req.Host,
		ReceiveHost:     req.ReceiveHost,
		PlaybackHost:    req.PlaybackHost,
		APIPort:         req.APIPort,
		APISecret:       req.APISecret,
		MediaServerUUID: uuid.NewString(),
		Weight:          weight,
		Tags:            cloneTags(req.Tags),
		State:           node.StateActive,
		RTPPortStart:    rtpStart,
		RTPPortEnd:      rtpEnd,
	}, nil
}

// ProbeCreate 读取候选 ZLM 的安全配置摘要，不登记节点，也不下发平台配置。
func (s *NodeService) ProbeCreate(ctx context.Context, req CreateNodeReq) (*NodeProbeResult, error) {
	candidate, err := buildCreateCandidate(req)
	if err != nil {
		return nil, err
	}
	config, err := s.probe.GetServerConfig(ctx, candidate)
	if err != nil {
		return nil, fmt.Errorf("ZLM 不可达 %s:%d: %w", req.Host, req.APIPort, redactNodeError(err, candidate))
	}
	detected := node.ParseServerConfig(config)
	return &NodeProbeResult{
		Online:        true,
		MediaServerID: config["general.mediaServerId"],
		ServerConfig: NodeProbeServerConfig{
			HTTPPort: detected.HTTPPort, HTTPSPort: detected.HTTPSPort,
			RTSPPort: detected.RTSPPort, RTSPSPort: detected.RTSPSPort,
			RTMPPort: detected.RTMPPort, RTMPSPort: detected.RTMPSPort,
			RTPProxyPort: detected.RTPProxyPort, ONVIFPort: detected.ONVIFPort,
			RTSPEnabled: detected.RTSPEnabled, RTMPEnabled: detected.RTMPEnabled,
			HLSEnabled: detected.HLSEnabled, TSEnabled: detected.TSEnabled,
			FMP4Enabled: detected.FMP4Enabled,
		},
	}, nil
}

func (s *NodeService) Create(ctx context.Context, req CreateNodeReq) (*NodeDTO, error) {
	tmp, err := buildCreateCandidate(req)
	if err != nil {
		return nil, err
	}

	// 1. 先 probe 探测连通性
	if _, err = s.probe.GetServerConfig(ctx, tmp); err != nil {
		return nil, fmt.Errorf("ZLM 不可达 %s:%d: %w", req.Host, req.APIPort, redactNodeError(err, tmp))
	}

	// 2. 入库 + 加内存(Registry.Add 内部 Repo.Create)
	added, err := s.registry.Add(ctx, *tmp)
	if err != nil {
		return nil, fmt.Errorf("入库失败: %w", err)
	}

	// 3. 把 mediaServerId + Hook 写到 ZLM(失败则回滚)
	if err := s.ConvergeNodeConfig(ctx, added.ID); err != nil {
		if rollbackErr := s.registry.Delete(ctx, added.ID); rollbackErr != nil {
			return nil, fmt.Errorf("%w: 创建失败且本地回滚失败: %v", ErrRollbackUncertain, redactNodeError(rollbackErr, added))
		}
		return nil, fmt.Errorf("写 ZLM 配置失败,已回滚: %w", redactNodeError(err, added))
	}

	return s.toDTO(added), nil
}

// Update 更新可变字段
func (s *NodeService) Update(ctx context.Context, id int64, req UpdateNodeReq) (*NodeDTO, error) {
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()

	cur, ok := s.registry.Get(id)
	if !ok {
		return nil, ErrNodeNotFound
	}
	old := cloneNode(cur)
	candidate := cloneNode(cur)
	if req.Name != nil {
		candidate.Name = *req.Name
	}
	if req.Host != nil {
		candidate.Host = *req.Host
	}
	if req.ReceiveHost != nil {
		candidate.ReceiveHost = *req.ReceiveHost
	}
	if req.PlaybackHost != nil {
		candidate.PlaybackHost = *req.PlaybackHost
	}
	if req.APIPort != nil {
		candidate.APIPort = *req.APIPort
	}
	if req.APISecret != nil {
		candidate.APISecret = *req.APISecret
	}
	if req.Weight != nil {
		candidate.Weight = *req.Weight
	}
	if req.Tags != nil {
		candidate.Tags = cloneTags(req.Tags)
	}
	if req.RTPPortStart != nil {
		candidate.RTPPortStart = *req.RTPPortStart
	}
	if req.RTPPortEnd != nil {
		candidate.RTPPortEnd = *req.RTPPortEnd
	}
	// 合并后整体校验,防 API 绕过前端约束写入倒置端口范围/异常权重
	if err := validateNodeFields(candidate.Host, candidate.APIPort, candidate.Weight, candidate.RTPPortStart, candidate.RTPPortEnd, candidate.APISecret); err != nil {
		return nil, err
	}

	connectionChanged := old.Host != candidate.Host || old.APIPort != candidate.APIPort || old.APISecret != candidate.APISecret
	if connectionChanged {
		recovery := pendingEndpointRecovery(candidate)
		candidate.RecoveryRequired = recovery.required
		candidate.RecoveryReason = recovery.reason
		candidate.RecoveryFingerprint = recovery.fingerprint
		if _, err := s.probe.GetServerConfig(ctx, candidate); err != nil {
			// candidate has not reached Registry yet: the old snapshot remains
			// authoritative and no persistence/write-back is attempted.
			return nil, fmt.Errorf("ZLM 不可达 %s:%d: %w", candidate.Host, candidate.APIPort, redactNodeError(err, candidate))
		}
	}

	if err := s.registry.Update(ctx, *candidate); err != nil {
		return nil, err
	}
	if candidate.IsActive() {
		if err := s.convergeNodeLocked(ctx, id); err != nil {
			if connectionChanged {
				// The candidate endpoint may have accepted SetConfig before a
				// readback/convergence failure. Restore the old endpoint in local
				// persistence, but retain a bounded recovery marker so a fresh
				// process remains fail-closed until reconciliation succeeds.
				recovery := failedEndpointRecovery(candidate, err)
				localErr := s.restoreNodeSnapshot(ctx, old, recovery)
				s.registry.SetAutoOnDemandReady(old.ID, false)
				s.registry.SetAdmissionBlocked(old.ID, true)
				if localErr != nil {
					return nil, fmt.Errorf("%w: local snapshot restore failed: %v", ErrRollbackUncertain, redactNodeError(localErr, old))
				}
				return nil, fmt.Errorf("%w: candidate external state uncertain", ErrRollbackUncertain)
			}
			rollbackErr := s.rollbackNodeLocked(ctx, old)
			if rollbackErr != nil {
				return nil, rollbackErr
			}
			return nil, redactNodeError(err, candidate)
		}
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
	lock := s.nodeLock(nodeID)
	lock.Lock()
	defer lock.Unlock()
	return s.convergeNodeLocked(ctx, nodeID)
}

// ScheduleConfigConvergence is non-blocking and de-duplicates per node. A
// failed apply leaves readiness false so the next heartbeat retries it.
func (s *NodeService) ScheduleConfigConvergence(nodeID int64) bool {
	lock := s.nodeLock(nodeID)
	lock.Lock()
	current, ok := s.beginConfigConvergence(nodeID)
	lock.Unlock()
	if !ok {
		return false
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		lock := s.nodeLock(nodeID)
		lock.Lock()
		err := s.applyClaimedConfig(ctx, current)
		lock.Unlock()
		if err != nil {
			s.logger.Warn("GB28181 ZLM 节点配置恢复失败",
				zap.Int64("nodeId", nodeID), zap.Error(err))
			return
		}
		s.logger.Info("GB28181 ZLM 节点配置已恢复", zap.Int64("nodeId", nodeID))
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
	return cloneNode(current), true
}

func (s *NodeService) convergeNodeLocked(ctx context.Context, nodeID int64) error {
	current, ok := s.registry.Get(nodeID)
	if !ok {
		return ErrNodeNotFound
	}
	if !current.IsActive() {
		s.registry.SetAutoOnDemandReady(nodeID, false)
		return fmt.Errorf("node %d is not active", nodeID)
	}
	if s.registry.IsAutoOnDemandReady(nodeID) {
		return nil
	}

	s.applyMu.Lock()
	if _, exists := s.applying[nodeID]; exists {
		s.applyMu.Unlock()
		return ErrConfigConvergenceInProgress
	}
	s.applying[nodeID] = struct{}{}
	s.applyMu.Unlock()
	return s.applyClaimedConfig(ctx, cloneNode(current))
}

func (s *NodeService) applyClaimedConfig(ctx context.Context, current *node.Node) error {
	defer func() {
		s.applyMu.Lock()
		delete(s.applying, current.ID)
		s.applyMu.Unlock()
	}()
	if err := s.probe.ApplyConfigForNode(ctx, current, s.tuning); err != nil {
		return redactNodeError(err, current)
	}
	// A successful set command is only an acknowledgement. Read the effective
	// ZLM configuration before admitting this node again.
	if _, err := s.probe.GetServerConfig(ctx, current); err != nil {
		s.registry.SetAutoOnDemandReady(current.ID, false)
		return fmt.Errorf("%w: %v", ErrExternalStateUncertain, redactNodeError(err, current))
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
	if latest.RecoveryRequired || latest.RecoveryReason != "" || latest.RecoveryFingerprint != "" {
		latest.RecoveryRequired = false
		latest.RecoveryReason = ""
		latest.RecoveryFingerprint = ""
		if err := s.registry.Update(ctx, *latest); err != nil {
			s.registry.SetAutoOnDemandReady(current.ID, false)
			return err
		}
		latest, ok = s.registry.Get(current.ID)
		if !ok {
			return ErrNodeNotFound
		}
		if !latest.IsActive() || latest.Host != current.Host || latest.APIPort != current.APIPort ||
			latest.APISecret != current.APISecret || latest.MediaServerUUID != current.MediaServerUUID {
			s.registry.SetAutoOnDemandReady(current.ID, false)
			return ErrNodeConfigChanged
		}
	}
	if !s.registry.SetAutoOnDemandReadyIfRevision(current.ID, latest.Revision, true) {
		if _, ok := s.registry.Get(current.ID); !ok {
			return ErrNodeNotFound
		}
		return ErrNodeConfigChanged
	}
	return nil
}

// restoreNodeSnapshot performs a CAS restore of the fields changed by the
// failed update while taking the latest lifecycle state and in-memory stats as
// the source of truth. A concurrent offline/maintenance transition therefore
// survives rollback instead of being overwritten by old.State.
func (s *NodeService) restoreNodeSnapshot(ctx context.Context, old *node.Node, recovery nodeRecoveryState) error {
	if old == nil {
		return node.ErrNotFound
	}
	for attempt := 0; attempt < 8; attempt++ {
		latest, ok := s.registry.Get(old.ID)
		if !ok {
			return node.ErrNotFound
		}
		restored := cloneNode(old)
		restored.Revision = latest.Revision
		restored.State = latest.State
		restored.Stats = latest.Stats
		if recovery.required || recovery.reason != "" || recovery.fingerprint != "" {
			restored.RecoveryRequired = recovery.required
			restored.RecoveryReason = boundedNodeRecoveryReason(recovery.reason)
			restored.RecoveryFingerprint = recovery.fingerprint
		}
		if err := s.registry.Update(ctx, *restored); err != nil {
			if errors.Is(err, node.ErrRevisionConflict) {
				if !recovery.required {
					recovery = nodeRecoveryState{
						required:    true,
						reason:      "rollback state changed concurrently",
						fingerprint: endpointRecoveryFingerprint(old),
					}
				}
				if quarantineErr := s.registry.MarkRecoveryRequired(
					ctx,
					old.ID,
					boundedNodeRecoveryReason(recovery.reason),
					recovery.fingerprint,
				); quarantineErr != nil {
					return fmt.Errorf("%w: recovery quarantine failed: %v", err, quarantineErr)
				}
				// The old snapshot was not restored. The caller must keep the
				// node fail-closed even though the latest durable fields won.
				return err
			}
			return err
		}
		return nil
	}
	return node.ErrRevisionConflict
}

// rollbackNodeLocked restores the persisted candidate and makes a best effort
// to restore the external ZLM state. It is called while the node keyed lock is
// held, so an older snapshot can never overwrite a newer update.
func (s *NodeService) rollbackNodeLocked(ctx context.Context, old *node.Node) error {
	s.registry.SetAutoOnDemandReady(old.ID, false)
	var externalErr error
	if err := s.probe.ApplyConfigForNode(ctx, old, s.tuning); err != nil {
		externalErr = redactNodeError(err, old)
	} else if _, err := s.probe.GetServerConfig(ctx, old); err != nil {
		externalErr = fmt.Errorf("%w: %v", ErrExternalStateUncertain, redactNodeError(err, old))
	}

	recovery := nodeRecoveryState{}
	if externalErr != nil {
		reason := "rollback failed: external apply failed"
		if errors.Is(externalErr, ErrExternalStateUncertain) {
			reason = "rollback failed: external state uncertain"
		}
		recovery = nodeRecoveryState{
			required:    true,
			reason:      reason,
			fingerprint: endpointRecoveryFingerprint(old),
		}
	}
	localErr := s.restoreNodeSnapshot(ctx, old, recovery)
	if externalErr != nil || localErr != nil {
		detail := ""
		if externalErr != nil {
			detail = " external=" + externalErr.Error()
		}
		if localErr != nil {
			detail += " local=" + redactNodeError(localErr, old).Error()
		}
		// Keep admission closed even if persistence itself failed. The in-memory
		// snapshot is not claimed to be authoritative in that uncertain case.
		s.registry.SetAutoOnDemandReady(old.ID, false)
		s.registry.SetAdmissionBlocked(old.ID, true)
		return fmt.Errorf("%w:%s", ErrRollbackUncertain, detail)
	}
	return nil
}

// Delete 删除前必须 state=maintenance 且已排空流量:
// 维护态允许旧流自然结束,但节点上仍有活跃会话/媒体源时删除会让
// 注册表与监控中留下无法关联的媒体会话
func (s *NodeService) Delete(ctx context.Context, id int64) error {
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()
	cur, ok := s.registry.Get(id)
	if !ok {
		return ErrNodeNotFound
	}
	if s.nodeImpactProvider() != nil {
		return ErrNodeImpactConfirmationRequired
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
	if s.nodeImpactProvider() != nil {
		if _, ok := s.registry.Get(id); !ok {
			return ErrNodeNotFound
		}
		return ErrNodeImpactConfirmationRequired
	}
	return s.setState(ctx, id, node.StateMaintenance)
}

// Activate 切回 active
//
// 关键:同时把 LastHeartbeatAt 重置为 now,给 ZLM 一个 90s 宽限期
// 让真实心跳上报。否则刚 Activate 完 Watcher 下一个 Tick 看到旧的
// LastHeartbeatAt 又把它标回 offline。
func (s *NodeService) Activate(ctx context.Context, id int64) error {
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()
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
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()
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
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()
	cur, ok := s.registry.Get(id)
	if !ok {
		return 0, ErrNodeNotFound
	}
	if s.nodeImpactProvider() != nil {
		return 0, ErrNodeImpactConfirmationRequired
	}
	return s.probe.KickSessions(ctx, cur)
}

// RestartAccepted starts a restart operation and returns only command
// acceptance. ZLM code=0 is not treated as lifecycle completion.
func (s *NodeService) RestartAccepted(ctx context.Context, id int64, graceMS int) (*RestartAcceptedResponse, error) {
	lock := s.nodeLock(id)
	lock.Lock()
	defer lock.Unlock()
	cur, ok := s.registry.Get(id)
	if !ok {
		return nil, ErrNodeNotFound
	}
	if s.restart == nil {
		s.restart = NewRestartCoordinator(s.registry)
		s.restart.SetConverger(s.ConvergeNodeConfig)
	}
	op, err := s.restart.Begin(id)
	if err != nil {
		return nil, err
	}
	if err := s.probe.RestartServer(ctx, cur, graceMS); err != nil {
		s.restart.FailGeneration(id, op.Generation, redactNodeError(err, cur))
		return nil, redactNodeError(err, cur)
	}
	s.restart.AdvanceToWaitingOffline(id, op.Generation)
	return &RestartAcceptedResponse{
		Accepted:    true,
		OperationID: op.OperationID,
		Status:      RestartStatusAccepted,
	}, nil
}

// Restart preserves the legacy error-only API while the controller and new
// callers use RestartAccepted for the operation response.
func (s *NodeService) Restart(ctx context.Context, id int64, graceMS int) error {
	_, err := s.RestartAccepted(ctx, id, graceMS)
	return err
}

// RestartPending is the explicit admission query used by scheduler/T14. It
// is intentionally not coupled to ordinary active/readiness semantics.
func (s *NodeService) RestartPending(id int64) bool {
	return s.restart != nil && s.restart.IsPending(id)
}

func (s *NodeService) RestartOperation(id int64) (RestartOperation, bool) {
	if s.restart == nil {
		return UnknownOperation(id), true
	}
	if operation, ok := s.restart.Get(id); ok {
		return operation, true
	}
	return UnknownOperation(id), true
}

func (s *NodeService) RestartOperationByID(operationID string) (RestartOperation, bool) {
	if s.restart == nil {
		return RestartOperation{}, false
	}
	return s.restart.GetByID(operationID)
}

// RestartNotifier exposes the optional heartbeat event bridge for T14
// bootstrap wiring without changing existing constructors.
func (s *NodeService) RestartNotifier() RestartEventNotifier { return s.restart }

// SetRestartCoordinator allows T14/bootstrap to provide a lifecycle policy
// (for example a shorter test timeout) without changing old constructors.
func (s *NodeService) SetRestartCoordinator(coordinator *RestartCoordinator) {
	if coordinator == nil {
		return
	}
	coordinator.SetConverger(s.ConvergeNodeConfig)
	previous := s.restart
	s.restart = coordinator
	if previous != nil && previous != coordinator {
		previous.Close()
	}
}
