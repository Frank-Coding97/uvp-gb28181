package devicequery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

func TestAlarmSearchFiltersCountAndDetailStayInsideDeviceScope(t *testing.T) {
	db, scope, deviceID, _, childID := newDeviceQueryDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	priority, method, typ := 1, 2, 3
	rows := []gbmodels.GbAlarmEvent{
		{DeviceID: deviceID, SourceCode: queryRootChannel, AlarmTime: &now, ReceivedAt: now, Priority: &priority, Method: &method, AlarmType: &typ, Description: "入口烟雾 100%", DedupeKey: "filtered-one"},
		{DeviceID: deviceID, SourceCode: queryRootDevice, AlarmTime: &now, ReceivedAt: now, Description: "unrelated", DedupeKey: "filtered-two"},
		{DeviceID: childID, SourceCode: queryRootChannel, AlarmTime: &now, ReceivedAt: now, Description: "secret", DedupeKey: "filtered-three"},
	}
	require.NoError(t, db.Create(&rows).Error)
	from, to := now.Add(-time.Minute), now.Add(time.Minute)
	service := New(db)
	page, err := service.SearchDeviceAlarmsInScope(context.Background(), scope, queryRootDevice, AlarmFilter{
		DeviceListOptions: resource.DeviceListOptions{Page: 1, PageSize: 20, Keyword: "100%"}, SourceCode: queryRootChannel,
		AlarmFrom: &from, AlarmTo: &to, Priority: &priority, Method: &method, AlarmType: &typ,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	detail, err := service.GetDeviceAlarmInScope(context.Background(), scope, queryRootDevice, page.Items[0].ID)
	require.NoError(t, err)
	require.Equal(t, "入口烟雾 100%", detail.Description)
	_, err = service.GetDeviceAlarmInScope(context.Background(), scope, queryChildDevice, page.Items[0].ID)
	require.ErrorIs(t, err, resource.ErrResourceNotFound)
	_, err = service.GetDeviceAlarmInScope(context.Background(), scope, queryRootDevice, "0")
	require.ErrorIs(t, err, resource.ErrInvalidListOptions)
	_, err = service.SearchDeviceAlarmsInScope(context.Background(), scope, queryRootDevice, AlarmFilter{AlarmFrom: &to, AlarmTo: &from})
	require.ErrorIs(t, err, resource.ErrInvalidListOptions)
}
