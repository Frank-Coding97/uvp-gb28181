package gb28181

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/gb28181/upgrade"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func cascadeUpgradeRequest(t *testing.T, platform model.GbCascadePlatform, session string) *sip.Request {
	t.Helper()
	body, err := manscdp.BuildDeviceUpgradeControlWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000012", 97, manscdp.DeviceUpgradeCommand{Firmware: "v2.3.4", FileURL: "https://fixture.example/firmware.bin", Manufacturer: "UVP", SessionID: session})
	require.NoError(t, err)
	return newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
}

func TestCascadeUpgradeMessagePersistsAndReturnsOnlyOriginalIdentity(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	downstream := make(chan []byte, 2)
	sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	var barrier *playauth.DeviceOperationBarrier
	db, platform, store, ptz := cascadeAuthorizedControlFixture(t, "2022", sender, &barrier)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
	require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022'").Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=1,ptz_enabled=0,config_enabled=0,maintenance_enabled=0").Error)
	service, err := upgrade.NewAuthorizedService(db, sender, ptz, time.Now, barrier)
	require.NoError(t, err)
	originalSession := strings.Repeat("a", 32)
	request := cascadeUpgradeRequest(t, platform, originalSession)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 4)}
	clients := newCascadePlatformClientFactory(upstream, nil, time.Second)
	hook, wait := newCascadeUpgradeHandlerWithService(store, clients, service, context.Background())
	handler := newCascadeMessageHandler(store, clients, hook)
	tx := siptest.NewServerTxRecorder(request)
	defer tx.Terminate()
	require.True(t, handler(request, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	wait()
	select {
	case body := <-downstream:
		parsed, err := manscdp.ParseDeviceUpgradeControlWithProfile(protocol.ProfileFor(protocol.Version2022), body)
		require.NoError(t, err)
		require.Equal(t, "34020000001320000001", parsed.DeviceID)
		require.NotEqual(t, 97, parsed.SN)
		require.NotEqual(t, originalSession, parsed.Command.SessionID)
	case <-time.After(time.Second):
		t.Fatal("missing downstream upgrade")
	}
	replayTx := siptest.NewServerTxRecorder(request)
	defer replayTx.Terminate()
	require.True(t, handler(request, replayTx))
	wait()
	require.Empty(t, downstream, "retransmission must not send firmware command again")
	var row gbmodels.GbDeviceFirmwareUpgrade
	require.NoError(t, db.Take(&row).Error)
	consumed, err := service.OnUpgradeMessage(t.Context(), row.DeviceCode, "device-business", "2", []byte(fmt.Sprintf(`<Response><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>OK</Result></Response>`, row.SN, row.DeviceCode)))
	require.NoError(t, err)
	require.True(t, consumed)
	require.NoError(t, deliverCascadeUpgradeResults(t.Context(), store, clients, service))
	business := awaitCascadeStatusResponse(t, upstream.requests)
	parsed, err := manscdp.ParseDeviceControlResponseWithProfile(protocol.ProfileFor(protocol.Version2022), business.Body())
	require.NoError(t, err)
	require.Equal(t, 97, parsed.SN)
	require.Equal(t, "34020000001320000012", parsed.DeviceID)
	finalBody, err := manscdp.BuildDeviceUpgradeResultWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.DeviceUpgradeResultNotify{SN: 29, DeviceID: row.DeviceCode, SessionID: row.SessionID, Firmware: "v2.3.4", UpgradeResult: "OK"})
	require.NoError(t, err)
	consumed, status, err := service.OnUpgradeResultMessage(t.Context(), row.DeviceCode, "final", "3", finalBody)
	require.NoError(t, err)
	require.True(t, consumed)
	require.Equal(t, 200, status)
	restarted, err := upgrade.NewAuthorizedService(db, sender, ptz, time.Now, barrier)
	require.NoError(t, err)
	require.NoError(t, deliverCascadeUpgradeResults(t.Context(), store, clients, restarted))
	final := awaitCascadeStatusResponse(t, upstream.requests)
	result, err := manscdp.ParseDeviceUpgradeResultWithProfile(protocol.ProfileFor(protocol.Version2022), final.Body())
	require.NoError(t, err)
	require.Equal(t, originalSession, result.SessionID)
	require.Equal(t, 97, result.SN)
	require.Equal(t, "34020000001320000012", result.DeviceID)
	require.NotContains(t, string(final.Body()), row.DeviceCode)
	require.NoError(t, deliverCascadeUpgradeResults(t.Context(), store, clients, restarted))
	require.Empty(t, upstream.requests)
	require.NoError(t, db.Exec("UPDATE gb_device_firmware_upgrade SET cascade_final_delivered=0").Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=0").Error)
	require.NoError(t, deliverCascadeUpgradeResults(t.Context(), store, clients, restarted))
	require.Empty(t, upstream.requests, "revoked platform must not receive late results")
}

func TestCascadeUpgradeRejectsMissingGrantHiddenTargetAndMalformedRequest(t *testing.T) {
	for _, scenario := range []string{"grant", "hidden", "sip-target", "old-version", "mixed", "device-version"} {
		t.Run(scenario, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				t.Error("rejected request must not upgrade a device")
				return uac.TrackedMessageResult{}, nil
			})
			db, platform, store, ptz := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
			require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=1").Error)
			require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022'").Error)
			request := cascadeUpgradeRequest(t, platform, strings.Repeat("a", 32))
			expected := 403
			switch scenario {
			case "grant":
				require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=0").Error)
			case "hidden":
				require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET active=0").Error)
			case "sip-target":
				request.Recipient.User = "34020000001320000099"
			case "old-version":
				require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET effective_version='2016',profile_override='2016'").Error)
				expected = 400
			case "mixed":
				request.SetBody([]byte(strings.Replace(string(request.Body()), "</Control>", "<TeleBoot>Boot</TeleBoot></Control>", 1)))
				expected = 400
			case "device-version":
				require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2016'").Error)
				expected = 400
			}
			service, err := upgrade.NewService(db, sender, ptz, time.Now)
			require.NoError(t, err)
			handler, wait := newCascadeUpgradeHandlerWithService(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second), service, t.Context())
			tx := siptest.NewServerTxRecorder(request)
			defer tx.Terminate()
			require.True(t, handler(request, tx))
			wait()
			require.Equal(t, expected, tx.Result()[0].StatusCode)
		})
	}
}

