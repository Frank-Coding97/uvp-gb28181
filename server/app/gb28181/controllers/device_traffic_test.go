package controllers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/traffic"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	globalapp "uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	sysmodels "uvplatform.com/uvp-gb28181/app/models"
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
	// ⛔⛔ 必须连**权限相关的表**一起建：数据权限层
	//（datascope.GetOwnerDeptIDsWithDB）会去查 sys_users / sys_user_role /
	//   sys_role / sys_department，缺表时 GORM 返回 error ⇒ 该函数走
	//   `return []uint{}, true`（fail-closed，需要过滤），接着查 gb_device_grant
	//   又缺表 ⇒ 整条 VisibilityScope 报错，controller 走FailAndAbort抛
	//   request_aborted panic，**测试报的是 panic 而不是断言失败**。
	//   这个坑很隐蔽：缺表时不是"403"，而是"测试崩了"。
	//   ⇒ 建全，让每张权限表都真实存在，测出来的才是业务行为。
	require.NoError(t, db.AutoMigrate(
		&sysmodels.User{}, &sysmodels.SysRole{}, &sysmodels.SysDepartment{}, &gbmodels.GbDeviceGrant{},
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
	return runTrafficHandlerWithBody(t, method, target, "", claims, handler)
}

// runTrafficHandlerWithBody 是带JSON body 的版本。
// ⛔ 分开而不是加参数：绝大多数用例没有 body，多一个参数只会让每个调用点多写一个 nil。
func runTrafficHandlerWithBody(t *testing.T, method, target, body string, claims *globalapp.Claims, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	ctx.Request = httptest.NewRequest(method, target, reader)
	if body != "" {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	if claims != nil {
		ctx.Set(consts.BindContextKeyName, claims)
	}
	// ⛔ 必须自己 recover：业务层 FailAndAbort 是**抛 panic**（consts.RequestAborted）
	//   而不是写状态码后 return —— 生产由 gin 中间件兜底。测试里不 recover 的话，
	//   任何走到 FailAndAbort 的用例都会以 panic 形式失败，
	//   报出来的是"panic: request_aborted"而不是断言不通过，最难读。
	//   ⛔ 不能复用 zlm_node_test.go 里的同名 helper：那个在 package controllers_test，
	//   本文件是 package controllers（内部包，需访问 currentViewer 等私有类型）。
	func() {
		defer func() {
			if r := recover(); r != nil && r != consts.RequestAborted {
				// 其它 panic 一律继续抛出 —— 只放过这一种，
				// 否则真实崩溃会被静默吞掉、测试假绿。
				panic(r)
			}
		}()
		handler(ctx)
	}()
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

// TestDeviceTrafficKickRejectsOrdinaryUser 锁定「普通用户不能强退观看连接」。
//
// ⭐ 这个保护**没有删除**，只是换了执行它的层：
// 原来在业务层第一行有一道 `if !trafficSuperAdmin(c) { 403"仅超级管理员…" }`，
// 而 trafficSuperAdmin 判的是「用户在 server.notcheckuser 白名单里」——
// 该配置默认 []，所以**对所有人都是 false**，系统管理员也被挡，功能实际不可用。
// 而且前端按钮显隐走 hasPermission(...) 不看 canKick ⇒ 只能得到
// 「按钮可见、点了报 403」这种最差体验。
//
// ⇒ 现在由**数据权限层**（datascope.VisibilityScope，fail-closed）承担这个保护：
// 没有该设备可见性的用户在 scope() 阶段就被判「设备不存在」，压根到不了踢流那步。
// casbin 中间件则负责「这个接口你有没有调它的权限」，两层各管一件事、互不重复。
//
// ⛔ 断言必须落在**「连接没被踢掉」**这个行为上，不要绑 HTTP 状态码：
//   业务层的 FailAndAbort 是**抛 panic**（consts.RequestAborted），
//   生产由 gin 中间件补上状态码；在测试里状态码仍是 recorder 的默认值 200。
//   （我第一版断言 403 / NotEqual(200) 都是假断言 —— 一个测"文案"、
//   一个测"gin 有没有帮我填状态码"，都不是我要保证的东西。）
//   ⭐ 更普适：拦截层换了措辞、换了实现层次，行为断言都不会假失败。
func TestDeviceTrafficKickRejectsOrdinaryUser(t *testing.T) {
	controller, _, client := newDeviceTrafficControllerTest(t)
	runTrafficHandler(t, http.MethodPost, "/?deviceId=device-1&channelId=channel-1",
		&globalapp.Claims{ClaimsUser: globalapp.ClaimsUser{UserID: 999}}, controller.KickViewer)
	require.Empty(t, client.kicked,
		"数据权限外的用户绝不能触达媒体节点（fail-closed 失效 = 越权踢流）")
}

// TestDeviceTrafficKickSucceedsForVisibleUser 是上面那条的**正向对照**，
// 也是这条护栏能不能被信任的关键。
//
// ⭐ 为什么必须有它（我第一版就踩了）：
//
//	只有"拒绝"用例时，把数据权限层整段删掉，测试**照样绿** ——
//	因为请求继续往下走、在参数校验（缺 body）处失败，kicked 依然为空。
//	⇒ "什么都没发生"无法区分"被正确拦住"和"在更早一步被别的原因挡住"。
//	配了这条"能踢成"的用例后，任何让请求到不了 KickSession 的改动都会红，
//	护栏才真正有区分能力。
func TestDeviceTrafficKickSucceedsForVisibleUser(t *testing.T) {
	controller, db, client := newDeviceTrafficControllerTest(t)
	// 造一个对该设备有可见性的用户（与 device-1 的归属部门 7 相同）
	require.NoError(t, db.Create(&sysmodels.User{
		BaseModel: sysmodels.BaseModel{ID: 1000}, Username: "visible-user", DeptID: 7,
	}).Error)

	response := runTrafficHandlerWithBody(t, http.MethodPost,
		"/?deviceId=device-1&channelId=channel-1",
		`{"id":"viewer-1","schema":"ws"}`,
		&globalapp.Claims{ClaimsUser: globalapp.ClaimsUser{UserID: 1000}}, controller.KickViewer)

	require.Equal(t, http.StatusOK, response.Code)
	require.NotContains(t, response.Body.String(), "不可用",
		"有可见性的用户不应被判成设备不可见：%s", response.Body.String())
	require.Equal(t, "viewer-1", client.kicked,
		"有可见性 + 参数合法时必须真的踢掉连接（这是上方拒绝用例的对照）")
}

// TestDeviceTrafficViewersReportsCanKickForAuthorizedUser 钉住 canKick 的新语义：
// 它恒为 true，因为「能不能强退」由 casbin + 数据权限决定，前端也用
// hasPermission 自己判显隐；后端再返回一个可能为 false 的值只会造成
// 「按钮可见但点了 403」的不一致。
func TestDeviceTrafficViewersReportsCanKickForAuthorizedUser(t *testing.T) {
	controller, _, _ := newDeviceTrafficControllerTest(t)
	response := runTrafficHandler(t, http.MethodGet, "/?deviceId=device-1&channelId=channel-1", nil, controller.Viewers)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Data struct {
			CanKick bool `json:"canKick"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.True(t, payload.Data.CanKick,
		"canKick 不再由 notcheckuser 白名单决定，必须恒为 true（真授权在 casbin 层）")
}
