package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

// writePortPlan 造一个最小的 config.env：带注释、带无关键、RTC 键是规划值。
// 端口规划文件里还有 SIP / Redis / HTTPS 一堆别的键，任何一处写丢都是事故，
// 所以测试必须放几条"不该被碰"的键进去。
func writePortPlan(t *testing.T, rtcPort, icePort string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.env")
	body := "# 端口规划（由打包脚本生成）\n" +
		"UVP_HTTP_PORT=51002\n" +
		"UVP_ZLM_HTTP_PORT=51004\n" +
		"UVP_ZLM_RTC_PORT=" + rtcPort + "\n" +
		"UVP_ZLM_RTP_RANGE=51014-51063\n" +
		"UVP_ZLM_ICE_PORT=" + icePort + "\n" +
		"\n" +
		"UVP_SIP_PORT=51064\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

// mockZLMClient stub 用于 config_service 测试
type mockZLMClient struct {
	getReturn     map[string]string
	getErr        error
	setErr        error
	lastSetParams map[string]string
	getCalls      int
}

func (m *mockZLMClient) GetServerConfig(_ context.Context, _ *node.Node) (map[string]string, error) {
	m.getCalls++
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.getReturn, nil
}
func (m *mockZLMClient) SetServerConfig(_ context.Context, _ *node.Node, params map[string]string) error {
	m.lastSetParams = params
	if m.getReturn == nil {
		m.getReturn = map[string]string{}
	}
	for key, value := range params {
		m.getReturn[key] = value
	}
	return m.setErr
}

func fakeRegistry(t *testing.T, nodes ...node.Node) *node.Registry {
	t.Helper()
	r := node.NewRegistry(newMemoryRepo())
	ctx := context.Background()
	for _, n := range nodes {
		_, err := r.Add(ctx, n)
		require.NoError(t, err)
	}
	return r
}

func TestConfigService_GetGrouped(t *testing.T) {
	full := map[string]string{
		"api.secret":                      "zlm-api-secret",
		"http.port":                       "80",
		"rtmp.port":                       "1935",
		"hook.enable":                     "1",
		"hook.on_stream_changed":          "http://x/y",
		"hook.on_stream_not_found":        "http://platform/index/hook/on_stream_not_found?cap=replayable-secret",
		"hook.on_flow_report":             "http://platform/index/hook/on_flow_report?cap=flow-secret",
		"general.streamNoneReaderDelayMS": "20000",
		"general.mediaServerId":           "uuid-a",
	}
	cli := &mockZLMClient{getReturn: full}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)

	n := reg.List()[0]
	grouped, err := svc.GetGrouped(context.Background(), n.ID)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, g := range grouped {
		names[g.Name] = true
	}
	require.True(t, names["网络端口"], "缺少分组 网络端口")
	require.True(t, names["Hook"] || names["Hook 回调"], "缺少分组 Hook")
	require.True(t, names["运行时策略"] || names["运行时"], "缺少分组 运行时")

	// 验证 hot_reloadable 标志
	var httpPort, hookEnable, streamNotFound, flowReport, apiSecret *service.ConfigItem
	for i := range grouped {
		for j := range grouped[i].Items {
			it := &grouped[i].Items[j]
			if it.Key == "http.port" {
				httpPort = it
			}
			if it.Key == "hook.enable" {
				hookEnable = it
			}
			if it.Key == "hook.on_stream_not_found" {
				streamNotFound = it
			}
			if it.Key == "hook.on_flow_report" {
				flowReport = it
			}
			if it.Key == "api.secret" {
				apiSecret = it
			}
		}
	}
	require.NotNil(t, httpPort)
	require.NotNil(t, hookEnable)
	require.False(t, httpPort.HotReloadable, "http.port 应该需要重启")
	require.True(t, hookEnable.HotReloadable, "hook.enable 应该可热改")
	require.NotNil(t, streamNotFound)
	require.NotContains(t, streamNotFound.Value, "replayable-secret")
	require.NotContains(t, streamNotFound.Value, "cap=")
	require.NotNil(t, flowReport)
	require.NotContains(t, flowReport.Value, "flow-secret")
	require.NotContains(t, flowReport.Value, "cap=")
	require.NotNil(t, apiSecret)
	require.Empty(t, apiSecret.Value)
}

