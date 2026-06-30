package zlm

import (
	"context"
	"fmt"
	"strconv"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

// hookPath Hook 端点统一前缀(挂在后端 HTTP 服务下)
const HookBasePath = "/index/hook"

// ApplyConfigForNode 启动时把运行时策略动态下发给 ZLM,并写入 mediaServerId
// 让 ZLM 后续 on_server_keepalive 等回调带上该 UUID 用于反查节点。
//
// 多节点场景:每节点首次启动调用一次。
func (c *Client) ApplyConfigForNode(ctx context.Context, media gbconfig.MediaConfig) error {
	base := fmt.Sprintf("http://%s:%d%s", media.HookHost, media.HookPort, HookBasePath)
	params := map[string]string{
		// Hook 全套回调地址
		"hook.enable":                "1",
		"hook.on_server_started":     base + "/on_server_started",
		"hook.on_server_keepalive":   base + "/on_server_keepalive",
		"hook.on_stream_changed":     base + "/on_stream_changed",
		"hook.on_stream_none_reader": base + "/on_stream_none_reader",
		"hook.on_rtp_server_timeout": base + "/on_rtp_server_timeout",
		"hook.on_publish":            base + "/on_publish",
		"hook.on_play":               base + "/on_play",
		// 心跳周期(秒)
		"hook.alive_interval": "30.0",
		// 运行时策略
		"general.streamNoneReaderDelayMS": strconv.Itoa(media.StreamNoneReaderTimeout * 1000),
	}
	if c.node != nil && c.node.MediaServerUUID != "" {
		params["general.mediaServerId"] = c.node.MediaServerUUID
	}
	return c.SetServerConfig(ctx, params)
}
