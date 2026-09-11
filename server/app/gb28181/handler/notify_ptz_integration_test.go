package handler

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type notifyPTZSender struct{}

func (notifyPTZSender) SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
	return uac.TrackedMessageResult{}, nil
}

func TestNotifyHandler_PTZPositionPersistsChannelStateThroughSubscriptionDialog(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbDeviceSubscription{}, &gbmodels.GbChannel{},
		&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZState{},
	))
	device := gbmodels.GbDevice{DeviceID: "34020000001320000001"}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "34020000001320000010"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{
		DeviceID: device.ID, Kind: gbmodels.SubscriptionKindPTZPrecisePosition,
		Enabled: true, Status: gbmodels.SubscriptionStatusActive, Event: "PTZPosition", CallID: "ptz-dialog",
	}).Error)

	now := time.Date(2026, 8, 10, 19, 31, 28, 0, time.Local)
	ptzService, err := ptz.NewService(db, notifyPTZSender{}, func() time.Time { return now })
	require.NoError(t, err)
	subscriptionService := subscribe.NewService(db, nil, func() time.Time { return now })
	subscriptionService.SetProcessor(gbmodels.SubscriptionKindPTZPrecisePosition, subscribe.NewPTZProcessor(ptzService))

	req := sip.NewRequest(sip.NOTIFY, sip.Uri{User: device.DeviceID, Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte(`<Notify><CmdType>PTZPosition</CmdType><SN>3</SN><DeviceID>34020000001320000010</DeviceID><Pan>12.5</Pan></Notify>`))
	req.AppendHeader(sip.NewHeader("Event", "PTZPosition"))
	callID := sip.CallIDHeader("ptz-dialog")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 2, MethodName: sip.NOTIFY})
	tx := siptest.NewServerTxRecorder(req)

	NewNotifyHandler(subscriptionService).Handle(req, tx)

	var state gbmodels.GbPTZState
	require.NoError(t, db.Where("channel_id = ?", channel.ID).First(&state).Error)
	require.Equal(t, device.ID, state.DeviceID)
	require.Equal(t, device.DeviceID, state.DeviceCode)
	require.Equal(t, channel.ID, state.ChannelID)
	require.Equal(t, channel.ChannelID, state.ChannelCode)
	require.NotNil(t, state.Pan)
	require.Equal(t, 12.5, *state.Pan)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}