func TestConfigService_WebRTCExternalIP(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{"rtc.externIP": ""}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID
	groups, err := svc.GetGrouped(context.Background(), id)
	require.NoError(t, err)
	var externalIP *service.ConfigItem
	for _, group := range groups {
		for _, item := range group.Items {
			if item.Key == "rtc.externIP" {
				copy := item
				externalIP = &copy
			}
		}
	}
	require.NotNil(t, externalIP, "服务配置页面必须展示 WebRTC 对外地址")
	require.Equal(t, service.ConfigModeHotReload, externalIP.Mode)
	require.Empty(t, externalIP.Value)
	for _, value := range []string{"62.234.152.39", ""} {
		result, err := svc.Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{"rtc.externIP": value}})
		require.NoError(t, err)
		require.Equal(t, []string{"rtc.externIP"}, result.Applied)
		require.Empty(t, result.RequiresRestart)
		require.Empty(t, result.Unknown)
		require.Equal(t, value, cli.lastSetParams["rtc.externIP"])
		require.Equal(t, value, cli.getReturn["rtc.externIP"])
	}
}

// WebRTC 在配置分类里是独立一组(不再散在「运行时策略」和「网络端口」里),四条硬约束:
//  1. 分组里必须有 rtc.externIP,且仍是可热更新 —— 它是跨网段部署唯一要人工填的项;
//  2. rtc.externIP 只能出现在 WebRTC 组里。两个组各留一份会让同一个 key 在页面上
//     出现两处,改一处另一处还显示旧值,看起来像"保存没生效";
//  3. 四个端口项可改(restart_required),但它们必须成对同号 —— 成对与持久化见下面
//     三个用例;这里只锁「模式」,避免有人把某一项悄悄改回只读;
//  4. rtc.port_range 保持只读:TURN 已被 ctl.sh 强制关闭,改了不生效。
func TestConfigService_WebRTCGroupIsStandalone(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{
		"rtc.externIP": "", "rtc.interfaces": "", "rtc.port": "51008", "rtc.tcpPort": "51008",
		"rtc.icePort": "51013", "rtc.iceTcpPort": "51013", "rtc.port_range": "49152-65535",
	}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)

	grouped, err := svc.GetGrouped(context.Background(), reg.List()[0].ID)
	require.NoError(t, err)

	groupsOfKey := map[string][]string{}
	webrtcItems := map[string]service.ConfigItem{}
	var runtimeGroup *service.ConfigGroup
	for i := range grouped {
		if grouped[i].Name == "运行时策略" {
			runtimeGroup = &grouped[i]
		}
		for _, item := range grouped[i].Items {
			groupsOfKey[item.Key] = append(groupsOfKey[item.Key], grouped[i].Name)
			if grouped[i].Name == "WebRTC" {
				webrtcItems[item.Key] = item
			}
		}
	}

	require.Equal(t, []string{"WebRTC"}, groupsOfKey["rtc.externIP"], "rtc.externIP 必须且只能属于 WebRTC 分组")
	require.NotNil(t, runtimeGroup)
	for _, item := range runtimeGroup.Items {
		require.NotEqual(t, "rtc.externIP", item.Key, "rtc.externIP 仍留在运行时策略里，等于同一项出现两次")
	}

	require.Equal(t, service.ConfigModeHotReload, webrtcItems["rtc.externIP"].Mode)
	require.Empty(t, webrtcItems["rtc.externIP"].Value)

	// rtc.interfaces 是第二个「地址类」项:它决定 ICE 往哪块网卡 bind,改完必须重启。
	require.Equal(t, service.ConfigModeRestartRequired, webrtcItems["rtc.interfaces"].Mode)

	for key, want := range map[string]string{
		"rtc.port": "51008", "rtc.tcpPort": "51008", "rtc.icePort": "51013", "rtc.iceTcpPort": "51013",
	} {
		item, ok := webrtcItems[key]
		require.True(t, ok, "%s 应该在 WebRTC 分组里可见", key)
		require.Equal(t, service.ConfigModeRestartRequired, item.Mode, "%s 应可改、改后需重启", key)
		require.Equal(t, want, item.Value, "%s 要显示 ZLM 实际值", key)
	}

	// TURN 端口池不跟着放开:TURN 被 ctl.sh 每次启动写 enableTurn=0,改了不生效,
	// 放开只会让人以为调通了。
	portRange, ok := webrtcItems["rtc.port_range"]
	require.True(t, ok)
	require.Equal(t, service.ConfigModeReadOnly, portRange.Mode)
	require.Equal(t, "49152-65535", portRange.Value)

	_, err = svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.port_range": "51065-51099"},
	})
	require.ErrorIs(t, err, service.ErrReadOnlyConfig)
	require.Nil(t, cli.lastSetParams, "只读项必须在任何下发之前就被拒收")
}

