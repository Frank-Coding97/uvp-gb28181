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
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeSnapshotConfigReadIsolation(t *testing.T) {
	for _, name := range []string{"own", "completed", "expired", "foreign", "unknown", "legacy", "old epoch", "old revision", "other source", "config disabled", "mapping changed before send", "config disabled before send"} {
		t.Run(name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				return uac.TrackedMessageResult{}, fmt.Errorf("unexpected send")
			}))
			require.NoError(t, db.Model(&platform).Update("config_enabled", true).Error)
			platform.ConfigEnabled = true
			require.NoError(t, db.Table("gb_device").Where("id=10").Update("effective_version", "2022").Error)
			session := model.GbCascadeSnapshotSession{PlatformID: platform.ID, UpstreamSessionID: "01234567890123456789012345678901", UpstreamUploadURL: "https://own.example.test/upload?auth=own-secret", PublishedChannelID: "34020000001320000012", SourceDeviceID: 10, SourceChannelID: 20, SourceDeviceEpoch: 1, ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision, State: model.CascadeSnapshotSessionPending, ExpiresAt: time.Now().Add(time.Minute)}
			switch name {
			case "completed":
				session.State = model.CascadeSnapshotSessionCompleted
			case "expired":
				session.ExpiresAt = time.Now().Add(-time.Minute)
			case "foreign":
				session.PlatformID++
			case "old epoch":
				session.SourceDeviceEpoch++
			case "old revision":
				session.ConfigRevision++
			case "other source":
				session.SourceChannelID++
			}
			saved, _, err := reserveCascadeSnapshotSession(t.Context(), db, session)
			require.NoError(t, err)
			sid := *saved.DownstreamSessionID
			token := *saved.UploadToken
			upload := "https://capture.example.test/api/gb28181/cascade-snapshots/uploads/" + *saved.UploadToken
			if name == "unknown" {
				sid = "unknown-local-snapshot-012345678901"
				upload = "https://local-private.example.test/upload?auth=local-secret"
			}
			if name == "legacy" {
				require.NoError(t, db.Model(&saved).Updates(map[string]interface{}{"downstream_session_id": nil, "upload_token": nil}).Error)
				sid = saved.UpstreamSessionID
				upload = saved.UpstreamUploadURL
			}
			if name == "config disabled" {
				require.NoError(t, db.Model(&platform).Update("config_enabled", false).Error)
			}
			source := []byte(fmt.Sprintf(`<Response><CmdType>ConfigDownload</CmdType><SN>1</SN><DeviceID>34020000001320000002</DeviceID><Result>OK</Result><SnapShot><SnapNum>2</SnapNum><Interval>5</Interval><UploadURL>%s</UploadURL><SessionID>%s</SessionID></SnapShot></Response>`, upload, sid))
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 2)}
			handler, wait := newCascadeConfigQueryHandlerWithQuery(store, newCascadePlatformClientFactory(upstream, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string, string) (cascadeConfigQueryResult, error) {
				return cascadeConfigQueryResult{body: source, authorize: func(context.Context) error {
					if name == "config disabled before send" {
						return db.Model(&platform).Update("config_enabled", false).Error
					}
					if name == "mapping changed before send" {
						return db.Model(&saved).Update("upstream_upload_url", "https://replacement.example.test/upload").Error
					}
					return nil
				}}, nil
			})
			t.Cleanup(wait)
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>ConfigDownload</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><ConfigType>SnapShotConfig</ConfigType></Query>`))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			response := string(awaitCascadeStatusResponse(t, upstream.requests).Body())
			require.NotContains(t, response, "capture.example.test")
			require.NotContains(t, response, token)
			require.NotContains(t, response, "local-private")
			own := name == "own" || name == "completed" || name == "expired" || name == "legacy"
			if !own {
				require.Contains(t, response, "<Result>ERROR</Result>")
				require.NotContains(t, response, "own-secret")
				require.NotContains(t, response, "<SnapShot>")
				return
			}
			require.Contains(t, response, "<SnapNum>2</SnapNum>")
			require.Contains(t, response, "<Interval>5</Interval>")
			if own {
				require.Contains(t, response, "<SessionID>"+saved.UpstreamSessionID+"</SessionID>")
				require.Contains(t, response, strings.ReplaceAll(saved.UpstreamUploadURL, "&", "&amp;"))
			} else {
				require.NotContains(t, response, "<SessionID>")
				require.NotContains(t, response, "<UploadURL>")
			}
			require.Contains(t, string(source), sid, "response sanitization must not mutate the cached device reply")
		})
	}
}