func TestCascadeUpgradeFailedDeliveryRecoversAndRuntimeStops(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	var barrier *playauth.DeviceOperationBarrier
	db, platform, store, ptz := cascadeAuthorizedControlFixture(t, "2022", sender, &barrier)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
	require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022'").Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=1").Error)
	service, err := upgrade.NewAuthorizedService(db, sender, ptz, time.Now, barrier)
	require.NoError(t, err)
	transport := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500}
	clients := newCascadePlatformClientFactory(transport, nil, time.Second)
	handler, wait := newCascadeUpgradeHandlerWithService(store, clients, service, t.Context())
	request := cascadeUpgradeRequest(t, platform, strings.Repeat("a", 32))
	tx := siptest.NewServerTxRecorder(request)
	defer tx.Terminate()
	require.True(t, handler(request, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	wait()
	var row gbmodels.GbDeviceFirmwareUpgrade
	require.NoError(t, db.Take(&row).Error)
	consumed, err := service.OnUpgradeMessage(t.Context(), row.DeviceCode, "business", "1", []byte(fmt.Sprintf(`<Response><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>ERROR</Result></Response>`, row.SN, row.DeviceCode)))
	require.NoError(t, err)
	require.True(t, consumed)
	require.Error(t, deliverCascadeUpgradeResults(t.Context(), store, clients, service))
	<-transport.requests
	require.NoError(t, db.First(&row, row.ID).Error)
	require.False(t, row.CascadeBusinessDelivered)
	transport.status = 200
	runtime := newCascadeUpgradeRuntime(store, clients, service)
	defer runtime.Shutdown(context.Background())
	message := awaitCascadeStatusResponse(t, transport.requests)
	require.Equal(t, platform.UpstreamServerID, message.To().Address.User)
	require.Eventually(t, func() bool {
		var current gbmodels.GbDeviceFirmwareUpgrade
		return db.First(&current, row.ID).Error == nil && current.CascadeBusinessDelivered
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, runtime.Shutdown(t.Context()))
	require.NoError(t, db.Model(&row).Update("cascade_business_delivered", false).Error)
	require.Empty(t, transport.requests)
	rejectedTx := siptest.NewServerTxRecorder(request)
	defer rejectedTx.Terminate()
	require.True(t, runtime.Handle(request, rejectedTx))
	require.Equal(t, 503, rejectedTx.Result()[0].StatusCode)
}

func TestCascadeUpgradeAcknowledgesOnlyDurableRequestBeforeDeviceSend(t *testing.T) {
	for _, persistFails := range []bool{false, true} {
		t.Run(fmt.Sprint(persistFails), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			var tx *siptest.ServerTxRecorder
			sent := 0
			sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				sent++
				responses := tx.Result()
				require.Len(t, responses, 1, "acceptance must precede downstream execution")
				require.Equal(t, 200, responses[0].StatusCode)
				return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
			})
			var barrier *playauth.DeviceOperationBarrier
			db, platform, store, ptz := cascadeAuthorizedControlFixture(t, "2022", sender, &barrier)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
			require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022'").Error)
			require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=1").Error)
			if persistFails {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_upgrade_persistence", func(q *gorm.DB) {
					if q.Statement.Table == "gb_device_firmware_upgrade" {
						q.AddError(errors.New("fixture write failure"))
					}
				}))
			} else {
				require.NoError(t, db.Callback().Create().After("gorm:create").Register("assert_upgrade_not_yet_accepted", func(q *gorm.DB) {
					if q.Statement.Table == "gb_device_firmware_upgrade" {
						require.Empty(t, tx.Result(), "uncommitted task must not receive SIP 200")
					}
				}))
			}
			service, err := upgrade.NewAuthorizedService(db, sender, ptz, time.Now, barrier)
			require.NoError(t, err)
			handler, wait := newCascadeUpgradeHandlerWithService(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 2)}, nil, time.Second), service, t.Context())
			request := cascadeUpgradeRequest(t, platform, strings.Repeat("c", 32))
			tx = siptest.NewServerTxRecorder(request)
			defer tx.Terminate()
			require.True(t, handler(request, tx))
			wait()
			require.Len(t, tx.Result(), 1)
			if persistFails {
				require.Equal(t, 503, tx.Result()[0].StatusCode)
				require.Zero(t, sent)
			} else {
				require.Equal(t, 1, sent)
			}
		})
	}
}

