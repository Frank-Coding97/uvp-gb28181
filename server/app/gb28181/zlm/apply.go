package zlm

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const AutoOnDemandStreamWaitMS = 30000

// ApplyConfigForNode 启动时把运行时策略动态下发给 ZLM,并写入 mediaServerId
// 让 ZLM 后续 on_server_keepalive 等回调带上该 UUID 用于反查节点。
//
// 多节点场景:每节点首次启动调用一次。
func (c *Client) ApplyConfigForNode(ctx context.Context, media gbconfig.MediaConfig) error {
	if c.node == nil {
		return fmt.Errorf("ZLM 节点未绑定")
	}
	params, err := ExpectedConfigForNode(c.node, media)
	if err != nil {
		return err
	}
	if err := c.SetServerConfig(ctx, params); err != nil {
		return err
	}
	applied, err := c.GetServerConfig(ctx)
	if err != nil {
		return fmt.Errorf("回读 ZLM 配置失败: %w", err)
	}
	readbackKeys := []string{"hook.enable", "general.flowThreshold", "general.maxStreamWaitMS", "general.mediaServerId"}
	if media.ManageRTCExternIP {
		readbackKeys = append(readbackKeys, "rtc.externIP")
	}
	for _, event := range playauth.ManagedHookEvents() {
		readbackKeys = append(readbackKeys, "hook."+string(event))
	}
	for _, key := range readbackKeys {
		if applied[key] != params[key] {
			return fmt.Errorf("ZLM 配置回读不一致: %s", key)
		}
	}
	return nil
}

// ExpectedConfigForNode builds the runtime values that ApplyConfigForNode
// writes to ZLM. Callers that only need to validate external state can reuse
// the exact Hook capability calculation without issuing a SetServerConfig.
// The returned map is new on every call and may be freely modified by the
// caller.
func ExpectedConfigForNode(n *node.Node, media gbconfig.MediaConfig) (map[string]string, error) {
	if n == nil {
		return nil, fmt.Errorf("ZLM 节点未绑定")
	}
	base, err := media.EffectiveHookBaseURL()
	if err != nil {
		return nil, fmt.Errorf("ZLM Hook 回调基址不可用: %w", err)
	}
	params := map[string]string{
		"hook.enable": "1",
		// 心跳周期(秒)
		"hook.alive_interval":     "30.0",
		"protocol.mp4_max_second": "3600",
		// 运行时策略
		"general.streamNoneReaderDelayMS": strconv.Itoa(media.StreamNoneReaderTimeout * 1000),
		"general.maxStreamWaitMS":         strconv.Itoa(AutoOnDemandStreamWaitMS),
		"general.flowThreshold":           "0",
		"general.mediaServerId":           n.MediaServerUUID,
	}
	for _, event := range playauth.ManagedHookEvents() {
		hookURL, buildErr := buildManagedHookURL(base, n.APISecret, n.MediaServerUUID, event)
		if buildErr != nil {
			return nil, buildErr
		}
		params["hook."+string(event)] = hookURL
	}
	if media.ManageRTCExternIP {
		ip := net.ParseIP(n.ReceiveHost)
		if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
			return nil, fmt.Errorf("媒体收流地址必须为具体 IPv4 地址")
		}
		params["rtc.externIP"] = ip.String()
	}
	return params, nil
}

func buildManagedHookURL(base *url.URL, apiSecret, mediaServerID string, event playauth.HookEvent) (string, error) {
	capability, err := playauth.HookCapability(apiSecret, mediaServerID, event)
	if err != nil {
		return "", fmt.Errorf("生成 ZLM Hook 回调凭据失败: %s", event)
	}
	raw, err := url.JoinPath(base.String(), string(event))
	if err != nil {
		return "", fmt.Errorf("构造 ZLM Hook 回调地址失败: %s", event)
	}
	hookURL, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("构造 ZLM Hook 回调地址失败: %s", event)
	}
	query := hookURL.Query()
	query.Set("node", mediaServerID)
	query.Set("cap", capability)
	hookURL.RawQuery = query.Encode()
	return hookURL.String(), nil
}
