package handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 能力刷新必须"只补没读到的通道"。
//
// 现场事故：67 通道的设备同步一次目录排了 136 条 ConfigDownload（两轮 × 68），
// 其中 96 条被 5 秒排队窗口判死。去重是这次修复的第一道闸：
// 已经成功读过 VideoParamOpt 的通道不再重复排。
func TestPendingCapabilityChannelsSkipsFreshChannels(t *testing.T) {
	channels := []gbmodels.GbChannel{
		{ID: 1, ChannelID: "34020000001320000001"},
		{ID: 2, ChannelID: "34020000001320000002"},
		{ID: 3, ChannelID: "34020000001320000003"},
	}
	fresh := map[string]struct{}{"34020000001320000002": {}}

	pending := pendingCapabilityChannels(channels, fresh)
	require.Len(t, pending, 2)
	require.Equal(t, "34020000001320000001", pending[0].ChannelID)
	require.Equal(t, "34020000001320000003", pending[1].ChannelID)

	// 全部刚读过 ⇒ 一条都不用发（第二波触发就该是空操作）。
	all := map[string]struct{}{
		"34020000001320000001": {}, "34020000001320000002": {}, "34020000001320000003": {},
	}
	require.Empty(t, pendingCapabilityChannels(channels, all))

	// 去重集合为空（查询失败）时必须退回"全读"，不能因为查不到就一条不发。
	require.Len(t, pendingCapabilityChannels(channels, nil), 3)
}

// 同一台设备同时只跑一轮刷新；轮内再来的触发登记为"待补"，
// ⛔ 不能直接丢弃——分批 Catalog 的后几批通道是刚入目录的，丢了就永远读不到能力。
func TestCapabilityRefresherSingleFlightPerDevice(t *testing.T) {
	r := &deviceCapabilityRefresher{
		running: make(map[string]struct{}),
		pending: make(map[string]struct{}),
	}
	require.True(t, r.begin("34020000002000000001"), "首次触发应抢占到执行权")
	require.False(t, r.begin("34020000002000000001"), "同一设备并发触发不得并行再跑一轮")
	require.True(t, r.consumePending("34020000002000000001"), "并发触发必须留下待补标记")
	require.False(t, r.consumePending("34020000002000000001"), "待补标记只消费一次")

	// 不同设备互不影响。
	require.True(t, r.begin("34020000002000000002"))

	r.finish("34020000002000000001")
	require.True(t, r.begin("34020000002000000001"), "收尾后应能重新被触发")
}
