package service

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// ConfigItem 单条 ZLM 配置元数据
type ConfigItem struct {
	Key             string `json:"key"`             // ZLM 配置 key,如 http.port
	Value           string `json:"value"`           // 当前值(GetGrouped 时填)
	Default         string `json:"default"`         // 默认值
	HotReloadable   bool   `json:"hotReloadable"`   // 是否可在线热改
	RestartRequired bool   `json:"restartRequired"` // 修改后是否要重启 ZLM
	Comment         string `json:"comment"`         // 中文说明
}

// ConfigGroup 产品化分组
type ConfigGroup struct {
	Name  string       `json:"name"`
	Items []ConfigItem `json:"items"`
}

// configCatalog 产品化分组 + 元数据(基于 ZLM 760 行 config.ini 调研,按业务相关性聚拢)
// 不覆盖全部 ZLM 配置项,只列 M1 阶段需要 UI 调的子集,后续按需扩展。
var configCatalog = []ConfigGroup{
	{
		Name: "网络端口",
		Items: []ConfigItem{
			{Key: "http.port", Default: "80", HotReloadable: false, RestartRequired: true, Comment: "ZLM HTTP API 端口"},
			{Key: "rtmp.port", Default: "1935", HotReloadable: false, RestartRequired: true, Comment: "RTMP 推流端口"},
			{Key: "rtsp.port", Default: "554", HotReloadable: false, RestartRequired: true, Comment: "RTSP 端口"},
			{Key: "rtp_proxy.port_range", Default: "30000-35000", HotReloadable: false, RestartRequired: true, Comment: "RTP 多端口收流范围"},
		},
	},
	{
		Name: "Hook",
		Items: []ConfigItem{
			{Key: "hook.enable", Default: "0", HotReloadable: true, Comment: "Hook 总开关"},
			{Key: "hook.on_server_started", Default: "", HotReloadable: true, Comment: "ZLM 启动回调"},
			{Key: "hook.on_server_keepalive", Default: "", HotReloadable: true, Comment: "心跳回调,M2 多节点收集状态用"},
			{Key: "hook.on_stream_changed", Default: "", HotReloadable: true, Comment: "流注册/注销回调"},
			{Key: "hook.on_stream_none_reader", Default: "", HotReloadable: true, Comment: "无人观看回调"},
			{Key: "hook.on_rtp_server_timeout", Default: "", HotReloadable: true, Comment: "RTP 收流超时回调"},
			{Key: "hook.on_publish", Default: "", HotReloadable: true, Comment: "推流鉴权"},
			{Key: "hook.on_play", Default: "", HotReloadable: true, Comment: "播放鉴权"},
			{Key: "hook.alive_interval", Default: "30.0", HotReloadable: true, Comment: "心跳上报周期(秒)"},
		},
	},
	{
		Name: "运行时策略",
		Items: []ConfigItem{
			{Key: "general.streamNoneReaderDelayMS", Default: "20000", HotReloadable: true, Comment: "无人观看自动断流延迟(毫秒)"},
			{Key: "general.mediaServerId", Default: "", HotReloadable: true, Comment: "节点 UUID,业务侧生成"},
			{Key: "general.maxStreamWaitMS", Default: "15000", HotReloadable: true, Comment: "流就绪最大等待(毫秒)"},
		},
	},
	{
		Name: "国标",
		Items: []ConfigItem{
			{Key: "rtp_proxy.checkSource", Default: "1", HotReloadable: true, Comment: "是否校验 SSRC"},
			{Key: "rtp_proxy.timeoutSec", Default: "15", HotReloadable: true, Comment: "RTP 收流超时(秒)"},
		},
	},
}

// catalogIndex 快速查 key → item 的索引
var catalogIndex = func() map[string]ConfigItem {
	m := map[string]ConfigItem{}
	for _, g := range configCatalog {
		for _, it := range g.Items {
			m[it.Key] = it
		}
	}
	return m
}()

