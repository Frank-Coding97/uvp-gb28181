package zlm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// ApplyConfigForNode 的逆操作必须只清**平台自己写的**那几条 hook。
//
// 判据是 hook URL 上的 `node` 查询参数（`buildManagedHookURL` 必然写进去的节点标识）。
// 这条约束在"IP 易主"时是安全边界：对端可能已经在被另一个平台/另一个节点使用，
// 清错一条就是改别人的配置。
func TestManagedHookRevocationOnlyClearsHooksOfThisNode(t *testing.T) {
	const mine = "56e37a2a-41ff-4346-832b-58c24ae93911"
	const theirs = "f36d056a-0000-0000-0000-000000000000"
	base := "http://192.168.10.106:8280/gb28181/hook"

	current := map[string]string{
		"hook.on_server_started":     hookURL(base, playauth.HookOnServerStarted, mine),
		"hook.on_server_keepalive":   hookURL(base, playauth.HookOnServerKeepalive, mine),
		"hook.on_stream_changed":     hookURL(base, playauth.HookOnStreamChanged, mine),
		"hook.on_stream_none_reader": hookURL(base, playauth.HookOnStreamNoneReader, mine),
		"hook.on_rtp_server_timeout": hookURL(base, playauth.HookOnRTPServerTimeout, mine),
		"hook.on_publish":            hookURL(base, playauth.HookOnPublish, mine),
		"hook.on_play":               hookURL(base, playauth.HookOnPlay, mine),
		"hook.on_flow_report":        hookURL(base, playauth.HookOnFlowReport, mine),
		"hook.on_stream_not_found":   hookURL(base, playauth.HookOnStreamNotFound, mine),
		"hook.on_record_mp4":         hookURL(base, playauth.HookOnRecordMP4, mine),
		// 同名的 key，但指向**别的节点** —— 说明这个实例已经被另一个平台接管，不能动。
		"hook.on_server_exited": "",
	}

	params := ManagedHookRevocation(current, mine)
	require.Len(t, params, len(playauth.ManagedHookEvents()),
		"平台写进去的每一条 managed hook 都要被清掉")
	for _, event := range playauth.ManagedHookEvents() {
		key := "hook." + string(event)
		require.Contains(t, params, key)
		require.Equal(t, "", params[key])
	}
	// hook.on_server_exited 不在 managed 列表里（平台从没写过它），一个字节都不碰。
	require.NotContains(t, params, "hook.on_server_exited")

	// 换成"全是对端自己的配置"：一条都不该清，返回 nil（调用方据此短路）。
	foreign := map[string]string{
		"hook.on_server_keepalive": hookURL("http://10.0.0.9:9000/hook", playauth.HookOnServerKeepalive, theirs),
		"hook.on_publish":          hookURL("http://10.0.0.9:9000/hook", playauth.HookOnPublish, theirs),
		"hook.on_send_rtp_stopped": "http://10.0.0.9:9000/stopped",
	}
	require.Nil(t, ManagedHookRevocation(foreign, mine),
		"对端上别人写的 hook 必须原样留着")
}

func TestManagedHookRevocationSkipsEmptyAndUnknown(t *testing.T) {
	const mine = "mine-uuid"
	tests := []struct {
		name    string
		current map[string]string
		want    int
	}{
		{name: "nil config", current: nil, want: 0},
		{name: "all empty", current: map[string]string{"hook.on_publish": ""}, want: 0},
		{name: "only whitespace", current: map[string]string{"hook.on_publish": "   "}, want: 0},
		{
			name:    "our hook present",
			current: map[string]string{"hook.on_publish": hookURL("http://p/hook", playauth.HookOnPublish, mine)},
			want:    1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Len(t, ManagedHookRevocation(tc.current, mine), tc.want)
		})
	}
	// 没有 uuid 就没有归属判据 ⇒ 一条都不清（宁可留着，不可误伤）。
	require.Nil(t, ManagedHookRevocation(
		map[string]string{"hook.on_publish": hookURL("http://p/hook", playauth.HookOnPublish, mine)}, ""))
}

func TestManagedHookTargetsNode(t *testing.T) {
	const uuid = "abc-123"
	require.True(t, ManagedHookTargetsNode("http://p/hook/on_publish?node=abc-123&cap=x", uuid))
	require.False(t, ManagedHookTargetsNode("http://p/hook/on_publish?node=other&cap=x", uuid))
	require.False(t, ManagedHookTargetsNode("", uuid))
	require.False(t, ManagedHookTargetsNode("http://p/hook/on_publish?node=abc-123", ""))
	// 解析失败时退化为整串包含判断：宁可漏判（不动别人的配置），不误判。
	require.True(t, ManagedHookTargetsNode("::::not a url::::abc-123", uuid))
	require.False(t, ManagedHookTargetsNode("::::not a url::::", uuid))
}

// 回归锚点：现场那个幽灵节点的 9 条 hook 全都要能被算出来（就是当年 `eeyelog-zlm` 的形态）。
func TestManagedHookRevocationMatchesObservedGhostNodeConfig(t *testing.T) {
	const ghost = "56e37a2a-41ff-4346-832b-58c24ae93911"
	base := "http://192.168.10.106:8280/gb28181/hook/"
	current := make(map[string]string, len(playauth.ManagedHookEvents()))
	for _, event := range playauth.ManagedHookEvents() {
		current["hook."+string(event)] = base + string(event) + "?node=" + ghost + "&cap=deadbeef"
	}
	current["hook.enable"] = "1"
	current["hook.alive_interval"] = "30.0"
	current["general.mediaServerId"] = ghost

	params := ManagedHookRevocation(current, ghost)
	require.Len(t, params, 10, "实测现场 10 条 managed hook 全部命中")
	// 总开关与节点身份**不在**撤销范围内：置 hook.enable=0 会把对端上别的 hook 一起关掉，
	// 而 mediaServerId 是对方自己的身份，平台无权改。
	require.NotContains(t, params, "hook.enable")
	require.NotContains(t, params, "general.mediaServerId")
	for key := range params {
		require.True(t, strings.HasPrefix(key, "hook."), "只清 hook.* 键")
	}
}

func hookURL(base string, event playauth.HookEvent, uuid string) string {
	return base + "/" + string(event) + "?node=" + uuid + "&cap=deadbeef"
}