func TestCascadeUpgradeFinalBeforeBusinessDeliversBothMilestones(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
	})
	var barrier *playauth.DeviceOperationBarrier
	db, platform, store, ptz := cascadeAuthorizedControlFixture(t, "2022", sender, &barrier)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
	require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022'").Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=1").Error)
	service, err := upgrade.NewAuthorizedService(db, sender, ptz, time.Now, barrier)
	require.NoError(t, err)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 4)}
	clients := newCascadePlatformClientFactory(upstream, nil, time.Second)
	handler, wait := newCascadeUpgradeHandlerWithService(store, clients, service, t.Context())
	request := cascadeUpgradeRequest(t, platform, strings.Repeat("d", 32))
	tx := siptest.NewServerTxRecorder(request)
	defer tx.Terminate()
	require.True(t, handler(request, tx))
	wait()
	var row gbmodels.GbDeviceFirmwareUpgrade
	require.NoError(t, db.Take(&row).Error)
	finalBody, err := manscdp.BuildDeviceUpgradeResultWithProfile(protocol.ProfileFor(protocol.Version2022), manscdp.DeviceUpgradeResultNotify{SN: 29, DeviceID: row.DeviceCode, SessionID: row.SessionID, Firmware: "v2.3.4", UpgradeResult: "OK"})
	require.NoError(t, err)
	consumed, _, err := service.OnUpgradeResultMessage(t.Context(), row.DeviceCode, "final-first", "2", finalBody)
	require.NoError(t, err)
	require.True(t, consumed)
	require.NoError(t, deliverCascadeUpgradeResults(t.Context(), store, clients, service))
	final, err := manscdp.ParseDeviceUpgradeResultWithProfile(protocol.ProfileFor(protocol.Version2022), awaitCascadeStatusResponse(t, upstream.requests).Body())
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("d", 32), final.SessionID)
	require.Equal(t, "OK", final.UpgradeResult)
	consumed, err = service.OnUpgradeMessage(t.Context(), row.DeviceCode, "business-late", "3", []byte(fmt.Sprintf(`<Response><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>ERROR</Result></Response>`, row.SN, row.DeviceCode)))
	require.NoError(t, err)
	require.True(t, consumed)
	require.NoError(t, deliverCascadeUpgradeResults(t.Context(), store, clients, service))
	business, err := manscdp.ParseDeviceControlResponseWithProfile(protocol.ProfileFor(protocol.Version2022), awaitCascadeStatusResponse(t, upstream.requests).Body())
	require.NoError(t, err)
	require.Equal(t, manscdp.DeviceControlResultError, business.Result)
	require.Equal(t, 97, business.SN)
	require.Equal(t, "34020000001320000012", business.DeviceID)
	require.NoError(t, db.Take(&row).Error)
	require.Equal(t, gbmodels.FirmwareUpgradeSucceeded, row.Status)
	require.Equal(t, "OK", row.DeviceResult)
	require.NoError(t, deliverCascadeUpgradeResults(t.Context(), store, clients, service))
	require.Empty(t, upstream.requests)
}
