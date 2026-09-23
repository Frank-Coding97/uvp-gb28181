package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// targetTrackFixture 在维护用例的 fixture 之上补建目标跟踪落库表。
//
// ⛔ 用 extensions 而不是改 maintenanceFixture：那个 fixture 服务于重启/格式化那一族，
// 往它的 AutoMigrate 列表里塞本主题的表，会让"存储卡格式化"的用例也依赖本功能的模型。
func newTargetTrackFixture(t *testing.T) maintenanceFixture {
	t.Helper()
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	require.NoError(t, fixture.db.AutoMigrate(&gbmodels.GbDeviceTargetTrack{}))
	return fixture
}

// targetTrackRouter 只注册本主题相关的两条路由（与生产 routes.go 的路径同形）。
//
// ⛔ 路径手写字面量是**故意的**：用例要证明"路由注册的路径就是迁移里登记的那个"，
// 从被测对象自己取值来断言自己等于没测。
func targetTrackRouter(f maintenanceFixture, middleware ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	if len(middleware) > 0 {
		router.Use(middleware...)
	}
	router.GET("/channel/:id/target-track", f.controller.GetChannelTargetTrack)
	router.POST("/channel/:id/target-track", f.controller.SetChannelTargetTrack)
	return router
}

func targetTrackCall(t *testing.T, router *gin.Engine, method string, channelID uint, body string) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	url := "/channel/" + uintStr(channelID) + "/target-track"
	if body == "" {
		request = httptest.NewRequest(method, url, nil)
	} else {
		request = httptest.NewRequest(method, url, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeTargetTrackData(t *testing.T, response *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope), response.Body.String())
	return envelope.Data
}

func loadTargetTrackIntent(t *testing.T, db *gorm.DB) (gbmodels.GbDeviceTargetTrack, bool) {
	t.Helper()
	var row gbmodels.GbDeviceTargetTrack
	result := db.Limit(1).Find(&row)
	require.NoError(t, result.Error)
	return row, result.RowsAffected == 1
}

