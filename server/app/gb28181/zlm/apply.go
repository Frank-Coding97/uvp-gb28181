package zlm

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

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
	// ⛔⛔⛔ `initialized` **不能只看 mediaServerId**，但也**不能只看 hook.enable**（2026-10-09 修）
	//
	// 现场症状：绿色包出包时把 `mediaserverid` **预置**进 config.ini（为了让它是固定值），
	//   ZLM 启动即读入 ⇒ 首次启动 `mediaServerId` 就已匹配
	//   ⇒ 原判据 initialized=true ⇒ 跳过 `hook.enable=1`
	//   ⇒ 再加上下面循环的两道 continue（现存值为空 ⇒ 既不带本节点 cap、
	//      也不是本平台 hook 端点）⇒ **ManagedHookEvents 全被跳过，回调一个都没下发**
	//   ⇒ ZLM 侧 `hook.on_*` 全空、`hook.enable=0`，而日志照样打「启动收敛完成」。
	//
	// ⚠️ 但**不能**把判据改成「mediaServerId 匹配 且 hook.enable=1」——
	//   那会破坏既有契约「**hook.enable=0 是用户的显式选择，要尊重**」
	//   （见 TestApplyConfigForNodePreservesCustomHooksOnRepeatedApply /
	//     TestApplyConfigForNodeRefreshesPlatformHookButPreservesDisabledSwitch）。
	//
	// ✅ 正确判据：**「用户是否曾经管过这些回调」**，而不是「hook 开着没有」。
	//   出包预置的 medianserverid 不算「管过」—— 判据是**现存回调值里有没有本平台的东西**：
	//     · 一个本平台该管的回调都没有 ⇒ 从未下发过 ⇒ 要下发（并同时打开 hook.enable）
	//     · 已经有过（无论是本平台的、还是 user-owned 的）⇒ 尊重现状，只刷新该刷新的
	hasManagedHook := false
	for _, event := range playauth.ManagedHookEvents() {
		if strings.TrimSpace(current["hook."+string(event)]) != "" {
			hasManagedHook = true
			break
		}
	}
	// 身份已匹配（认得这台节点）**且**回调已有人管过 ⇒ 才算 initialized
	initialized := current["general.mediaServerId"] == c.node.MediaServerUUID &&
		c.node.MediaServerUUID != "" && hasManagedHook
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
		// hook 未启用时**连同全部回调一起**下发，否则下面循环会把它们全continue 掉
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
