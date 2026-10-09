package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

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
