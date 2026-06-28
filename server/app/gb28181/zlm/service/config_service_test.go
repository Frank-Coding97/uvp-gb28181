package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

// mockZLMClient stub 用于 config_service 测试
type mockZLMClient struct {
	getReturn       map[string]string
	getErr          error
	setErr          error
	lastSetParams   map[string]string
	getCalls        int
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
		"http.port":                       "80",
		"rtmp.port":                       "1935",
		"hook.enable":                     "1",
		"hook.on_stream_changed":          "http://x/y",
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
	var httpPort, hookEnable *service.ConfigItem
	for i := range grouped {
		for j := range grouped[i].Items {
			it := &grouped[i].Items[j]
			if it.Key == "http.port" {
				httpPort = it
			}
			if it.Key == "hook.enable" {
				hookEnable = it
			}
		}
	}
	require.NotNil(t, httpPort)
	require.NotNil(t, hookEnable)
	require.False(t, httpPort.HotReloadable, "http.port 应该需要重启")
	require.True(t, hookEnable.HotReloadable, "hook.enable 应该可热改")
}

func TestConfigService_Update_SplitsHotAndRestart(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID

	resp, err := svc.Update(context.Background(), id, service.UpdateConfigReq{
		Changes: map[string]string{
			"hook.enable": "1",
			"http.port":   "8080",
		},
	})
	require.NoError(t, err)
	require.Contains(t, resp.Applied, "hook.enable")
	require.Contains(t, resp.RequiresRestart, "http.port")
	require.NotContains(t, resp.Applied, "http.port", "需重启项不应在 Applied")
	require.Equal(t, map[string]string{"hook.enable": "1"}, cli.lastSetParams,
		"只应给 ZLM 下发热改项")
}

func TestConfigService_Update_AllHot_NoRestart(t *testing.T) {
	cli := &mockZLMClient{getReturn: map[string]string{}}
	reg := fakeRegistry(t, node.Node{Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive})
	svc := service.NewConfigService(reg, cli)
	id := reg.List()[0].ID

	resp, err := svc.Update(context.Background(), id, service.UpdateConfigReq{
		Changes: map[string]string{"hook.enable": "0"},
	})
	require.NoError(t, err)
	require.Empty(t, resp.RequiresRestart)
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
