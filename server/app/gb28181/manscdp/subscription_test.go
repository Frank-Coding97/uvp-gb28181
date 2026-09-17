package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestBuildSubscriptionQuery(t *testing.T) {
	tests := []struct {
		kind      gbmodels.SubscriptionKind
		wantEvent string
		wantBody  string
	}{
		{gbmodels.SubscriptionKindCatalog, "Catalog", "<CmdType>Catalog</CmdType>"},
		{gbmodels.SubscriptionKindMobilePosition, "presence", "<Interval>30</Interval>"},
		{gbmodels.SubscriptionKindAlarm, "presence", "<CmdType>Alarm</CmdType>"},
		{gbmodels.SubscriptionKindPTZPrecisePosition, "PTZPosition", "<CmdType>PTZPosition</CmdType>"},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			body, event, err := BuildSubscriptionQuery(tt.kind, "34020000001320000001", 7, 30)
			require.NoError(t, err)
			require.Equal(t, tt.wantEvent, event)
			require.Contains(t, string(body), `encoding="GB2312"`)
			require.Contains(t, string(body), tt.wantBody)
			require.Contains(t, string(body), "<DeviceID>34020000001320000001</DeviceID>")
		})
	}
}

func TestResolveSubscriptionKind_CompatibleEventVariants(t *testing.T) {
	cases := []struct {
		event string
		body  string
		want  gbmodels.SubscriptionKind
	}{
		{"Catalog", `<Notify><CmdType>Catalog</CmdType><DeviceID>D</DeviceID></Notify>`, gbmodels.SubscriptionKindCatalog},
		{"presence;id=4", `<Notify><CmdType>Catalog</CmdType><DeviceID>D</DeviceID></Notify>`, gbmodels.SubscriptionKindCatalog},
		{"presence", `<Notify><CmdType>MobilePosition</CmdType><DeviceID>D</DeviceID></Notify>`, gbmodels.SubscriptionKindMobilePosition},
		{"presence", `<Notify><CmdType>Alarm</CmdType><DeviceID>D</DeviceID></Notify>`, gbmodels.SubscriptionKindAlarm},
		{"Alarm;id=9", `<Notify><CmdType>Alarm</CmdType><DeviceID>D</DeviceID></Notify>`, gbmodels.SubscriptionKindAlarm},
		{"PTZPosition", `<Notify><CmdType>PTZPosition</CmdType><DeviceID>D</DeviceID></Notify>`, gbmodels.SubscriptionKindPTZPrecisePosition},
	}
	for _, tc := range cases {
		got, err := ResolveSubscriptionKind(tc.event, []byte(tc.body))
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}

func TestParseSubscriptionNotifications(t *testing.T) {
	catalog, err := ParseCatalogNotify([]byte(`<?xml version="1.0" encoding="UTF-8"?><Notify><CmdType>Catalog</CmdType><SN>1</SN><DeviceID>D</DeviceID><DeviceList Num="1"><Item><DeviceID>C</DeviceID><Name>前门</Name><Event>ADD</Event></Item></DeviceList></Notify>`))
	require.NoError(t, err)
	require.Equal(t, "D", catalog.DeviceID)
	require.Equal(t, "ADD", catalog.DeviceList.Items[0].Event)
	require.Equal(t, "前门", catalog.DeviceList.Items[0].Name)

	position, err := ParseMobilePositionNotify([]byte(`<Notify><CmdType>MobilePosition</CmdType><SN>2</SN><DeviceID>C</DeviceID><Time>2026-07-19T16:00:00</Time><Longitude>116.4001</Longitude><Latitude>39.9002</Latitude><Speed>12.5</Speed></Notify>`))
	require.NoError(t, err)
	require.Equal(t, "C", position.DeviceID)
	require.Equal(t, 116.4001, position.Longitude)
	require.Equal(t, 39.9002, position.Latitude)

	alarm, err := ParseAlarmNotify([]byte(`<Notify><CmdType>Alarm</CmdType><SN>3</SN><DeviceID>C</DeviceID><AlarmPriority>2</AlarmPriority><AlarmMethod>5</AlarmMethod><AlarmTime>2026-07-19T16:00:00</AlarmTime><AlarmDescription>video lost</AlarmDescription></Notify>`))
	require.NoError(t, err)
	require.Equal(t, 2, alarm.Priority)
	require.Equal(t, "video lost", alarm.Description)

	_, err = ParseMobilePositionNotify([]byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>C</DeviceID><Longitude>200</Longitude><Latitude>39</Latitude></Notify>`))
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "经纬度"))
}

// TestParseMobilePositionNotify_2022ListForm 锁住 GB/T 28181-2022 A.2.5.6 列表形态。
//
// 改动前这一形态会被**静默解析成 0,0**：顶层 DeviceID 两版都有，所以非空守卫放行；
// Longitude/Latitude 解出 0 也落在合法区间内，范围校验照样通过；
// 最后才在 subscribe 包的「位置坐标不能为 0」被丢掉，无日志无异常。
func TestParseMobilePositionNotify_2022ListForm(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Notify>
<CmdType>MobilePosition</CmdType>
<SN>7</SN>
<DeviceID>34020000001110000001</DeviceID>
<Time>2026-09-17T22:00:00</Time>
<SumNum>2</SumNum>
<DeviceList Num="2">
<Item>
<DeviceID>34020000001320000001</DeviceID>
<CaptureTime>2026-09-17T21:59:00</CaptureTime>
<Longitude>116.4041</Longitude>
<Latitude>39.9152</Latitude>
<Speed>36.0</Speed>
<Direction>90.0</Direction>
<Altitude>12.5</Altitude>
<Height>3.5</Height>
</Item>
<Item>
<DeviceID>34020000001320000002</DeviceID>
<CaptureTime>2026-09-17T21:59:30</CaptureTime>
<Longitude>116.5001</Longitude>
<Latitude>39.8002</Latitude>
</Item>
</DeviceList>
</Notify>`)
	notify, err := ParseMobilePositionNotify(body)
	require.NoError(t, err)
	require.Equal(t, "34020000001110000001", notify.DeviceID)

	positions := notify.Positions()
	require.Len(t, positions, 2)

	first := positions[0]
	require.Equal(t, "34020000001320000001", first.DeviceID)
	// 2022 的采集时间来自 Item/CaptureTime，**不是**根 Time（根 Time 已改为「上报通知时间」）。
	require.Equal(t, "2026-09-17T21:59:00", first.CaptureTime)
	require.InDelta(t, 116.4041, first.Longitude, 1e-9)
	require.InDelta(t, 39.9152, first.Latitude, 1e-9)
	require.InDelta(t, 36.0, first.Speed, 1e-9)
	require.InDelta(t, 90.0, first.Direction, 1e-9)
	require.InDelta(t, 12.5, first.Altitude, 1e-9)
	require.InDelta(t, 3.5, first.Height, 1e-9)

	second := positions[1]
	require.Equal(t, "34020000001320000002", second.DeviceID)
	require.InDelta(t, 116.5001, second.Longitude, 1e-9)
	// 标准里 Speed/Direction/Altitude/Height 是 minOccurs=0 可选字段：
	// 「未上报」与「上报 0」在解析层刻意不可区分，两者都是 0。
	require.Zero(t, second.Speed)
	require.Zero(t, second.Height)
}

// TestMobilePositionNotifyPositions_FlatFormNormalizesToSingleItem 保证 2016 扁平形态
// 归一化后仍是「一条」，且采集时间取根 Time —— 2016 语义没有被动过。
func TestMobilePositionNotifyPositions_FlatFormNormalizesToSingleItem(t *testing.T) {
	notify, err := ParseMobilePositionNotify([]byte(`<Notify><CmdType>MobilePosition</CmdType><SN>2</SN><DeviceID>C</DeviceID><Time>2026-07-19T16:00:00</Time><Longitude>116.4001</Longitude><Latitude>39.9002</Latitude><Speed>12.5</Speed><Direction>7.0</Direction><Altitude>3.0</Altitude></Notify>`))
	require.NoError(t, err)

	positions := notify.Positions()
	require.Len(t, positions, 1)
	require.Equal(t, "C", positions[0].DeviceID)
	require.Equal(t, "2026-07-19T16:00:00", positions[0].CaptureTime)
	require.InDelta(t, 116.4001, positions[0].Longitude, 1e-9)
	require.InDelta(t, 3.0, positions[0].Altitude, 1e-9)
}

// TestParseMobilePositionNotify_Empty2022ListYieldsNoPositions 是本次的关键回归。
//
// 2022 的 SumNum=0（本次无位置上报）是 A.2.5.6 允许的**合法空列表**；
// 一旦判定为列表形态就必须在列表语义里走到底 —— 回落扁平字段会拿根上的 0 值坐标
// 合成一条 (0,0) **假位置**，前端地图上就是个漂到几内亚湾的设备。
func TestParseMobilePositionNotify_Empty2022ListYieldsNoPositions(t *testing.T) {
	cases := map[string]string{
		"SumNum 与 DeviceList 都在但 Item 为空": `<Notify><CmdType>MobilePosition</CmdType><SN>1</SN><DeviceID>D</DeviceID><Time>2026-09-17T22:00:00</Time><SumNum>0</SumNum><DeviceList Num="0"></DeviceList></Notify>`,
		"只有 SumNum 没有 DeviceList":        `<Notify><CmdType>MobilePosition</CmdType><SN>1</SN><DeviceID>D</DeviceID><Time>2026-09-17T22:00:00</Time><SumNum>0</SumNum></Notify>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			notify, err := ParseMobilePositionNotify([]byte(body))
			require.NoError(t, err)
			require.Empty(t, notify.Positions(), "列表形态下空列表必须是空 no-op，不得回落扁平字段")
		})
	}
}

// Item 缺 CaptureTime（标准里是必选，但设备实现常有疏漏）→ 用根 Time 兜底：
// 时间退化好过整条位置被丢。同时锁住「不会因为 CaptureTime 空而 error」。
func TestMobilePositionNotifyPositions_ItemFallsBackToRootTime(t *testing.T) {
	body := []byte(`<Notify><CmdType>MobilePosition</CmdType><SN>1</SN><DeviceID>D</DeviceID><Time>2026-09-17T22:00:00</Time><SumNum>1</SumNum><DeviceList Num="1"><Item><DeviceID>C</DeviceID><Longitude>116.4</Longitude><Latitude>39.9</Latitude></Item></DeviceList></Notify>`)
	notify, err := ParseMobilePositionNotify(body)
	require.NoError(t, err)
	require.Len(t, notify.Positions(), 1)
	require.Equal(t, "2026-09-17T22:00:00", notify.Positions()[0].CaptureTime)
}

// 列表里任一项坐标越界 → 整包非法（与 Catalog/Alarm 一样「先整体校验再落地」，
// 不给下游留半截脏数据）。
func TestParseMobilePositionNotify_2022ListRejectsOutOfRangeItem(t *testing.T) {
	body := []byte(`<Notify><CmdType>MobilePosition</CmdType><SN>1</SN><DeviceID>D</DeviceID><Time>2026-09-17T22:00:00</Time><SumNum>2</SumNum><DeviceList Num="2"><Item><DeviceID>OK</DeviceID><CaptureTime>2026-09-17T21:59:00</CaptureTime><Longitude>116.4</Longitude><Latitude>39.9</Latitude></Item><Item><DeviceID>BAD</DeviceID><CaptureTime>2026-09-17T21:59:01</CaptureTime><Longitude>200</Longitude><Latitude>39.9</Latitude></Item></DeviceList></Notify>`)
	_, err := ParseMobilePositionNotify(body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "经纬度")
}

func TestParseAlarmNotify_ParsesStandardNestedInfo(t *testing.T) {
	alarm, err := ParseAlarmNotify([]byte(`<?xml version="1.0" encoding="GB2312"?>
<Notify>
<CmdType>Alarm</CmdType>
<SN>1260</SN>
<DeviceID>37010301021320000111</DeviceID>
<AlarmPriority>4</AlarmPriority>
<AlarmMethod>5</AlarmMethod>
<AlarmTime>2026-08-04T17:52:23</AlarmTime>
<Info>
<AlarmType>2</AlarmType>
<AlarmTypeParam><EventType>1</EventType></AlarmTypeParam>
</Info>
</Notify>`))
	require.NoError(t, err)
	require.NotNil(t, alarm.AlarmType)
	require.Equal(t, 2, *alarm.AlarmType)
	require.Equal(t, "EventType=1", alarm.AlarmTypeParam)
}