// 改 WebRTC 媒体端口 = 三件事必须同时成立，缺一条就是"看起来改了"：
//  1. UDP(rtc.port) 与 TCP(rtc.tcpPort) 同号 —— 只改一个，另一路监听会留在旧端口，
//     这正是 2026-10-12 那个「tcpPort 留在 8000 被占 ⇒ ZLM 启动即退出」的成因；
//  2. 新值写回部署端口规划(config.env)，否则 ctl.sh 下次启动按旧值重写 ini；
//  3. 响应里给出防火墙放行清单，端口改了不开防火墙 = 浏览器拿不到画面。
func TestConfigService_WebRTCPortChangePairsAndPersists(t *testing.T) {
	envPath := writePortPlan(t, "51008", "51013")
	cli := &mockZLMClient{getReturn: map[string]string{
		"rtc.port": "51008", "rtc.tcpPort": "51008", "rtc.icePort": "51013", "rtc.iceTcpPort": "51013",
	}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	svc.SetPortPlan(service.NewPortPlan(envPath))

	resp, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.port": "51070"},
	})
	require.NoError(t, err)

	require.Equal(t, "51070", cli.lastSetParams["rtc.port"])
	require.Equal(t, "51070", cli.lastSetParams["rtc.tcpPort"], "UDP 改了 TCP 必须自动跟上，否则复刻 8000 那个事故")
	require.NotContains(t, cli.lastSetParams, "rtc.icePort", "没提交的 STUN 端口不该被顺手下发")
	require.ElementsMatch(t, []string{"rtc.port", "rtc.tcpPort"}, resp.RequiresRestart)
	require.Empty(t, resp.Applied, "端口要重启才生效，不能报成已生效")

	persisted, readErr := os.ReadFile(envPath)
	require.NoError(t, readErr)
	body := string(persisted)
	require.Contains(t, body, "UVP_ZLM_RTC_PORT=51070")
	require.Contains(t, body, "UVP_USER_SET_UVP_ZLM_RTC_PORT=1", "没有用户改过标记，老包下次启动会按默认值覆盖")
	// 其它键必须逐字保留（这份文件里还有 SIP / Redis / 平台 HTTP 端口）
	require.Contains(t, body, "UVP_HTTP_PORT=51002")
	require.Contains(t, body, "UVP_ZLM_HTTP_PORT=51004")
	require.Contains(t, body, "UVP_ZLM_RTP_RANGE=51014-51063")
	require.Contains(t, body, "UVP_ZLM_ICE_PORT=51013")
	require.Contains(t, body, "UVP_SIP_PORT=51064")
	require.NotContains(t, body, "UVP_ZLM_RTC_PORT=51008")

	require.Len(t, resp.Hints, 1)
	require.Contains(t, resp.Hints[0], "51070/udp")
	require.Contains(t, resp.Hints[0], "51070/tcp")
}

func TestConfigService_WebRTCPortPairMustMatch(t *testing.T) {
	envPath := writePortPlan(t, "51008", "51013")
	cli := &mockZLMClient{getReturn: map[string]string{"rtc.port": "51008", "rtc.tcpPort": "51008"}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	svc.SetPortPlan(service.NewPortPlan(envPath))

	// 两个都给、但不同号：拒绝，而不是替用户选一个（选错就是一路监听留在旧端口）
	_, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.port": "51070", "rtc.tcpPort": "51071"},
	})
	require.ErrorIs(t, err, service.ErrInvalidConfigValue)
	require.Nil(t, cli.lastSetParams, "非同号必须在任何下发之前被拒收")

	// 0 没有"禁用"语义（ZLM 会 bind 随机端口），必须拒
	_, err = svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.icePort": "0"},
	})
	require.ErrorIs(t, err, service.ErrInvalidConfigValue)

	// 媒体端口和 STUN 端口都改成同一个号：两者都是 UDP 监听，必然抢不到 bind
	_, err = svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.port": "51070", "rtc.icePort": "51070"},
	})
	require.ErrorIs(t, err, service.ErrInvalidConfigValue)
	require.Nil(t, cli.lastSetParams)
}

