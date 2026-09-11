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
	media          []zlm.MediaInfo
	players        []zlm.MediaPlayer
	mediaListCalls []string
	kicked         string
}

func (c *trafficTestClient) GetMediaList(_ context.Context, _, _, stream string) ([]zlm.MediaInfo, error) {
	c.mediaListCalls = append(c.mediaListCalls, stream)
	if stream == "" {
		return c.media, nil
	}
	filtered := make([]zlm.MediaInfo, 0, len(c.media))
	for _, item := range c.media {
		if item.Stream == stream {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
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
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceTrafficDaily{}, &gbmodels.GbDeviceTrafficHourly{},
		&gbmodels.GbDeviceTrafficSession{}, &gbmodels.GbDeviceTrafficGap{},
	))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "device-1", Name: "device", OwnerDeptID: 7}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "device-1", ChannelID: "channel-1", Name: "前门摄像机", StreamID: "stream-1", OwnerDeptID: 7}).Error)
	client := &trafficTestClient{
		media: []zlm.MediaInfo{{
			Schema: "ws", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", ReaderCount: 1,
			CreateStamp: 1700000000, AliveSecond: 120, BytesSpeed: 250000, TotalBytes: 9000000,
		}},
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

func TestTrafficRangeDefaultsToSevenDays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	from, to, err := trafficRange(ctx)
	require.NoError(t, err)
	require.Equal(t, 6*24*time.Hour, to.Sub(from))
	require.Equal(t, 0, to.Hour())
	require.Equal(t, time.UTC, to.Location())
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

func TestDeviceTrafficHourlyTrendReturnsTwentyFourBuckets(t *testing.T) {
	controller, db, _ := newDeviceTrafficControllerTest(t)
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	currentHour := time.Now().In(location).Truncate(time.Hour)
	previousHour := currentHour.Add(-time.Hour)
	require.NoError(t, db.Create(&gbmodels.GbDeviceTrafficHourly{
		StatHour: previousHour, DeviceCode: "device-1", ChannelCode: "channel-1",
		UpstreamBytes: 120, DownstreamBytes: 30,
	}).Error)

	response := runTrafficHandler(t, http.MethodGet, "/?deviceId=device-1&granularity=hour", nil, controller.Trend)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				Bucket          string `json:"bucket"`
				UpstreamBytes   uint64 `json:"upstreamBytes"`
				DownstreamBytes uint64 `json:"downstreamBytes"`
				TotalBytes      uint64 `json:"totalBytes"`
			} `json:"list"`
			Granularity string `json:"granularity"`
			Timezone    string `json:"timezone"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Zero(t, payload.Code)
	require.Equal(t, "hour", payload.Data.Granularity)
	require.Equal(t, "Asia/Shanghai", payload.Data.Timezone)
	require.Len(t, payload.Data.List, 24)
	require.EqualValues(t, 150, payload.Data.List[22].TotalBytes)
}

func TestDeviceTrafficSessionsFiltersSelectedBucketAndPaginates(t *testing.T) {
	controller, db, _ := newDeviceTrafficControllerTest(t)
	start := time.Date(2026, 8, 21, 8, 0, 0, 0, time.UTC)
	inside := start.Add(15 * time.Minute)
	outside := start.Add(2 * time.Hour)
	rows := []gbmodels.GbDeviceTrafficSession{
		{BusinessKey: "inside-1", NodeID: 1, Direction: "upstream", DeviceCode: "device-1", ChannelCode: "channel-1", State: "settled", StartedAt: &inside, SettledTotalBytes: 100},
		{BusinessKey: "inside-2", NodeID: 1, Direction: "downstream", DeviceCode: "device-1", ChannelCode: "channel-1", State: "settled", StartedAt: &inside, SettledTotalBytes: 200},
		{BusinessKey: "outside", NodeID: 1, Direction: "upstream", DeviceCode: "device-1", ChannelCode: "channel-1", State: "settled", StartedAt: &outside, SettledTotalBytes: 300},
	}
	require.NoError(t, db.Create(&rows).Error)

	target := "/?deviceId=device-1&from=" + start.Format(time.RFC3339) + "&to=" + start.Add(time.Hour).Format(time.RFC3339) + "&page=2&pageSize=1"
	response := runTrafficHandler(t, http.MethodGet, target, nil, controller.Sessions)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Code int `json:"code"`
		Data struct {
			List     []gbmodels.GbDeviceTrafficSession `json:"list"`
			Total    int64                             `json:"total"`
			Page     int                               `json:"page"`
			PageSize int                               `json:"pageSize"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Zero(t, payload.Code)
	require.EqualValues(t, 2, payload.Data.Total)
	require.Equal(t, 2, payload.Data.Page)
	require.Equal(t, 1, payload.Data.PageSize)
	require.Len(t, payload.Data.List, 1)
	require.NotEqual(t, "outside", payload.Data.List[0].BusinessKey)
}

