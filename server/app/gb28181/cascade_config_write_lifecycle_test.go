package gb28181

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeConfigWriteRechecksQueuedPublicationAndCancellation(t *testing.T) {
	for _, mutation := range []string{
		"UPDATE gb_cascade_platform SET config_enabled=0",
		"UPDATE gb_cascade_channel_projection SET active=0",
		"UPDATE gb_cascade_platform SET config_revision=config_revision+1",
		"UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP",
		"cancel request",
	} {
		t.Run(mutation, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			sent := make(chan []byte, 1)
			sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
				sent <- body
				return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
			})
			db, platform, _, service := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.Model(&platform).Update("config_enabled", true).Error)
			target, err := control.NewGormTargetLoader(db).LoadAuthorized(t.Context(), 10, 20)
			require.NoError(t, err)
			block := `<FrameMirror>1</FrameMirror>`
			// Device-scoped writes must also lose authorization when the published channel disappears.
			if mutation == "UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP" {
				block = `<BasicParam><HeartBeatInterval>60</HeartBeatInterval></BasicParam>`
			}
			command, err := manscdp.ParseCascadeConfigWrite([]byte(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID>` + block + `</Control>`))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				_, err := service.ConfigWriteForCascade(ctx, target, command, ptz.CascadeConfigAuthorization{PlatformID: platform.ID, ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision, PublishedID: "34020000001320000012"}, "queued-config-write")
				done <- err
			}()
			t.Cleanup(func() { cancel(); service.Retire(); <-exited })
			var operation gbmodels.GbPTZOperation
			require.Eventually(t, func() bool { return db.Where("idempotency_key=?", "queued-config-write").Take(&operation).Error == nil }, time.Second, 5*time.Millisecond)
			if mutation == "cancel request" {
				cancel()
				require.Error(t, <-done)
			} else {
				require.NoError(t, db.Exec(mutation).Error)
			}
			scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
			scheduler.Start(t.Context())
			t.Cleanup(scheduler.Stop)
			require.Eventually(t, func() bool {
				var current gbmodels.GbPTZOperation
				return db.First(&current, operation.ID).Error == nil && current.Status != gbmodels.PTZOperationQueued && current.Status != gbmodels.PTZOperationSent
			}, time.Second, 5*time.Millisecond)
			if mutation != "cancel request" {
				require.Error(t, <-done)
			}
			require.Empty(t, sent, "revoked or cancelled queued writes must never reach the network")
		})
	}
}

func TestCascadeConfigWriteApplicationResultsAndRevocation(t *testing.T) {
	for _, scenario := range []string{"ERROR", "missing Result", "duplicate Result", "source epoch changed", "source epoch changed before basic", "upstream changed"} {
		t.Run(scenario, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			sent := make(chan []byte, 1)
			sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
				sent <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{StatusCode: 200, Attempted: true, CallID: "downstream-config", CSeq: "1"}, nil
			})
			db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.Model(&platform).Update("config_enabled", true).Error)
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			execute := cascadeConfigWriteFunc(writeCascadeConfig)
			if scenario == "source epoch changed" || scenario == "source epoch changed before basic" || scenario == "upstream changed" {
				execute = func(ctx context.Context, target cascadeConfigWriteTarget, command *manscdp.CascadeConfigWrite, authorization ptz.CascadeConfigAuthorization, key string) (cascadeConfigQueryResult, error) {
					result, err := writeCascadeConfig(ctx, target, command, authorization, key)
					if err == nil {
						mutation := "UPDATE gb_device SET access_epoch=access_epoch+1,cleanup_completed_epoch=cleanup_completed_epoch+1"
						if scenario == "upstream changed" {
							mutation = "UPDATE gb_cascade_platform SET config_revision=config_revision+1"
						}
						err = db.Exec(mutation).Error
					}
					return result, err
				}
			}
			handler, wait := newCascadeConfigWriteHandlerWithExecutor(store, newCascadePlatformClientFactory(upstream, nil, time.Second), execute)
			t.Cleanup(wait)
			scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
			scheduler.Start(t.Context())
			t.Cleanup(scheduler.Stop)
			blocks := `<FrameMirror>1</FrameMirror>`
			if scenario == "source epoch changed before basic" {
				blocks += `<BasicParam><HeartBeatInterval>60</HeartBeatInterval></BasicParam>`
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID>`+blocks+`</Control>`))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			var body []byte
			select {
			case body = <-sent:
			case <-time.After(2 * time.Second):
				t.Fatal("missing command")
			}
			command, err := manscdp.ParseCascadeConfigWrite(body)
			require.NoError(t, err)
			result := `<Result>OK</Result>`
			switch scenario {
			case "ERROR":
				result = `<Result>ERROR</Result>`
			case "missing Result":
				result = ""
			case "duplicate Result":
				result = `<Result>ERROR</Result><Result>OK</Result>`
			}
			response := []byte(fmt.Sprintf(`<Response><CmdType>DeviceConfig</CmdType><SN>%d</SN><DeviceID>%s</DeviceID>%s</Response>`, command.SN(), command.DeviceID(), result))
			require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "config-result", "2", response))
			wait()
			if scenario == "upstream changed" {
				require.Empty(t, upstream.requests)
				return
			}
			select {
			case response := <-upstream.requests:
				ack, err := manscdp.ParseCascadeConfigWriteResponse(response.Body(), manscdp.ConfigDownloadExpectation{SN: 97, DeviceID: "34020000001320000012"})
				require.NoError(t, err)
				require.False(t, ack.Accepted())
			default:
				t.Fatal("missing failed result")
			}
			require.Empty(t, sent, "application rejection must not trigger another write")
		})
	}
}