// 端口撞车不会在保存时炸，而是下次启动 `address already in use` ⇒ ZLM 直接退出。
// 所以必须在**下发之前**就拿端口规划里的其它键比一遍。
func TestConfigService_WebRTCPortMustNotCollideWithPortPlan(t *testing.T) {
	cases := []struct {
		name    string
		changes map[string]string
		wantIn  string
	}{
		{
			name:    "落进 RTP 收流段",
			changes: map[string]string{"rtc.port": "51018"},
			wantIn:  "UVP_ZLM_RTP_RANGE=51014-51063",
		},
		{
			name:    "撞平台 HTTP 端口",
			changes: map[string]string{"rtc.port": "51002"},
			wantIn:  "UVP_HTTP_PORT=51002",
		},
		{
			name:    "媒体端口填成 STUN 端口（另一侧没改）",
			changes: map[string]string{"rtc.port": "51013"},
			wantIn:  "UVP_ZLM_ICE_PORT=51013",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envPath := writePortPlan(t, "51008", "51013")
			cli := &mockZLMClient{getReturn: map[string]string{"rtc.port": "51008", "rtc.tcpPort": "51008"}}
			reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
			svc := service.NewConfigService(reg, cli)
			svc.SetPortPlan(service.NewPortPlan(envPath))

			_, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: tc.changes})
			require.ErrorIs(t, err, service.ErrInvalidConfigValue)
			require.Contains(t, err.Error(), tc.wantIn, "报错要点出撞的是哪个键，否则用户无从下手")
			require.Nil(t, cli.lastSetParams, "撞车必须在任何下发之前被拒收")

			// 端口规划本身也不能被动过
			body, readErr := os.ReadFile(envPath)
			require.NoError(t, readErr)
			require.NotContains(t, string(body), "=51018")
		})
	}
}

func TestConfigService_WebRTCPortChangeWithoutPortPlanIsRejected(t *testing.T) {
	// 容器编排等部署方式没有 config.env ⇒ 无从持久化 ⇒ 必须在下发之前就拒，
	// 否则 ZLM 接受了、下次启动被改回，用户会以为是自己改错了。
	cli := &mockZLMClient{getReturn: map[string]string{"rtc.icePort": "51013", "rtc.iceTcpPort": "51013"}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)

	_, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.icePort": "51023"},
	})
	require.ErrorIs(t, err, service.ErrPortPlanUnavailable)
	require.Nil(t, cli.lastSetParams)
}

func TestConfigService_WebRTCPortMissingPortPlanFailsClosed(t *testing.T) {
	// 端口规划文件不存在（容器编排等）⇒ 无从持久化 ⇒ 必须在**下发之前**就拒，
	// 否则 ZLM 接受了、下次启动被改回，用户会以为是自己改错了。
	cli := &mockZLMClient{getReturn: map[string]string{"rtc.port": "51008", "rtc.tcpPort": "51008"}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	svc.SetPortPlan(service.NewPortPlan(filepath.Join(t.TempDir(), "missing.env")))

	_, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.port": "51070"},
	})
	require.ErrorIs(t, err, service.ErrPortPlanUnavailable)
	require.Nil(t, cli.lastSetParams, "文件缺失要在下发之前就暴露，不能先改 ZLM 再报错")
}

func TestConfigService_WebRTCPortPersistFailureIsReported(t *testing.T) {
	// 端口规划文件存在但写不进去（目录只读/磁盘问题）⇒ ZLM 侧已经改了，但下次启动
	// 会退回。必须失败而不是成功，否则用户重启后遇到"端口自己变回去了"且毫无线索。
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 chmod 不生效，无法稳定模拟目录不可写")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 会绕过目录权限")
	}
	envPath := writePortPlan(t, "51008", "51013")
	dir := filepath.Dir(envPath)
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	cli := &mockZLMClient{getReturn: map[string]string{"rtc.port": "51008", "rtc.tcpPort": "51008"}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	svc.SetPortPlan(service.NewPortPlan(envPath))

	_, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{
		Changes: map[string]string{"rtc.port": "51070"},
	})
	require.ErrorIs(t, err, service.ErrPortPlanPersistFailed)
	require.NotNil(t, cli.lastSetParams, "这条断言固定住失败发生的位置：ZLM 已下发，持久化才失败")
}

