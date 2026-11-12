package node_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

// 收流范围的解析规则与「服务配置」下发校验**必须同进同出**：两边判定的
// "这串值能不能用"一旦不一致，就会出现"下发能过、回读解析不过"（或反过来）的静默漂移
// —— 登记值从此对着一个 ZLM 根本不认的段算容量。
func TestParseRTPPortRange_AcceptsOnlyWellFormedRanges(t *testing.T) {
	valid := map[string][2]int{
		"30000-35000": {30000, 35000},
		"51014-51063": {51014, 51063},
		"51200-51299": {51200, 51299},
		"1-1":         {1, 1}, // start == end：容量 0，但不是脏数据
		"1-65535":     {1, 65535},
	}
	for value, want := range valid {
		t.Run("valid/"+value, func(t *testing.T) {
			start, end, ok := node.ParseRTPPortRange(value)
			require.True(t, ok, value)
			require.Equal(t, want[0], start)
			require.Equal(t, want[1], end)
		})
	}

	// 后半段与 service 包的 TestConfigService_InvalidNetworkPortBatchNeverSets 共用同一张表；
	// 前半段是读值路径额外会遇到的形态（ZLM 配置被手改/带尾巴空格等）。
	invalid := []string{
		"", "0-10", "500-499", "1-65536", "1,2", "1 - 2", "abc-def",
		"-500", "500-", "1-2-3", "51014-51063 ", " 51014-51063", "1e3-2e3", "51014--51063",
	}
	for _, value := range invalid {
		t.Run("invalid/"+value, func(t *testing.T) {
			start, end, ok := node.ParseRTPPortRange(value)
			require.False(t, ok, value)
			require.Zero(t, start)
			require.Zero(t, end)
		})
	}
}

func TestParseRTPPortRangeString_RendersBack(t *testing.T) {
	require.Equal(t, "51014-51063", node.RTPPortRangeString(51014, 51063))
	require.Equal(t, "", node.RTPPortRangeString(0, 51063))
	require.Equal(t, "", node.RTPPortRangeString(51014, 0))
}

// 登记值是真源的镜像：真源变了要落库 + 同步内存；
// 没变则**不许写库**（白抬一次 revision 会把并发的统计/心跳写挤掉一次）。
func TestRegistryReconcileRTPPortRange(t *testing.T) {
	ctx := context.Background()
	reg := node.NewRegistry(newMemoryRepo())
	seeded, err := reg.Add(ctx, node.Node{
		Name: "zlm-default", Host: "127.0.0.1", APIPort: 18080,
		MediaServerUUID: "u1", RTPPortStart: 30000, RTPPortEnd: 35000,
	})
	require.NoError(t, err)

	// ① 与端点一致 → 不写
	changed, err := reg.ReconcileRTPPortRange(ctx, seeded.ID, 30000, 35000)
	require.NoError(t, err)
	require.False(t, changed)
	got, ok := reg.Get(seeded.ID)
	require.True(t, ok)
	require.Equal(t, seeded.Revision, got.Revision, "没变化就不该抬 revision")

	// ② 与端点不一致 → 落库 + 内存同步 + revision 前进
	changed, err = reg.ReconcileRTPPortRange(ctx, seeded.ID, 51014, 51063)
	require.NoError(t, err)
	require.True(t, changed)
	got, ok = reg.Get(seeded.ID)
	require.True(t, ok)
	require.Equal(t, 51014, got.RTPPortStart)
	require.Equal(t, 51063, got.RTPPortEnd)
	require.Equal(t, seeded.Revision+1, got.Revision)

	// ③ 容量随之按真源算（登记 5000 个端口 → 49 个）
	withLoad := *got
	withLoad.Stats.MediaSourceCount = 40
	require.InDelta(t, float64(40)/49.0, withLoad.PortUsage(), 0.0001)

	// ④ 非法范围直接拒，已登记值原封不动
	for _, bad := range [][2]int{{0, 0}, {51063, 51014}, {1, 65536}, {0, 51063}} {
		_, err := reg.ReconcileRTPPortRange(ctx, seeded.ID, bad[0], bad[1])
		require.ErrorIs(t, err, node.ErrInvalidRTPPortRange)
	}
	got, ok = reg.Get(seeded.ID)
	require.True(t, ok)
	require.Equal(t, 51014, got.RTPPortStart)
	require.Equal(t, 51063, got.RTPPortEnd)

	// ⑤ 节点不存在
	_, err = reg.ReconcileRTPPortRange(ctx, 9999, 1, 2)
	require.ErrorIs(t, err, node.ErrNotFound)
}
