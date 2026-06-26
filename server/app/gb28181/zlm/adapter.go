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
