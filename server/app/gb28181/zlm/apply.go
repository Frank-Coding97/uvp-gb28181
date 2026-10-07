package zlm

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
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
	current, err := c.GetServerConfig(ctx)
	if err != nil {
		return fmt.Errorf("读取 ZLM 现有配置失败: %w", err)
	}
	initialized := current["general.mediaServerId"] == c.node.MediaServerUUID && c.node.MediaServerUUID != ""
	base, err := media.EffectiveHookBaseURL()
	if err != nil {
		return fmt.Errorf("ZLM Hook 回调基址不可用: %w", err)
	}
	params := map[string]string{
		"protocol.mp4_max_second": "3600",
		// 运行时策略
		"general.streamNoneReaderDelayMS": strconv.Itoa(media.StreamNoneReaderTimeout * 1000),
		"general.flowThreshold":           "0",
	}
	if !initialized {
		params["hook.enable"] = "1"
	}
	params["general.mediaServerId"] = c.node.MediaServerUUID
	for _, event := range playauth.ManagedHookEvents() {
		key := "hook." + string(event)
		// ZLM persists setServerConfig to config.ini. 只刷新**平台自己的**回调：
		// ⓐ 带本节点 node+cap(= 现在就是我们写的)；
		// ⓑ 路径恰好是本平台自己的 hook 端点 —— 覆盖"旧版本写的 / 运维手改的、丢了 cap
		//    的死 hook"。少了 ⓑ 这类值会被当成 user-owned 永久保留，表现为回调全丢但无告警，
		//    见 ManagedHookClaimsEndpoint 的现场记录。
		// 其余（包括空值）按 user-owned 处理，不碰。
		if initialized {
			if !ManagedHookTargetsNode(current[key], c.node.MediaServerUUID) &&
				!ManagedHookClaimsEndpoint(current[key], event) {
				continue
			}
		}
		hookURL, buildErr := buildManagedHookURL(base, c.node.APISecret, c.node.MediaServerUUID, event)
		if buildErr != nil {
			return buildErr
		}
		params[key] = hookURL
	}
	if err := c.SetServerConfig(ctx, params); err != nil {
		return err
	}
	applied, err := c.GetServerConfig(ctx)
	if err != nil {
		return fmt.Errorf("回读 ZLM 配置失败: %w", err)
	}
	readbackKeys := []string{"general.flowThreshold", "general.mediaServerId"}
	if !initialized {
		readbackKeys = append(readbackKeys, "hook.enable")
	}
	for _, event := range playauth.ManagedHookEvents() {
		key := "hook." + string(event)
		if _, applied := params[key]; applied {
			readbackKeys = append(readbackKeys, key)
		}
	}
	for _, key := range readbackKeys {
		if applied[key] != params[key] {
			return fmt.Errorf("ZLM 配置回读不一致: %s", key)
		}
	}
	return nil
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
