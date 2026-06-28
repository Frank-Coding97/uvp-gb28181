package heartbeat_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// 构造 registry + 预置节点,返回 registry 与节点 ID(便于断言)
func setupRegistry(t *testing.T, uuid string) (*node.Registry, int64) {
	t.Helper()
	r := node.NewRegistry(newMemoryRepo())
	added, err := r.Add(context.Background(), node.Node{
		Name:            "zlm-test",
		MediaServerUUID: uuid,
		State:           node.StateActive,
	})
	require.NoError(t, err)
	return r, added.ID
}

func TestCollector_ParsesKeepalivePayload(t *testing.T) {
	reg, id := setupRegistry(t, "uuid-1")
	coll := heartbeat.NewCollector(reg)

	// ZLM 真实 keepalive payload(直接抓自 WebApi.cpp getStatisticJson):
	// SessionCount = TcpSession + UdpSession;线程负载在 keepalive 里没有,
	// 由 ThreadLoadPoller 周期独立拉。
	payload := []byte(`{
		"mediaServerId": "uuid-1",
		"data": {
			"MediaSource": 3,
			"TcpSession": 2,
			"UdpSession": 3,
			"Socket": 130,
			"TcpServer": 96
		}
	}`)

	err := coll.Receive(payload)
	require.NoError(t, err)

	got, ok := reg.Get(id)
	require.True(t, ok)
	require.Equal(t, 3, got.Stats.MediaSourceCount)
	require.Equal(t, 5, got.Stats.SessionCount, "SessionCount = TcpSession + UdpSession")
	require.False(t, got.Stats.LastHeartbeatAt.IsZero(), "Collector 应填 LastHeartbeatAt")
}

func TestCollector_PreservesThreadLoad(t *testing.T) {
	// Collector 不应覆盖 ThreadLoadPoller 已经写入的 NetThread/WorkThread 字段
	reg, id := setupRegistry(t, "uuid-1")
	reg.UpdateStats("uuid-1", node.Stats{
		NetThreadLoadAvg:  0.5,
		WorkThreadLoadAvg: 0.3,
	})

	coll := heartbeat.NewCollector(reg)
	require.NoError(t, coll.Receive([]byte(`{"mediaServerId":"uuid-1","data":{"MediaSource":7,"TcpSession":1}}`)))

	got, _ := reg.Get(id)
	require.Equal(t, 7, got.Stats.MediaSourceCount)
	require.InDelta(t, 0.5, got.Stats.NetThreadLoadAvg, 0.001, "Collector 不应覆盖 NetThread")
	require.InDelta(t, 0.3, got.Stats.WorkThreadLoadAvg, 0.001, "Collector 不应覆盖 WorkThread")
}

func TestCollector_UnknownUUID_NoOp(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-known")
	coll := heartbeat.NewCollector(reg)

	payload := []byte(`{"mediaServerId":"uuid-nope","data":{"MediaSource":1}}`)
	// 不应 panic,不应报错(节点可能刚被删除,正常情况)
	require.NoError(t, coll.Receive(payload))
}

func TestCollector_InvalidJSON_ReturnsErr(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-1")
	coll := heartbeat.NewCollector(reg)

	require.Error(t, coll.Receive([]byte(`not a json`)))
}

func TestCollector_EmptyMediaServerId_ReturnsErr(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-1")
	coll := heartbeat.NewCollector(reg)

	// mediaServerId 缺失 → 无法反查节点,拒绝
	require.Error(t, coll.Receive([]byte(`{"data":{"MediaSource":1}}`)))
}
