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