func TestPortPlanPersistIsIdempotentAndAddsMissingKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	// 只有 RTC 键，没有 ICE 键 —— 缺键必须追加，不能静默跳过
	body := "# 端口规划\nUVP_ZLM_RTC_PORT=51008\n\nUVP_SIP_PORT=51064\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	plan := service.NewPortPlan(path)

	require.NoError(t, plan.PersistWebRTCPorts(51018, 51023))
	first, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(first), "UVP_ZLM_RTC_PORT=51018")
	require.Contains(t, string(first), "UVP_ZLM_ICE_PORT=51023")
	require.Contains(t, string(first), "UVP_USER_SET_UVP_ZLM_ICE_PORT=1")
	require.Contains(t, string(first), "UVP_SIP_PORT=51064")
	require.Contains(t, string(first), "# 端口规划")

	// 同值再写一次：内容必须逐字不变（不能每保存一次就多几行）
	require.NoError(t, plan.PersistWebRTCPorts(51018, 51023))
	second, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(first), string(second))

	// 只改其中一个，另一个必须保持原值
	require.NoError(t, plan.PersistWebRTCPorts(0, 51029))
	third, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(third), "UVP_ZLM_RTC_PORT=51018")
	require.Contains(t, string(third), "UVP_ZLM_ICE_PORT=51029")
}

func TestConfigService_GetGroupedMarksEveryBooleanWithSharedStatusDictionary(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)

	grouped, err := svc.GetGrouped(context.Background(), reg.List()[0].ID)
	require.NoError(t, err)
	byKey := map[string]service.ConfigItem{}
	for _, group := range grouped {
		for _, item := range group.Items {
			byKey[item.Key] = item
		}
	}
	booleanKeys := []string{
		"hook.enable",
		"protocol.enable_rtsp", "protocol.enable_rtmp", "protocol.enable_hls", "protocol.enable_ts",
		"protocol.enable_fmp4", "protocol.enable_mp4", "protocol.enable_audio", "protocol.add_mute_audio",
		"general.publishToHls", "general.publishToMP4", "general.resetWhenRePlay",
		"rtp_proxy.checkSource", "api.apiDebug", "general.check_nvr_status",
	}
	for _, key := range booleanKeys {
		require.Equal(t, "status", byKey[key].DictCode, key)
	}
	for _, key := range []string{"http.port", "hook.timeoutSec", "general.mergeWriteMS", "rtp_proxy.h264_pt", "record.fileSecond"} {
		require.Empty(t, byKey[key].DictCode, key)
	}
	require.Equal(t, "是否开启 MP4 录制", byKey["protocol.enable_mp4"].Comment)
}