// ⛔ 本组最重要的一条口径：读接口返回的 `deviceAcknowledged` 恒为 false，
// 而且**不是**"暂时还没收到应答"—— 目标跟踪是无应答命令（9.3.1 d) + 表 1 序号 13），
// 且 2022 全文没有任何查询目标跟踪状态的命令。界面上若按"等一会儿就有"来措辞，
// 用户会一直等一个永远不会来的回执。
func TestGetChannelTargetTrackReportsNoAckAndNeverSendsSIP(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	seedMaintenanceAdmin(t, fixture.db)
	router := targetTrackRouter(fixture, maintenanceClaims(17))

	response := targetTrackCall(t, router, http.MethodGet, fixture.channel.ID, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	data := decodeTargetTrackData(t, response)
	require.Equal(t, false, data["deviceAcknowledged"])
	require.Equal(t, false, data["responseRequired"])
	require.Nil(t, data["intent"], "还没下发过就不能凭空给出一条指令")
	require.Contains(t, data["windowHint"], "实际渲染的像素尺寸")
	// 能力默认 unknown：设备没声明过就不臆断支持（目标跟踪需要双目全景结构）。
	capability, ok := data["capability"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, string(capabilityUnknownForTest), capability["state"])
	require.Zero(t, fixture.sender.calls, "读接口不许产生任何 SIP 报文")
}

const capabilityUnknownForTest = "unknown"

// 自动跟踪：一次调用即终态 sent（无应答命令由 Execute 同步发出），
// 报文里只有 <TargetTrack>Auto</TargetTrack>，不带框、不带 DeviceID2；
// 同时把"平台已下发"落成意图，并在**同一个响应**里回给前端。
func TestSetChannelTargetTrackAutoRecordsIntentAndWiresStandardElement(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	seedMaintenanceAdmin(t, fixture.db)
	router := targetTrackRouter(fixture, maintenanceClaims(17))

	response := targetTrackCall(t, router, http.MethodPost, fixture.channel.ID, `{"mode":"auto","idempotencyKey":"tt-auto-1"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	data := decodeTargetTrackData(t, response)
	require.Equal(t, "sent", data["status"])
	require.Equal(t, false, data["responseRequired"], "无应答命令：写成 true 会把成功报成「结果未知」")
	require.Equal(t, false, data["deviceAcknowledged"])
	require.Equal(t, "target_track", data["action"])

	intent, found := loadTargetTrackIntent(t, fixture.db)
	require.True(t, found)
	require.Equal(t, gbmodels.TargetTrackModeAuto, intent.Mode)
	require.Nil(t, intent.AreaLength)
	require.Equal(t, "", intent.DeviceID2)

	// 响应里必须带上落库后的意图，前端不必再打一次读接口（也就不会闪上一条）。
	returnedIntent, ok := data["intent"].(map[string]interface{})
	require.True(t, ok, "下发响应必须带回新的意图：%v", data)
	require.Equal(t, "Auto", returnedIntent["mode"])

	// 再读一次：读到的就是刚下发的那条。
	response = targetTrackCall(t, router, http.MethodGet, fixture.channel.ID, "")
	readData := decodeTargetTrackData(t, response)
	readIntent, ok := readData["intent"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "Auto", readIntent["mode"])
}

// 手动跟踪必须带框；缺框是调用方 bug，在**进 operation 表之前**就该被挡住。
func TestSetChannelTargetTrackManualRequiresArea(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	seedMaintenanceAdmin(t, fixture.db)
	router := targetTrackRouter(fixture, maintenanceClaims(17))

	response := targetTrackCall(t, router, http.MethodPost, fixture.channel.ID, `{"mode":"Manual"}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "必须携带 TargetArea")
	require.Zero(t, fixture.sender.calls, "参数不合法时不能发出任何报文")

	var operations int64
	require.NoError(t, fixture.db.Model(&gbmodels.GbPTZOperation{}).Count(&operations).Error)
	require.Zero(t, operations, "被拒的请求不得落库成 operation")

	_, found := loadTargetTrackIntent(t, fixture.db)
	require.False(t, found, "被拒的请求不得留下意图")
}

func TestSetChannelTargetTrackManualKeepsWindowAndBox(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	seedMaintenanceAdmin(t, fixture.db)
	router := targetTrackRouter(fixture, maintenanceClaims(17))

	body := `{"mode":"Manual","deviceId2":"34020000001310000009",
		"area":{"length":1280,"width":720,"midPointX":640,"midPointY":360,"lengthX":200,"lengthY":120},
		"idempotencyKey":"tt-manual-1"}`
	response := targetTrackCall(t, router, http.MethodPost, fixture.channel.ID, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	intent, found := loadTargetTrackIntent(t, fixture.db)
	require.True(t, found)
	require.Equal(t, gbmodels.TargetTrackModeManual, intent.Mode)
	require.Equal(t, "34020000001310000009", intent.DeviceID2)
	require.NotNil(t, intent.AreaLength)
	require.Equal(t, 1280, *intent.AreaLength)
	require.Equal(t, 200, *intent.AreaLengthX)
	// 报文里必须原样带着全景窗口尺寸与框 —— 少了任何一项设备都做不了比例换算。
	require.Contains(t, intent.RawSummary, "<TargetArea><Length>1280</Length>")
	require.Contains(t, intent.RawSummary, "<MidPointX>640</MidPointX>")
	require.Contains(t, intent.RawSummary, "<DeviceID2>34020000001310000009</DeviceID2>")
}

// 停止跟踪：意图要变成 Stop，并且**六列框选坐标必须被清成 NULL**。
// 留着旧框的表现是界面上"已停止"却还画着一个跟踪框。
func TestSetChannelTargetTrackStopClearsArea(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	seedMaintenanceAdmin(t, fixture.db)
	router := targetTrackRouter(fixture, maintenanceClaims(17))

	area := `{"length":1280,"width":720,"midPointX":640,"midPointY":360,"lengthX":200,"lengthY":120}`
	require.Equal(t, http.StatusOK, targetTrackCall(t, router, http.MethodPost, fixture.channel.ID,
		`{"mode":"Manual","area":`+area+`,"idempotencyKey":"tt-m"}`).Code)
	require.Equal(t, http.StatusOK, targetTrackCall(t, router, http.MethodPost, fixture.channel.ID,
		`{"mode":"Stop","idempotencyKey":"tt-s"}`).Code)

	intent, found := loadTargetTrackIntent(t, fixture.db)
	require.True(t, found)
	require.Equal(t, gbmodels.TargetTrackModeStop, intent.Mode)
	require.Nil(t, intent.AreaLength)
	require.Nil(t, intent.AreaMidPointX)
	require.Nil(t, intent.AreaLengthY)
}

func TestSetChannelTargetTrackRejectsUnknownModeAndStopWithArea(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	seedMaintenanceAdmin(t, fixture.db)
	router := targetTrackRouter(fixture, maintenanceClaims(17))

	response := targetTrackCall(t, router, http.MethodPost, fixture.channel.ID, `{"mode":"Follow"}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "不是 Auto/Manual/Stop")

	// Stop 带框是自相矛盾的指令，别替设备猜意图。
	response = targetTrackCall(t, router, http.MethodPost, fixture.channel.ID,
		`{"mode":"Stop","area":{"length":1280,"width":720,"midPointX":640,"midPointY":360,"lengthX":200,"lengthY":120}}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "不应携带 TargetArea")

	require.Zero(t, fixture.sender.calls)
}

// 窗口尺寸为 0 必须被拒：设备无法用 0 做比例换算，而且它会顺带掩盖"前端没算画面尺寸"这个 bug。
func TestSetChannelTargetTrackRejectsZeroWindowSize(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	seedMaintenanceAdmin(t, fixture.db)
	router := targetTrackRouter(fixture, maintenanceClaims(17))

	response := targetTrackCall(t, router, http.MethodPost, fixture.channel.ID,
		`{"mode":"Manual","area":{"length":0,"width":720,"midPointX":10,"midPointY":10,"lengthX":20,"lengthY":20}}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "全景播放窗口尺寸必须为正数")
	require.Zero(t, fixture.sender.calls)
}

// 匿名调用在**写侧**必须被挡在第一步：它会真的操作设备。
//
// ⛔ 这里只测写侧，是刻意的。读接口（GET）不在控制器里复检登录，与
// `GetChannelStorageCards` / `GetPTZState` 那一族一致 —— 那道门由路由的 JWT 中间件把，
// 而其契约由 `routes/device_target_track_routes_test.go`（注册形态）覆盖。
// 在控制器里再判一次会造出"同一批读接口里只有它特殊"的假差异，
// 反而让人以为别的读接口也自己检查过。
func TestSetChannelTargetTrackRejectsAnonymousCaller(t *testing.T) {
	fixture := newTargetTrackFixture(t)
	router := targetTrackRouter(fixture) // 无 claims

	response := targetTrackCall(t, router, http.MethodPost, fixture.channel.ID, `{"mode":"Auto"}`)
	require.Contains(t, response.Body.String(), "未登录")
	require.Zero(t, fixture.sender.calls, "匿名请求绝不能真的给设备发指令")

	var operations int64
	require.NoError(t, fixture.db.Model(&gbmodels.GbPTZOperation{}).Count(&operations).Error)
	require.Zero(t, operations, "被拒的请求不得落库成 operation")

	_, found := loadTargetTrackIntent(t, fixture.db)
	require.False(t, found, "被拒的请求不得留下意图")
}