// UpdateConfigReq 更新请求
type UpdateConfigReq struct {
	Changes map[string]string `json:"changes" binding:"required"`
}

// UpdateConfigResp 更新结果
type UpdateConfigResp struct {
	Applied         []string `json:"applied"`         // 已生效(热改成功)
	RequiresRestart []string `json:"requiresRestart"` // 需要重启 ZLM 才生效
	Unknown         []string `json:"unknown"`         // 未在 catalog 中的 key(原样下发)
}

// TestConnectionResult 探测结果
type TestConnectionResult struct {
	Online    bool   `json:"online"`
	HTTPPort  string `json:"httpPort,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ZLMConfigClient ZLM 配置面接口(GetServerConfig + SetServerConfig)
type ZLMConfigClient interface {
	GetServerConfig(ctx context.Context, n *node.Node) (map[string]string, error)
	SetServerConfig(ctx context.Context, n *node.Node, params map[string]string) error
}

// ConfigService 节点配置查询 + 下发
type ConfigService struct {
	registry *node.Registry
	client   ZLMConfigClient
}

// NewConfigService 构造
func NewConfigService(reg *node.Registry, cli ZLMConfigClient) *ConfigService {
	return &ConfigService{registry: reg, client: cli}
}

// GetGrouped 取节点当前 ZLM 配置,按产品化分组返回(含 hot_reloadable 元数据)
func (s *ConfigService) GetGrouped(ctx context.Context, nodeID int64) ([]ConfigGroup, error) {
	n, ok := s.registry.Get(nodeID)
	if !ok {
		return nil, ErrNodeNotFound
	}
	current, err := s.client.GetServerConfig(ctx, n)
	if err != nil {
		return nil, err
	}
	out := make([]ConfigGroup, 0, len(configCatalog))
	for _, g := range configCatalog {
		items := make([]ConfigItem, 0, len(g.Items))
		for _, it := range g.Items {
			if v, ok := current[it.Key]; ok {
				it.Value = v
			} else {
				it.Value = it.Default
			}
			items = append(items, it)
		}
		out = append(out, ConfigGroup{Name: g.Name, Items: items})
	}
	return out, nil
}

// Update 下发配置变更,自动按 hot_reloadable 分流
func (s *ConfigService) Update(ctx context.Context, nodeID int64, req UpdateConfigReq) (*UpdateConfigResp, error) {
	n, ok := s.registry.Get(nodeID)
	if !ok {
		return nil, ErrNodeNotFound
	}
	resp := &UpdateConfigResp{
		Applied:         []string{},
		RequiresRestart: []string{},
		Unknown:         []string{},
	}
	hotParams := map[string]string{}
	for k, v := range req.Changes {
		meta, known := catalogIndex[k]
		if !known {
			resp.Unknown = append(resp.Unknown, k)
			hotParams[k] = v // 未知项 fallback 当作热改下发
			continue
		}
		if meta.HotReloadable {
			hotParams[k] = v
			resp.Applied = append(resp.Applied, k)
		} else {
			resp.RequiresRestart = append(resp.RequiresRestart, k)
		}
	}
	if len(hotParams) > 0 {
		if err := s.client.SetServerConfig(ctx, n, hotParams); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// TestConnection 探测 ZLM 是否可达
// 总是返回 nil error(把"不可达"也算正常响应),失败信息塞 Result.Error
func (s *ConfigService) TestConnection(ctx context.Context, nodeID int64) (*TestConnectionResult, error) {
	n, ok := s.registry.Get(nodeID)
	if !ok {
		return nil, ErrNodeNotFound
	}
	conf, err := s.client.GetServerConfig(ctx, n)
	if err != nil {
		return &TestConnectionResult{Online: false, Error: err.Error()}, nil
	}
	return &TestConnectionResult{Online: true, HTTPPort: conf["http.port"]}, nil
}
