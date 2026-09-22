package zlm

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
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
// 判据是 URL 的 `node` 查询参数 —— 那是 ApplyConfigForNode（`buildManagedHookURL`）
// 必然写进去的节点标识。解析失败时退化为"整串里是否出现该 uuid"，
// 宁可漏判（不动别人的配置）也不误判。
func ManagedHookTargetsNode(rawURL, mediaServerUUID string) bool {
	if rawURL == "" || mediaServerUUID == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return strings.Contains(rawURL, mediaServerUUID)
	}
	if parsed.Query().Get("node") == mediaServerUUID {
		return true
	}
	// 参数结构变了（例如被中间层重写过）时仍保留一次保守匹配。
	return strings.Contains(rawURL, mediaServerUUID)
}
