package zlm

import (
	"context"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// ServiceAdapter 实现 service.ZLMProbe + service.ZLMConfigClient
// 每次按 node 临时构造 Client,避免持有连接池(Go http.Client 自带连接复用)。
type ServiceAdapter struct {
	tuning gbconfig.MediaConfig
}

// NewServiceAdapter 构造
func NewServiceAdapter(media gbconfig.MediaConfig) *ServiceAdapter {
	return &ServiceAdapter{tuning: media}
}

// GetServerConfig 实现 service.ZLMProbe / service.ZLMConfigClient
func (a *ServiceAdapter) GetServerConfig(ctx context.Context, n *node.Node) (map[string]string, error) {
	return NewClientForNode(n).GetServerConfig(ctx)
}

// SetServerConfig 实现 service.ZLMConfigClient
func (a *ServiceAdapter) SetServerConfig(ctx context.Context, n *node.Node, params map[string]string) error {
	return NewClientForNode(n).SetServerConfig(ctx, params)
}

// ApplyConfigForNode 实现 service.ZLMProbe
func (a *ServiceAdapter) ApplyConfigForNode(ctx context.Context, n *node.Node, t service.MediaTuning) error {
	media := gbconfig.MediaConfig{
		HookHost:                a.tuning.HookHost,
		HookPort:                a.tuning.HookPort,
		StreamNoneReaderTimeout: a.tuning.StreamNoneReaderTimeout,
		RTPServerTimeout:        a.tuning.RTPServerTimeout,
	}
	// 优先用传入的 tuning(允许 service 层覆盖默认值)
	if t.HookHost != "" {
		media.HookHost = t.HookHost
	}
	if t.HookPort != 0 {
		media.HookPort = t.HookPort
	}
	if t.StreamNoneReaderTimeout != 0 {
		media.StreamNoneReaderTimeout = t.StreamNoneReaderTimeout
	}
	return NewClientForNode(n).ApplyConfigForNode(ctx, media)
}

// KickSessions 实现 service.ZLMProbe(T3.5)
//
// 节点驱逐场景:filter 传 nil 踢全部。后续如要细粒度驱逐可单独走 Client.KickSessions。
func (a *ServiceAdapter) KickSessions(ctx context.Context, n *node.Node) (int, error) {
	return NewClientForNode(n).KickSessions(ctx, nil)
}

// RestartServer 实现 service.ZLMProbe(T3.5)
//
// graceMS 透传给 Client(当前 ZLM 不支持 grace,接口预留)。
func (a *ServiceAdapter) RestartServer(ctx context.Context, n *node.Node, graceMS int) error {
	return NewClientForNode(n).RestartServer(ctx, graceMS)
}

// GetThreadsLoad 实现 heartbeat.ThreadLoadFetcher(2026-06-28 Stats 字段 mismatch 修)
func (a *ServiceAdapter) GetThreadsLoad(ctx context.Context, n *node.Node) (float64, error) {
	return NewClientForNode(n).GetThreadsLoad(ctx)
}

// GetWorkThreadsLoad 实现 heartbeat.ThreadLoadFetcher
func (a *ServiceAdapter) GetWorkThreadsLoad(ctx context.Context, n *node.Node) (float64, error) {
	return NewClientForNode(n).GetWorkThreadsLoad(ctx)
}
