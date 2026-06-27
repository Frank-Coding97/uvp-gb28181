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

	// 含 ZLM 风格的 data 字段:计数标量 + 线程负载数组
	payload := []byte(`{
		"mediaServerId": "uuid-1",
		"data": {
			"MediaSource": 3,
			"Session": 5,
			"NetThreadLoad": [{"load": 0.2}, {"load": 0.4}],
			"WorkThreadLoad": [{"load": 0.1}, {"load": 0.3}, {"load": 0.5}],
			"memUsage": 12345678,
			"totalBytesIn": 1000,
			"totalBytesOut": 2000
		}
	}`)

	err := coll.Receive(payload)
	require.NoError(t, err)

	got, ok := reg.Get(id)
	require.True(t, ok)
	require.Equal(t, 3, got.Stats.MediaSourceCount)
	require.Equal(t, 5, got.Stats.SessionCount)
	require.InDelta(t, 0.3, got.Stats.NetThreadLoadAvg, 0.001)
	require.InDelta(t, 0.3, got.Stats.WorkThreadLoadAvg, 0.001)
	require.False(t, got.Stats.LastHeartbeatAt.IsZero(), "Collector 应填 LastHeartbeatAt")
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
