package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/service"
)

// 「服务配置」改 rtp_proxy.port_range 之后，平台登记的收流范围必须跟着真源走。
//
// 为什么必须有这条：登记的 rtp_port_start/end 只用来算端口容量（node.PortUsage），
// 它停在旧值时会一直按错的段判"是否接近满" —— 登记段比实际段大时调度永不判满，
// 直到 ZLM 端口真的耗光、推流开始失败。改动前那句脚注（"修改后还需同步节点登记的
// RTP 收流范围"）就是把这件事甩给了运维手工做。
func TestConfigService_UpdateRTPPortRangeSyncsRegisteredRange(t *testing.T) {
	cli := &mockZLMClient{}
	reg := fakeRegistry(t, node.Node{
		Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 35000,
	})
	id := reg.List()[0].ID

	resp, err := service.NewConfigService(reg, cli).Update(context.Background(), id,
		service.UpdateConfigReq{Changes: map[string]string{"rtp_proxy.port_range": "51200-51299"}})

	require.NoError(t, err)
	require.Contains(t, resp.RequiresRestart, "rtp_proxy.port_range")
	got, ok := reg.Get(id)
	require.True(t, ok)
	require.Equal(t, 51200, got.RTPPortStart)
	require.Equal(t, 51299, got.RTPPortEnd)
}

// 只改别的键时不许顺手动收流范围（别把"顺手对齐"做成"任何配置变更都重写一遍"）。
func TestConfigService_UpdateOtherKeyLeavesRegisteredRangeAlone(t *testing.T) {
	cli := &mockZLMClient{}
	reg := fakeRegistry(t, node.Node{
		Name: "n1", MediaServerUUID: "uuid-a", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 35000,
	})
	id := reg.List()[0].ID

	_, err := service.NewConfigService(reg, cli).Update(context.Background(), id,
		service.UpdateConfigReq{Changes: map[string]string{"hook.timeoutSec": "12"}})

	require.NoError(t, err)
	got, ok := reg.Get(id)
	require.True(t, ok)
	require.Equal(t, 30000, got.RTPPortStart)
	require.Equal(t, 35000, got.RTPPortEnd)
}

// 登记值对账失败（落库报错/并发 revision 冲突）**不许**把下发判成失败：
// ZLM 侧确实已经改成功了，报错只会诱导用户重复下发；下次启动探活会再对一次。
func TestConfigService_UpdateSurvivesRangeSyncFailure(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	added, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "127.0.0.1", APIPort: 18080, MediaServerUUID: "uuid-a",
		State: node.StateActive, RTPPortStart: 30000, RTPPortEnd: 35000,
	})
	require.NoError(t, err)
	repo.failOn = "update" // 对账那一次落库强制失败

	cli := &mockZLMClient{}
	resp, err := service.NewConfigService(reg, cli).Update(context.Background(), added.ID,
		service.UpdateConfigReq{Changes: map[string]string{"rtp_proxy.port_range": "51200-51299"}})

	require.NoError(t, err)
	require.Contains(t, resp.RequiresRestart, "rtp_proxy.port_range")
	got, ok := reg.Get(added.ID)
	require.True(t, ok)
	require.Equal(t, 30000, got.RTPPortStart, "落库失败时登记值保持原样，不许只改内存")
	require.Equal(t, 35000, got.RTPPortEnd)
}
