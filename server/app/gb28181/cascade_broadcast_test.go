package gb28181

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func prepareCascadeBroadcastFixture(t *testing.T) (*gorm.DB, model.GbCascadePlatform, *repository.GormRepository) {
	t.Helper()
	db, platform, store := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.AutoMigrate(&model.GbCascadeMediaSession{}))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbTalkSession{}))
	for _, statement := range []string{
		"ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT DEFAULT 1",
		"ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT DEFAULT 1",
		"ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL",
		"UPDATE gb_device SET device_id='34020000001320000001', effective_version='2022' WHERE id=10",
		"UPDATE gb_channel SET device_id='34020000001320000001', channel_id='34020000001320000002' WHERE id=20",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	return db, platform, store
}

func TestCascadeBroadcastMessageForwardsToAuthorizedDeviceAndPersistsBinding(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, platform, store := prepareCascadeBroadcastFixture(t)
	downstream := make(chan []byte, 1)
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
		require.Equal(t, "34020000001320000001", device)
		require.Equal(t, "192.0.2.30:5060", address)
		require.Equal(t, "UDP", transport)
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), sender)
	body, err := manscdp.BuildBroadcastNotifyWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.BroadcastNotify{SN: 97, SourceID: platform.UpstreamServerID, TargetID: "34020000001320000012"})
	require.NoError(t, err)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	callID := sip.CallIDHeader("upstream-broadcast-call")
	req.ReplaceHeader(&callID)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, runtime.Handle(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	select {
	case forwarded := <-downstream:
		var notify manscdp.BroadcastNotify
		notify, err = manscdp.ParseCascadeBroadcastNotify(protocol.ProfileFor(protocol.Version2022), forwarded)
		require.NoError(t, err)
		require.Equal(t, "34020000002000000002", notify.SourceID)
		require.Equal(t, "34020000001320000002", notify.TargetID)
		require.NotEqual(t, 97, notify.SN)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for downstream Broadcast")
	}
	var row model.GbCascadeMediaSession
	require.NoError(t, db.Where("call_id = ?", "upstream-broadcast-call").Take(&row).Error)
	require.Equal(t, model.CascadeMediaSessionStateProvisioning, row.State)
	require.EqualValues(t, 10, row.SourceDeviceID)
	require.EqualValues(t, 20, row.SourceChannelID)
}

func TestCascadeBroadcastDeviceResponseRoutesToOriginalUpstream(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, platform, store := prepareCascadeBroadcastFixture(t)
	var downstreamSN int
	sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		notify, err := manscdp.ParseCascadeBroadcastNotify(protocol.ProfileFor(protocol.Version2022), body)
		require.NoError(t, err)
		downstreamSN = notify.SN
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), sender)
	body, err := manscdp.BuildBroadcastNotifyWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.BroadcastNotify{SN: 97, SourceID: platform.UpstreamServerID, TargetID: "34020000001320000012"})
	require.NoError(t, err)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	callID := sip.CallIDHeader("upstream-broadcast-call")
	req.ReplaceHeader(&callID)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, runtime.Handle(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	require.Positive(t, downstreamSN)
	require.Empty(t, transport.requests)
	responseBody, err := manscdp.BuildBroadcastResponseWithProfile(protocol.ProfileFor(protocol.Version2022), downstreamSN, "34020000001320000001", "OK")
	require.NoError(t, err)
	handled, err := runtime.RouteBroadcastMessage(context.Background(), "34020000001320000001", responseBody)
	require.NoError(t, err)
	require.True(t, handled)
	select {
	case upstream := <-transport.requests:
		response, err := manscdp.ParseBroadcastResponse(upstream.Body())
		require.NoError(t, err)
		require.Equal(t, 97, response.SN)
		require.Equal(t, "34020000001320000012", response.DeviceID)
		require.Equal(t, "OK", response.Result)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream Broadcast response")
	}
	var row model.GbCascadeMediaSession
	require.NoError(t, db.Where("call_id = ?", "upstream-broadcast-call").Take(&row).Error)
	// A successful Broadcast business response has not established any media.
	require.Equal(t, model.CascadeMediaSessionStateAnswered, row.State)
	require.Nil(t, row.ActiveAt)
	require.Nil(t, row.ClosedAt)
}

func TestCascadeBroadcastEntryLeavesDeviceResponsesForDeviceRouter(t *testing.T) {
	body, err := manscdp.BuildBroadcastResponseWithProfile(protocol.ProfileFor(protocol.Version2022), 7, "34020000001320000001", "OK")
	require.NoError(t, err)
	req := newCascadeStatusRequest(model.GbCascadePlatform{}, "34020000002000000002", "34020000002000000002", body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	var runtime *cascadeBroadcastRuntime
	require.False(t, runtime.Handle(req, tx))
	require.Empty(t, tx.Result())
}

func TestCascadeBroadcastRejectsUnpublishedTarget(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, platform, store := prepareCascadeBroadcastFixture(t)
	sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		t.Fatal("an unpublished Broadcast target must never be sent downstream")
		return uac.TrackedMessageResult{}, nil
	})
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), sender)
	body, err := manscdp.BuildBroadcastNotifyWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.BroadcastNotify{SN: 97, SourceID: platform.UpstreamServerID, TargetID: "34020000001320000002"})
	require.NoError(t, err)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, runtime.Handle(req, tx))
	require.Equal(t, 403, tx.Result()[0].StatusCode)
}

func TestCascadeBroadcastRejectsResponseAfterSourceBindingChanges(t *testing.T) {
	for name, statement := range map[string]string{
		"epoch":           "UPDATE gb_device SET access_epoch=2,cleanup_completed_epoch=2 WHERE id=10",
		"both owners":     "UPDATE gb_device SET owner_dept_id=1 WHERE id=10; UPDATE gb_channel SET owner_dept_id=1 WHERE id=20",
		"source address":  "UPDATE gb_device SET ip='192.0.2.99' WHERE id=10",
		"source protocol": "UPDATE gb_device SET effective_version='2016' WHERE id=10",
	} {
		t.Run(name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, platform, store := prepareCascadeBroadcastFixture(t)
			var downstreamSN int
			sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
				notify, err := manscdp.ParseCascadeBroadcastNotify(protocol.ProfileFor(protocol.Version2022), body)
				require.NoError(t, err)
				downstreamSN = notify.SN
				return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
			})
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			runtime := newCascadeBroadcastRuntime(db, store, newCascadePlatformClientFactory(transport, nil, time.Second), sender)
			body, err := manscdp.BuildBroadcastNotifyWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.BroadcastNotify{SN: 97, SourceID: platform.UpstreamServerID, TargetID: "34020000001320000012"})
			require.NoError(t, err)
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, runtime.Handle(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			for _, sql := range strings.Split(statement, ";") {
				require.NoError(t, db.Exec(sql).Error)
			}
			responseBody := []byte(fmt.Sprintf(`<Response><CmdType>Broadcast</CmdType><SN>%d</SN><DeviceID>34020000001320000001</DeviceID><TargetID>34020000001320000002</TargetID><Result>OK</Result></Response>`, downstreamSN))
			handled, err := runtime.RouteBroadcastMessage(t.Context(), "34020000001320000001", responseBody)
			require.True(t, handled)
			require.Error(t, err)
			require.Empty(t, transport.requests)
		})
	}
}
