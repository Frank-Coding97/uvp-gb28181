package zlm

import (
	"context"
	"fmt"
	"strconv"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

// hookPath Hook 端点统一前缀(挂在后端 HTTP 服务下)
const HookBasePath = "/index/hook"

// ApplyConfig 启动时把运行时策略动态下发给 ZLM:
// Hook 回调地址(指向后端)、无人观看超时、收流超时等。
// 替代 config.ini 写死,呼应"控制面动态下发"设计。
func (c *Client) ApplyConfig(ctx context.Context, media gbconfig.MediaConfig) error {
	base := fmt.Sprintf("http://%s:%d%s", media.HookHost, media.HookPort, HookBasePath)
	params := map[string]string{
		// Hook 全套回调地址
		"hook.enable":                  "1",
		"hook.on_server_started":       base + "/on_server_started",
		"hook.on_stream_changed":       base + "/on_stream_changed",
		"hook.on_stream_none_reader":   base + "/on_stream_none_reader",
		"hook.on_rtp_server_timeout":   base + "/on_rtp_server_timeout",
		"hook.on_publish":              base + "/on_publish",
		"hook.on_play":                 base + "/on_play",
		// 运行时策略
		"general.streamNoneReaderDelayMS": strconv.Itoa(media.StreamNoneReaderTimeout * 1000),
	}
	return c.SetServerConfig(ctx, params)
}
