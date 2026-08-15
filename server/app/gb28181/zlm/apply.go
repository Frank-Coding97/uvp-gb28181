package zlm

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// hookPath Hook 端点统一前缀(挂在后端 HTTP 服务下)
const HookBasePath = "/index/hook"

const AutoOnDemandStreamWaitMS = 30000

// ApplyConfigForNode 启动时把运行时策略动态下发给 ZLM,并写入 mediaServerId
// 让 ZLM 后续 on_server_keepalive 等回调带上该 UUID 用于反查节点。
//
// 多节点场景:每节点首次启动调用一次。
func (c *Client) ApplyConfigForNode(ctx context.Context, media gbconfig.MediaConfig) error {
	base := fmt.Sprintf("http://%s:%d%s", media.HookHost, media.HookPort, HookBasePath)
	if c.node == nil {
		return fmt.Errorf("ZLM 节点未绑定")
	}
	capability, err := playauth.CallbackCapability(c.node.APISecret, c.node.MediaServerUUID)
	if err != nil {
		return fmt.Errorf("生成 ZLM 回调凭据失败: %w", err)
	}
	notFoundURL, err := url.Parse(base + "/on_stream_not_found")
	if err != nil {
		return fmt.Errorf("构造 ZLM 缺流回调地址失败: %w", err)
	}
	query := notFoundURL.Query()
	query.Set("cap", capability)
	notFoundURL.RawQuery = query.Encode()
	flowReportURL, err := url.Parse(base + "/on_flow_report")
	if err != nil {
		return fmt.Errorf("构造 ZLM 流量回调地址失败: %w", err)
	}
	query = flowReportURL.Query()
	query.Set("cap", capability)
	flowReportURL.RawQuery = query.Encode()
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
		"hook.on_record_mp4":         base + "/on_record_mp4",
		"hook.on_stream_not_found":   notFoundURL.String(),
		"hook.on_flow_report":        flowReportURL.String(),
		// 心跳周期(秒)
		"hook.alive_interval":     "30.0",
		"protocol.mp4_max_second": "3600",
		// 运行时策略
		"general.streamNoneReaderDelayMS": strconv.Itoa(media.StreamNoneReaderTimeout * 1000),
		"general.maxStreamWaitMS":         strconv.Itoa(AutoOnDemandStreamWaitMS),
		"general.flowThreshold":           "0",
	}
	params["general.mediaServerId"] = c.node.MediaServerUUID
	if err := c.SetServerConfig(ctx, params); err != nil {
		return err
	}
	applied, err := c.GetServerConfig(ctx)
	if err != nil {
		return fmt.Errorf("回读 ZLM 配置失败: %w", err)
	}
	for _, key := range []string{"hook.enable", "hook.on_stream_not_found", "hook.on_flow_report", "general.flowThreshold", "general.maxStreamWaitMS", "general.mediaServerId"} {
		if applied[key] != params[key] {
			return fmt.Errorf("ZLM 配置回读不一致: %s", key)
		}
	}
	return nil
}
