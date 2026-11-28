package gb28181

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

type failedSnapshotResponseTx struct{ sip.ServerTransaction }

func (failedSnapshotResponseTx) Respond(*sip.Response) error { return errors.New("transport failed") }

func TestCascadeSnapshotRequestBinding(t *testing.T) {
	for _, scenario := range []string{"different transaction", "changed URL", "changed SessionID", "added device scope", "removed snapshot", "completed replay", "failed replay response", "concurrent retry", "failed application replay"} {
		t.Run(scenario, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				return uac.TrackedMessageResult{}, fmt.Errorf("unexpected network send")
			}))
			configureCascadeSnapshotIngress(t, db)
			require.NoError(t, db.Model(&platform).Update("config_enabled", true).Error)
			require.NoError(t, db.Table("gb_device").Where("id=10").Update("effective_version", "2022").Error)
			require.NoError(t, db.AutoMigrate(&model.GbCascadeSnapshotSession{}))
			var executions atomic.Int32
			entered, release := make(chan struct{}, 2), make(chan struct{})
			execute := func(ctx context.Context, _ cascadeConfigWriteTarget, c *manscdp.CascadeConfigWrite, _ ptz.CascadeConfigAuthorization, _ string) (cascadeConfigQueryResult, error) {
				executions.Add(1)
				entered <- struct{}{}
				if scenario == "failed replay response" || scenario == "concurrent retry" {
					select {
					case <-release:
					case <-ctx.Done():
						return cascadeConfigQueryResult{}, ctx.Err()
					}
				}
				if scenario == "failed application replay" {
					return cascadeConfigQueryResult{}, errors.New("device rejected capture")
				}
				return cascadeConfigQueryResult{body: []byte(`<Response><CmdType>DeviceConfig</CmdType><SN>1</SN><DeviceID>34020000001320000002</DeviceID><Result>OK</Result></Response>`), authorize: func(context.Context) error { return nil }}, nil
			}
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 4)}
			handler, wait := newCascadeConfigWriteHandlerWithExecutor(store, newCascadePlatformClientFactory(upstream, nil, time.Second), execute)
			t.Cleanup(wait)
			released := false
			defer func() {
				if !released {
					close(release)
				}
			}()
			block := `<SnapShotConfig><SnapNum>1</SnapNum><UploadURL>https://example.test/upload</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShotConfig>`
			body := func(config string) []byte {
				return []byte(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID>` + config + `</Control>`)
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body(block))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("no first execution")
			}
			if scenario == "failed replay response" {
				retry := siptest.NewServerTxRecorder(req)
				defer retry.Terminate()
				require.True(t, handler(req, failedSnapshotResponseTx{retry}))
				var saved model.GbCascadeSnapshotSession
				require.NoError(t, db.First(&saved).Error)
				require.Equal(t, model.CascadeSnapshotSessionPending, saved.State, "failed duplicate response must not cancel the original capture")
				return
			}
			if scenario == "concurrent retry" {
				retry := siptest.NewServerTxRecorder(req)
				defer retry.Terminate()
				require.True(t, handler(req, retry))
				require.Equal(t, 200, retry.Result()[0].StatusCode)
				close(release)
				released = true
				wait()
				require.EqualValues(t, 1, executions.Load(), "concurrent retransmissions must share the original execution")
				require.Len(t, upstream.requests, 2)
				return
			}
			wait()
			select {
			case <-upstream.requests:
			case <-time.After(time.Second):
				t.Fatal("no original result")
			}
			retry := req.Clone()
			switch scenario {
			case "different transaction":
				retry.CSeq().SeqNo++
			case "changed URL":
				retry.SetBody(body(`<SnapShotConfig><SnapNum>1</SnapNum><UploadURL>https://example.test/changed</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShotConfig>`))
			case "changed SessionID":
				retry.SetBody(body(`<SnapShotConfig><SnapNum>1</SnapNum><UploadURL>https://example.test/upload</UploadURL><SessionID>11234567890123456789012345678901</SessionID></SnapShotConfig>`))
			case "added device scope":
				retry.SetBody(body(block + `<BasicParam><HeartBeatInterval>60</HeartBeatInterval></BasicParam>`))
			case "removed snapshot":
				retry.SetBody(body(`<FrameMirror>1</FrameMirror>`))
			case "completed replay":
				require.NoError(t, db.Model(&model.GbCascadeSnapshotSession{}).Where("platform_id=?", platform.ID).Update("state", model.CascadeSnapshotSessionCompleted).Error)
			}
			retryTx := siptest.NewServerTxRecorder(retry)
			defer retryTx.Terminate()
			require.True(t, handler(retry, retryTx))
			if scenario == "completed replay" || scenario == "failed application replay" {
				require.Equal(t, 200, retryTx.Result()[0].StatusCode)
				wait()
				select {
				case response := <-upstream.requests:
					want := "OK"
					if scenario == "failed application replay" {
						want = "ERROR"
					}
					require.Contains(t, string(response.Body()), "<Result>"+want+"</Result>")
				case <-time.After(time.Second):
					t.Fatal("no cached application result")
				}
			} else {
				require.Equal(t, 409, retryTx.Result()[0].StatusCode)
			}
			require.EqualValues(t, 1, executions.Load(), "a capture must not execute twice or acquire extra configuration scopes")
		})
	}
}