func TestDeviceTrafficViewersReturnsConnectionCapability(t *testing.T) {
	controller, _, _ := newDeviceTrafficControllerTest(t)
	response := runTrafficHandler(t, http.MethodGet, "/?deviceId=device-1&channelId=channel-1", nil, controller.Viewers)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Code int `json:"code"`
		Data struct {
			List         []currentViewerStream `json:"list"`
			Total        int                   `json:"total"`
			TotalViewers int                   `json:"totalViewers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Zero(t, payload.Code)
	require.Equal(t, 1, payload.Data.Total)
	require.Equal(t, 1, payload.Data.TotalViewers)
	require.Len(t, payload.Data.List, 1)
	stream := payload.Data.List[0]
	require.Equal(t, "前门摄像机", stream.ChannelName)
	require.Equal(t, "channel-1", stream.ChannelID)
	require.Equal(t, uint64(120), stream.AliveSecond)
	require.Equal(t, float64(2000), stream.BitrateKbps)
	require.Equal(t, uint64(9000000), stream.TotalBytes)
	require.Equal(t, "2023-11-14T22:13:20Z", stream.StartedAt.Format(time.RFC3339))
	require.Len(t, stream.Viewers, 1)
	require.Equal(t, "viewer-1", stream.Viewers[0].ID)
	require.True(t, stream.Viewers[0].Kickable)
}

func TestDeviceTrafficViewersDefaultsToAllDeviceChannels(t *testing.T) {
	controller, db, client := newDeviceTrafficControllerTest(t)
	require.NoError(t, db.Create(&gbmodels.GbChannel{
		DeviceID: "device-1", ChannelID: "channel-2", StreamID: "stream-2", OwnerDeptID: 7,
	}).Error)
	client.media = append(client.media, zlm.MediaInfo{Schema: "ws", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-2", ReaderCount: 1})

	response := runTrafficHandler(t, http.MethodGet, "/?deviceId=device-1", nil, controller.Viewers)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Code int `json:"code"`
		Data struct {
			List         []currentViewerStream `json:"list"`
			Total        int                   `json:"total"`
			TotalViewers int                   `json:"totalViewers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Zero(t, payload.Code)
	require.Equal(t, 2, payload.Data.Total)
	require.Equal(t, 2, payload.Data.TotalViewers)
	require.ElementsMatch(t, []string{"channel-1", "channel-2"}, []string{payload.Data.List[0].ChannelID, payload.Data.List[1].ChannelID})
	require.Equal(t, []string{""}, client.mediaListCalls, "同一媒体节点的全部通道应合并为一次媒体列表查询")
}

func TestDeviceTrafficKickRejectsOrdinaryUser(t *testing.T) {
	controller, _, client := newDeviceTrafficControllerTest(t)
	response := runTrafficHandler(t, http.MethodPost, "/?deviceId=device-1&channelId=channel-1", &globalapp.Claims{ClaimsUser: globalapp.ClaimsUser{UserID: 999}}, controller.KickViewer)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Empty(t, client.kicked)
}
