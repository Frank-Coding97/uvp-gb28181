package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/traffic"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	globalapp "uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
)

type trafficTestNodes struct{ mediaNode *node.Node }

func (n trafficTestNodes) Get(id int64) (*node.Node, bool) {
	return n.mediaNode, n.mediaNode != nil && n.mediaNode.ID == id
}

type trafficTestLocations struct{ nodeID int64 }

func (l trafficTestLocations) Lookup(string) (int64, bool) { return l.nodeID, l.nodeID != 0 }

type trafficTestClient struct {
	media   []zlm.MediaInfo
	players []zlm.MediaPlayer
	kicked  string
}

func (c *trafficTestClient) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	return c.media, nil
}
func (c *trafficTestClient) GetMediaPlayerList(context.Context, string, string, string, string) ([]zlm.MediaPlayer, error) {
	return c.players, nil
}
func (c *trafficTestClient) KickSession(_ context.Context, id string) error {
	c.kicked = id
	return nil
}

func newDeviceTrafficControllerTest(t *testing.T) (*DeviceTrafficController, *gorm.DB, *trafficTestClient) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceTrafficDaily{},
		&gbmodels.GbDeviceTrafficSession{}, &gbmodels.GbDeviceTrafficGap{},
	))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "device-1", Name: "device", OwnerDeptID: 7}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "device-1", ChannelID: "channel-1", StreamID: "stream-1", OwnerDeptID: 7}).Error)
	client := &trafficTestClient{
		media:   []zlm.MediaInfo{{Schema: "ws", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", ReaderCount: 1}},
		players: []zlm.MediaPlayer{{PeerIP: "10.0.0.1", PeerPort: 1000, Identifier: "viewer-1", TypeID: "TcpSession"}},
	}
	controller := NewDeviceTrafficController(db, traffic.NewRealtimeStore(time.Minute), trafficTestNodes{mediaNode: &node.Node{ID: 8}}, trafficTestLocations{nodeID: 8})
	controller.clientFor = func(*node.Node) trafficNodeClient { return client }
	return controller, db, client
}

func runTrafficHandler(t *testing.T, method, target string, claims *globalapp.Claims, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, nil)
	if claims != nil {
		ctx.Set(consts.BindContextKeyName, claims)
	}
	handler(ctx)
	return recorder
}

func TestDeviceTrafficSummaryAndTrendFilterChannel(t *testing.T) {
	controller, db, _ := newDeviceTrafficControllerTest(t)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, db.Create(&gbmodels.GbDeviceTrafficDaily{StatDate: day, DeviceCode: "device-1", ChannelCode: "channel-1", UpstreamBytes: 100, DownstreamBytes: 40}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceTrafficDaily{StatDate: day, DeviceCode: "device-1", ChannelCode: "channel-2", UpstreamBytes: 900}).Error)

	response := runTrafficHandler(t, http.MethodGet, "/?deviceId=device-1&channelId=channel-1", nil, controller.Summary)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Code int `json:"code"`
		Data struct {
			UpstreamBytes, DownstreamBytes, TotalBytes uint64
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Zero(t, payload.Code)
	require.EqualValues(t, 100, payload.Data.UpstreamBytes)
	require.EqualValues(t, 40, payload.Data.DownstreamBytes)
	require.EqualValues(t, 140, payload.Data.TotalBytes)
}

func TestDeviceTrafficViewersReturnsConnectionCapability(t *testing.T) {
	controller, _, _ := newDeviceTrafficControllerTest(t)
	response := runTrafficHandler(t, http.MethodGet, "/?deviceId=device-1&channelId=channel-1", nil, controller.Viewers)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "viewer-1")
	require.Contains(t, response.Body.String(), "kickable")
}

func TestDeviceTrafficKickRejectsOrdinaryUser(t *testing.T) {
	controller, _, client := newDeviceTrafficControllerTest(t)
	response := runTrafficHandler(t, http.MethodPost, "/?deviceId=device-1&channelId=channel-1", &globalapp.Claims{ClaimsUser: globalapp.ClaimsUser{UserID: 999}}, controller.KickViewer)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Empty(t, client.kicked)
}
