package zlm

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

// UnprovisionHooks 撤销平台自己写进对端 ZLM 的 managed hook，让一个**已被移除**的
// 节点停止回调本平台。
//
// 背景：`Registry.Delete` 只删本平台的行与内存索引，**不通知对端**。对端的 hook 配置
// 是平台自己写进去的（见 apply.go 的 ApplyConfigForNode），删行之后它毫不知情，仍按
// 自己的周期回调 ⇒ 平台每条都记一次 `node_unknown`。本方法是对 ApplyConfigForNode 的
// **逆操作**：把当初写进去的那几条 URL 清空。
//
// ⛔ 只清**平台自己写的**那些：判据是 hook URL 的 `node` 查询参数等于本节点的 uuid
// （那是 ApplyConfigForNode 必然写进去的标记）。对端上别人写的 hook（`on_send_rtp_stopped`
// 之类，或另一套平台配的 URL）一律不碰 —— 换主人的 IP 上尤其重要。
//
// ⛔ 不清 `hook.enable`：它是全局总开关，置 0 会把对端上**别的** hook 也一起关掉。
// 逐条清 URL 已足够：ZLM 的每个 hook 都要求自己的 URL 非空才触发
// （`server/WebHook.cpp` 里 `if (!hook_enable || hook_xxx.empty()) return;`）。
//
// 写后**回读校验**：`setServerConfig` 成功只代表请求被受理，不代表值已经变了。
//
// ⚠️ **清空 `on_server_keepalive` 会在对端留下周期性异常**（2026-09-21 现场实测）：
// ZLM 的 keepalive Timer 只在启动时创建、且只在**那一刻**判一次 URL 是否为空
// （`server/WebHook.cpp` 的 `reportServerKeepalive()`）。启动时非空、之后被清空
// ⇒ Timer 继续跑，每周期 `do_http_hook("")` 抛一次 `非法的http url`
// （`Timer.cpp:31`，实测每 10s 一条）。
//
// 这**不影响平台**（是对端本地日志），且对端一旦重启（启动时 URL 为空 ⇒ 根本不建 Timer）
// 或重新接入本平台（`ApplyConfigForNode` 会覆盖）就消失。仍然选择清空的理由：
// 让对端**不再回调我们**是首要目标，对端一条本地异常远好于平台 8640 行/天。
// 对端是**共用** ZLM 时也用不了 `hook.enable=0` 规避（那会连别人的 hook 一起关）。
func (c *Client) UnprovisionHooks(ctx context.Context) error {
	if c == nil || c.node == nil {
		return fmt.Errorf("ZLM 节点未绑定")
	}
	current, err := c.GetServerConfig(ctx)
	if err != nil {
		return fmt.Errorf("读取 ZLM 配置失败: %w", err)
	}
	params := ManagedHookRevocation(current, c.node.MediaServerUUID)
	if len(params) == 0 {
		// 对端没有任何一条指向本节点的 managed hook —— 已经解约过，或者从没配上。
		// 这不是错误：解约的目标（"它不再回调我们"）本来就已经成立。
		return nil
	}
	if err := c.SetServerConfig(ctx, params); err != nil {
		return fmt.Errorf("清空 ZLM Hook 配置失败: %w", err)
	}
	applied, err := c.GetServerConfig(ctx)
	if err != nil {
		return fmt.Errorf("回读 ZLM 配置失败: %w", err)
	}
	for key := range params {
		if strings.TrimSpace(applied[key]) != "" {
			return fmt.Errorf("ZLM 配置回读不一致: %s 仍未清空", key)
		}
	}
	return nil
}

// ManagedHookRevocation 从对端当前配置里算出"要清空哪些 hook 键"。
//
// 纯函数（不碰网络），便于对"只清自己的"这条硬约束做针对性测试。
// 返回空 map 表示"对端没有指向本节点的 hook"，调用方据此短路。
func ManagedHookRevocation(current map[string]string, mediaServerUUID string) map[string]string {
	if mediaServerUUID == "" {
		return nil
	}
	params := make(map[string]string)
	for _, event := range playauth.ManagedHookEvents() {
		key := "hook." + string(event)
		raw := strings.TrimSpace(current[key])
		if raw == "" {
			continue
		}
		if !ManagedHookTargetsNode(raw, mediaServerUUID) {
			// 这条不是我们写的（或已被别人覆盖）⇒ 不碰。
			continue
		}
		params[key] = ""
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

// ManagedHookTargetsNode 判断一条 hook URL 是否指向指定节点。
//
// 平台回调同时带当前节点的 node 标识和 cap 凭据。
// 自定义 URL 仅包含 UUID 或解析失败时不能视为平台所有。
func ManagedHookTargetsNode(rawURL, mediaServerUUID string) bool {
	if rawURL == "" || mediaServerUUID == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	return parsed.Query().Get("node") == mediaServerUUID && parsed.Query().Get("cap") != ""
}

// ManagedHookClaimsEndpoint 判断一条 hook URL 是否**打在本平台自己的 managed hook 端点上**。
//
// 与 ManagedHookTargetsNode 的分工（一个管"写"、一个管"删"）：
//   - ManagedHookTargetsNode = "能证明是我们写的"（带本节点 node + cap）⇒ 删除侧用它：
//     UnprovisionHooks 只清自己写的，绝不动同一台 ZLM 上别人配的回调。
//   - 本函数 = "这条回调打的是我们自己的端点" ⇒ 下发侧用它：ApplyConfigForNode 该不该覆盖。
//
// ⛔ 为什么下发侧必须认路径：`cap`/`node` 是平台写进去的标记，但**旧版本写的、运维手改的、
// 或从文档/测试里抄来的**值不带它们。只看 cap+node 会把这类值判成 user-owned ⇒ 永久保留
// ⇒ 得到一条"ZLM 认为回调成功、平台按 credentials_invalid 静默丢弃"的死 hook：
// 平台一条事件都收不到，**且没有任何告警**。
//
// 2026-10-03 现场即此：ZLM 上 `hook.on_stream_changed` 停在
// `http://192.168.0.204:8280/index/hook/on_stream_changed`（旧 IP、无 cap）⇒
// `ObserveTalkStream(regist=true)` 永不触发 ⇒ 对讲/广播的激活压根不开始（会话卡在
// publishing、`signal_phase` 空、`broadcast_sn=0`，30 秒后租约过期），
// 流注销（regist=false）也一并丢失 ⇒ 浏览器停推后会话不会自动收尾。
//
// 判据刻意保守：仅 http/https、host 非空、**路径恰好**等于 `/index/hook/<event>`。
// 指向别的路径的自定义 URL（别人的中继服务、另一套平台的换路径部署）一律不认。
func ManagedHookClaimsEndpoint(rawURL string, event playauth.HookEvent) bool {
	if strings.TrimSpace(rawURL) == "" || !event.Valid() {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	return strings.TrimSuffix(parsed.Path, "/") == "/index/hook/"+string(event)
}
