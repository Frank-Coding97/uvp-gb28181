package play

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// 契约 docs/logging-governance/contracts/play.md §2.2e:
// 空值必须省略,不能打成 device_id="" —— 否则统计脚本/门禁会把它当成"已带定位字段"。
func TestStopLogFieldsOmitsEmptyIdentity(t *testing.T) {
	keys := func(fields []zap.Field) []string {
		out := make([]string, 0, len(fields))
		for _, f := range fields {
			out = append(out, f.Key)
		}
		return out
	}

	require.Equal(t, []string{"stream_id"}, keys(stopLogFields("ssrc-1", "", "")))
	require.Equal(t, []string{"device_id", "stream_id"}, keys(stopLogFields("ssrc-1", "dev-1", "")))
	require.Equal(t, []string{"channel_id", "stream_id"}, keys(stopLogFields("ssrc-1", "", "ch-1")))
	require.Equal(t, []string{"device_id", "channel_id", "stream_id"}, keys(stopLogFields("ssrc-1", "dev-1", "ch-1")))
}

// §2.2c: 调用方已经给了值就不再回退 —— 即便内存会话里挂着另一组值。
func TestStopLogIdentityPrefersCallerValues(t *testing.T) {
	s, _, _ := newSvc(t, &mockZLM{}, &mockInviter{}, onlineDevice(), aChannel())
	require.True(t, s.sessions.PutIfCurrent(&uac.Session{
		StreamID: "ssrc-9", DeviceID: "mem-dev", ChannelID: "mem-ch",
	}))

	deviceID, channelID := s.stopLogIdentity("ssrc-9", "dev-in", "ch-in")
	require.Equal(t, "dev-in", deviceID)
	require.Equal(t, "ch-in", channelID)
}

// §2.2c: 首选兜底是内存会话(uac.Session 在发 INVITE 时写入,纯 map 查找零 I/O)。
func TestStopLogIdentityFallsBackToMemorySession(t *testing.T) {
	s, _, _ := newSvc(t, &mockZLM{}, &mockInviter{}, onlineDevice(), aChannel())
	require.True(t, s.sessions.PutIfCurrent(&uac.Session{
		StreamID:  "ssrc-7",
		DeviceID:  "34020000001320000002",
		ChannelID: "34020000001320000003",
	}))

	deviceID, channelID := s.stopLogIdentity("ssrc-7", "", "")
	require.Equal(t, "34020000001320000002", deviceID)
	require.Equal(t, "34020000001320000003", channelID)
}

// §2.2c: 会话已丢(重启后对账清理假阳性)时,固定流 ID 仍能解出设备/通道。
func TestStopLogIdentityFallsBackToFixedStreamID(t *testing.T) {
	s, _, _ := newSvc(t, &mockZLM{}, &mockInviter{}, onlineDevice(), aChannel())

	deviceID, channelID := s.stopLogIdentity("34020000001320000002_34020000001320000003", "", "")
	require.Equal(t, "34020000001320000002", deviceID)
	require.Equal(t, "34020000001320000003", channelID)
}

// §2.2.4 纪律①:两个来源都落空时静默降级为"字段省略",不报错、不 panic、不改控制流。
func TestStopLogIdentityDegradesSilently(t *testing.T) {
	s, _, _ := newSvc(t, &mockZLM{}, &mockInviter{}, onlineDevice(), aChannel())

	deviceID, channelID := s.stopLogIdentity("fake-stream", "", "")
	require.Empty(t, deviceID)
	require.Empty(t, channelID)
}

// §2.2f 端到端:停播三事件带 device_id/channel_id;拿不到时字段直接缺席(而非空串)。
func TestStopEventsCarryDeviceAndChannel(t *testing.T) {
	core, observed := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	s, _, _ := newSvc(t, &mockZLM{}, &mockInviter{}, onlineDevice(), aChannel())
	require.NoError(t, s.Stop(context.Background(), "fake-stream", "dev-x", "ch-y"))

	requested := observed.FilterField(zap.String("event", "gb28181.play.stop_requested")).All()
	require.Len(t, requested, 1)
	require.Equal(t, "dev-x", requested[0].ContextMap()["device_id"])
	require.Equal(t, "ch-y", requested[0].ContextMap()["channel_id"])

	completed := observed.FilterField(zap.String("event", "gb28181.play.stop_completed")).All()
	require.Len(t, completed, 1)
	require.Equal(t, "dev-x", completed[0].ContextMap()["device_id"])
	require.Equal(t, "ch-y", completed[0].ContextMap()["channel_id"])
}

// §2.2e 端到端:ZLM webhook 那条路径传空且内存里也没有会话 → 字段必须**缺席**。
func TestStopEventsOmitIdentityWhenUnresolvable(t *testing.T) {
	core, observed := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	s, _, _ := newSvc(t, &mockZLM{}, &mockInviter{}, onlineDevice(), aChannel())
	require.NoError(t, s.Stop(context.Background(), "fake-stream", "", ""))

	requested := observed.FilterField(zap.String("event", "gb28181.play.stop_requested")).All()
	require.Len(t, requested, 1)
	fields := requested[0].ContextMap()
	require.NotContains(t, fields, "device_id")
	require.NotContains(t, fields, "channel_id")
	require.Equal(t, "fake-stream", fields["stream_id"])
}
