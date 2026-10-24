package gb28181

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

type cascadeSnapshotTestConfig struct{ cascadeInviteTestConfig }

func (c cascadeSnapshotTestConfig) GetString(key string) string {
	if key == gbconfig.SnapshotUploadBaseURLConfigKey {
		return "https://capture.example.test/uvp"
	}
	return c.cascadeInviteTestConfig.GetString(key)
}

func TestCascadeSnapshotSameUpstreamSessionIDIsIsolated(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, first, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		return uac.TrackedMessageResult{}, fmt.Errorf("unexpected send")
	}))
	app.ConfigYml = cascadeSnapshotTestConfig{}
	require.NoError(t, db.Model(&first).Update("config_enabled", true).Error)
	first.ConfigEnabled = true
	require.NoError(t, db.Table("gb_device").Where("id=10").Update("effective_version", "2022").Error)
	projection, err := store.ProjectionSnapshot(t.Context(), first.ID)
	require.NoError(t, err)
	second := first
	second.ID, second.Name, second.Host, second.UpstreamServerID = 0, "snapshot second", "192.0.2.11", "34020000002000000003"
	require.NoError(t, db.Create(&second).Error)
	device := projection.Devices[0]
	device.ID, device.PlatformID, device.PublishedDeviceID = 0, second.ID, "34020000001320000021"
	require.NoError(t, db.Create(&device).Error)
	channel := projection.Channels[0]
	channel.ID, channel.PlatformID, channel.DeviceProjectionID, channel.PublishedChannelID = 0, second.ID, device.ID, "34020000001320000022"
	require.NoError(t, db.Create(&channel).Error)
	downstream := make(chan *manscdp.SnapShotBlock, 2)
	execute := func(_ context.Context, _ cascadeConfigWriteTarget, c *manscdp.CascadeConfigWrite, _ ptz.CascadeConfigAuthorization, _ string) (cascadeConfigQueryResult, error) {
		snapshot, ok := c.SnapshotConfig()
		if !ok {
			return cascadeConfigQueryResult{}, fmt.Errorf("missing snapshot")
		}
		downstream <- snapshot
		return cascadeConfigQueryResult{body: []byte(`<Response><CmdType>DeviceConfig</CmdType><SN>1</SN><DeviceID>34020000001320000002</DeviceID><Result>OK</Result></Response>`), authorize: func(context.Context) error { return nil }}, nil
	}
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 6)}
	clients := newCascadePlatformClientFactory(upstream, nil, time.Second)
	handler, wait := newCascadeConfigWriteHandlerWithExecutor(store, clients, execute)
	t.Cleanup(wait)
	sid := "01234567890123456789012345678901"
	var captures []*manscdp.SnapShotBlock
	for i, p := range []model.GbCascadePlatform{first, second} {
		published := []string{"34020000001320000012", "34020000001320000022"}[i]
		raw := fmt.Sprintf(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>%s</DeviceID><SnapShotConfig><SnapNum>1</SnapNum><UploadURL>https://upstream.example.test/%d/upload?auth=secret</UploadURL><SessionID>%s</SessionID></SnapShotConfig></Control>`, published, i, sid)
		req := newCascadeStatusRequest(p, p.LocalDeviceID, p.LocalDeviceID, []byte(raw))
		req.SetSource(fmt.Sprintf("%s:%d", p.Host, p.Port))
		tx := siptest.NewServerTxRecorder(req)
		require.True(t, handler(req, tx))
		require.Equal(t, 200, tx.Result()[0].StatusCode)
		tx.Terminate()
		wait()
		select {
		case capture := <-downstream:
			captures = append(captures, capture)
		case <-time.After(time.Second):
			t.Fatal("capture not executed")
		}
		awaitCascadeStatusResponse(t, upstream.requests)
	}
	require.NotEqual(t, sid, *captures[0].SessionID, "downstream must have an independent SessionID")
	require.NotEqual(t, *captures[0].SessionID, *captures[1].SessionID, "same device and same upstream ID must still produce separate captures")
	for _, capture := range captures {
		require.Contains(t, *capture.UploadURL, "https://capture.example.test/uvp/api/gb28181/cascade-snapshots/uploads/")
		require.NotContains(t, *capture.UploadURL, "auth=secret")
	}
	require.NotEqual(t, *captures[0].UploadURL, *captures[1].UploadURL)
	// Recover each HTTP route from persistent state after both requests exist.
	// Equal upstream SessionIDs must not send a picture to the other storage.
	uploads := 0
	uploadRouter := gin.New()
	uploadRouter.PUT("/api/gb28181/cascade-snapshots/uploads/*token", newCascadeSnapshotUploadHandler(store, &http.Client{Transport: cascadeSnapshotRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, fmt.Sprintf("/%d/upload/photo.jpg", uploads), req.URL.Path)
		require.Equal(t, sid, req.URL.Query().Get("SessionID"))
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, []byte{0xff, 0xd8, byte(uploads), 0xff, 0xd9}, payload)
		uploads++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}))
	for i, capture := range captures {
		destination, err := url.Parse(*capture.UploadURL)
		require.NoError(t, err)
		request := httptest.NewRequest("PUT", strings.TrimPrefix(destination.Path, "/uvp")+"/photo.jpg?SessionID="+*capture.SessionID, bytes.NewReader([]byte{0xff, 0xd8, byte(i), 0xff, 0xd9}))
		request.Header.Set("Content-Type", "image/jpeg")
		response := httptest.NewRecorder()
		uploadRouter.ServeHTTP(response, request)
		require.Equal(t, 200, response.Code)
	}
	require.Equal(t, 2, uploads)
	completion := newCascadeSnapshotResultHandler(store, clients)
	for i, capture := range captures {
		body, err := manscdp.BuildUploadSnapShotFinished(protocol.ProfileFor(protocol.Version2022), manscdp.UploadSnapShotFinished{SN: "9001", DeviceID: "34020000001320000002", SessionID: *capture.SessionID, FileIDs: []string{fmt.Sprintf("file-%d", i)}})
		require.NoError(t, err)
		req := newCascadeStatusRequest(first, first.LocalDeviceID, first.LocalDeviceID, body)
		req.From().Address.User = "34020000001320000001"
		tx := siptest.NewServerTxRecorder(req)
		require.True(t, completion(req, tx))
		require.Equal(t, 200, tx.Result()[0].StatusCode)
		tx.Terminate()
		sent := awaitCascadeStatusResponse(t, upstream.requests)
		notice, err := manscdp.ParseUploadSnapShotFinished(sent.Body())
		require.NoError(t, err)
		require.Equal(t, sid, notice.SessionID)
		require.Equal(t, []string{"34020000001320000012", "34020000001320000022"}[i], notice.DeviceID)
		require.Equal(t, []string{first.UpstreamServerID, second.UpstreamServerID}[i], sent.To().Address.User)
	}
}