func TestConfigService_Update_SplitsHotAndRestart(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID

	resp, err := svc.Update(context.Background(), id, service.UpdateConfigReq{
		Changes: map[string]string{
			"hook.timeoutSec": "12",
			"http.port":       "8080",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"hook.timeoutSec"}, resp.Applied)
	require.Equal(t, []string{"http.port"}, resp.RequiresRestart)
	require.Equal(t, "8080", cli.lastSetParams["http.port"])
	require.Equal(t, "8080", cli.getReturn["http.port"])
}

func TestConfigService_NetworkPortsAreRestartRequiredAndPersisted(t *testing.T) {
	ports := map[string]string{
		"http.port": "18080", "http.sslport": "0", "rtmp.port": "0", "rtmp.sslport": "0",
		"rtsp.port": "0", "rtsp.sslport": "0", "rtp_proxy.port": "10000", "rtp_proxy.port_range": "30000-35000", "shell.port": "0",
	}
	cli := &mockZLMClient{getReturn: map[string]string{}}
	reg := fakeRegistry(t, node.Node{Name: "n1", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID
	groups, err := svc.GetGrouped(context.Background(), id)
	require.NoError(t, err)
	for _, group := range groups {
		if group.Name != "网络端口" {
			continue
		}
		require.Len(t, group.Items, len(ports))
		for _, item := range group.Items {
			require.Equal(t, service.ConfigModeRestartRequired, item.Mode, item.Key)
			require.True(t, item.RestartRequired, item.Key)
			require.False(t, item.HotReloadable, item.Key)
		}
	}
	resp, err := svc.Update(context.Background(), id, service.UpdateConfigReq{Changes: ports})
	require.NoError(t, err)
	require.Empty(t, resp.Applied)
	require.Len(t, resp.RequiresRestart, len(ports))
	require.Equal(t, ports, cli.lastSetParams)
	for key, value := range ports {
		require.Equal(t, value, cli.getReturn[key])
	}
}

func TestConfigService_InvalidNetworkPortBatchNeverSets(t *testing.T) {
	invalid := map[string][]string{
		"http.port":            {"", "0", "-1", "65536", "1.5", " 80", "1e3", "abc"},
		"http.sslport":         {"-1", "65536"},
		"rtmp.port":            {"-1", "65536"},
		"rtmp.sslport":         {"-1", "65536"},
		"rtsp.port":            {"-1", "65536"},
		"rtsp.sslport":         {"-1", "65536"},
		"shell.port":           {"-1", "65536"},
		"rtp_proxy.port":       {"-1", "65535", "abc"},
		"rtp_proxy.port_range": {"", "0-10", "500-499", "1-65536", "1,2", "1 - 2", "abc-def"},
	}
	for key, values := range invalid {
		for _, value := range values {
			t.Run(key+"="+value, func(t *testing.T) {
				cli := &mockZLMClient{}
				reg := fakeRegistry(t, node.Node{Name: "n1", State: node.StateActive})
				_, err := service.NewConfigService(reg, cli).Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: map[string]string{
					key: value, "hook.timeoutSec": "12",
				}})
				require.ErrorIs(t, err, service.ErrInvalidConfigValue)
				require.Nil(t, cli.lastSetParams)
			})
		}
	}
}

func TestConfigService_Update_AllHot_NoRestart(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID

	resp, err := svc.Update(context.Background(), id, service.UpdateConfigReq{
		Changes: map[string]string{"hook.timeoutSec": "15"},
	})
	require.NoError(t, err)
	require.Empty(t, resp.RequiresRestart)
}

func TestConfigService_CustomHooksAreEditableAndRoundTrip(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	changes := map[string]string{"hook.enable": "0"}
	for _, event := range playauth.ManagedHookEvents() {
		changes["hook."+string(event)] = "https://example.com/" + string(event) + "?node=uuid-a&authMode=jwt"
	}
	changes["hook.on_flow_report"] = ""
	resp, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: changes})
	require.NoError(t, err)
	require.Len(t, resp.Applied, len(changes))
	groups, err := svc.GetGrouped(context.Background(), reg.List()[0].ID)
	require.NoError(t, err)
	for _, group := range groups {
		for _, item := range group.Items {
			if expected, ok := changes[item.Key]; ok {
				require.Equal(t, service.ConfigModeHotReload, item.Mode)
				require.Equal(t, expected, item.Value)
			}
		}
	}
}

func TestConfigService_InvalidHookBatchNeverSets(t *testing.T) {
	for _, value := range []string{"ftp://example.com/hook", "javascript:alert(1)", "/relative", "https://", "https://user:pass@example.com/hook", "https://example.com/hook#fragment", " https://example.com/hook", "https://example.com:bad/hook", "https://example.com/hook?x=%zz"} {
		t.Run(value, func(t *testing.T) {
			cli := &mockZLMClient{}
			reg := fakeRegistry(t, node.Node{Name: "n1", State: node.StateActive})
			_, err := service.NewConfigService(reg, cli).Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: map[string]string{"hook.enable": "1", "hook.on_play": value}})
			require.Error(t, err)
			require.Nil(t, cli.lastSetParams)
			require.NotContains(t, err.Error(), value)
		})
	}
	cli := &mockZLMClient{}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	_, err := service.NewConfigService(reg, cli).Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: map[string]string{"hook.enable": "2"}})
	require.Error(t, err)
	require.Nil(t, cli.lastSetParams)
	_, err = service.NewConfigService(reg, cli).Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: map[string]string{"hook.on_play": "https://custom.example/play?node=" + reg.List()[0].MediaServerUUID + "&cap=forged"}})
	require.ErrorIs(t, err, service.ErrInvalidConfigValue)
	require.Nil(t, cli.lastSetParams)
}

func TestConfigService_HookParametersRemainVisible(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{"hook.enable": "1", "hook.timeoutSec": "10", "hook.alive_interval": "30.0"}}
	reg := fakeRegistry(t, node.Node{Name: "n1", State: node.StateActive})
	groups, err := service.NewConfigService(reg, cli).GetGrouped(context.Background(), reg.List()[0].ID)
	require.NoError(t, err)
	for _, group := range groups {
		for _, item := range group.Items {
			if value, ok := cli.getReturn[item.Key]; ok {
				require.Equal(t, value, item.Value)
			}
		}
	}
}

func TestConfigService_HeartbeatIntervalCanBeSavedForRestart(t *testing.T) {
	for _, value := range []string{"15", "45.5"} {
		t.Run(value, func(t *testing.T) {
			cli := &mockZLMClient{getReturn: map[string]string{}}
			reg := fakeRegistry(t, node.Node{Name: "n1", State: node.StateActive})
			svc := service.NewConfigService(reg, cli)
			resp, err := svc.Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: map[string]string{
				"hook.alive_interval": value, "hook.timeoutSec": "12",
			}})
			require.NoError(t, err)
			require.Equal(t, []string{"hook.alive_interval"}, resp.RequiresRestart)
			require.Equal(t, []string{"hook.timeoutSec"}, resp.Applied)
			groups, err := svc.GetGrouped(context.Background(), reg.List()[0].ID)
			require.NoError(t, err)
			for _, group := range groups {
				for _, item := range group.Items {
					if item.Key == "hook.alive_interval" {
						require.Equal(t, service.ConfigMode("restart_required"), item.Mode)
						require.False(t, item.HotReloadable)
						require.True(t, item.RestartRequired)
						require.Equal(t, value, item.Value)
					}
				}
			}
		})
	}
}

func TestConfigService_InvalidHeartbeatIntervalNeverSets(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "NaN", "Inf", "1e100", "1e-100", "abc", " 30 "} {
		t.Run(value, func(t *testing.T) {
			cli := &mockZLMClient{}
			reg := fakeRegistry(t, node.Node{Name: "n1", State: node.StateActive})
			_, err := service.NewConfigService(reg, cli).Update(context.Background(), reg.List()[0].ID, service.UpdateConfigReq{Changes: map[string]string{
				"hook.alive_interval": value, "hook.timeoutSec": "12",
			}})
			require.ErrorIs(t, err, service.ErrInvalidConfigValue)
			require.Nil(t, cli.lastSetParams)
		})
	}
}

func TestConfigService_UpdateRejectsPlatformManagedAutoOnDemandKeys(t *testing.T) {
	cli := &mockZLMClient{}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID

	for _, key := range []string{
		"api.secret", "general.flowThreshold", "general.mediaServerId",
	} {
		_, err := svc.Update(context.Background(), id, service.UpdateConfigReq{Changes: map[string]string{key: "tampered"}})
		require.ErrorIs(t, err, service.ErrManagedConfigKey, key)
	}
	require.Nil(t, cli.lastSetParams)
}

func TestConfigService_Update_NodeNotFound(t *testing.T) {
	cli := &mockZLMClient{}
	reg := fakeRegistry(t)
	svc := service.NewConfigService(reg, cli)
	_, err := svc.Update(context.Background(), 9999, service.UpdateConfigReq{Changes: map[string]string{"hook.enable": "1"}})
	require.ErrorIs(t, err, service.ErrNodeNotFound)
}

func TestConfigService_TestConnection_Online(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{"http.port": "80"}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID

	res, err := svc.TestConnection(context.Background(), id)
	require.NoError(t, err)
	require.True(t, res.Online)
}

func TestConfigService_TestConnection_Offline(t *testing.T) {
	cli := &mockZLMClient{getErr: errors.New("connection refused")}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID

	res, err := svc.TestConnection(context.Background(), id)
	require.NoError(t, err)
	require.False(t, res.Online)
	require.NotEmpty(t, res.Error)
}
