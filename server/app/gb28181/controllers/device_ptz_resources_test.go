package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

type resourcePTZSender struct {
	mu     sync.Mutex
	bodies []string
	err    error
}

type runtimeDeviceControlSender struct {
	mu    sync.Mutex
	calls int
}

func (s *runtimeDeviceControlSender) SendMessage(context.Context, string, string, string, []byte) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return nil
}

func (s *runtimeDeviceControlSender) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestPTZServiceReloadAccessorAndSenderAreRaceFree(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controller, db, channel, trackedSender := newPTZResourceController(t)
	service := mustPTZService(t, db, trackedSender)
	fallbackSender := &runtimeDeviceControlSender{}
	controller.SetPTZRuntime(fallbackSender, service)
	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			controller.SetPTZRuntime(nil, nil)
			controller.SetPTZRuntime(fallbackSender, service)
		}
	}()
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 100; i++ {
				func() {
					defer func() { _ = recover() }() // Common.FailAndAbort uses panic as control flow.
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					ctx.Params = gin.Params{{Key: "id", Value: uintStr(channel.ID)}}
					ctx.Request = httptest.NewRequest(http.MethodPost, "/device-mgmt/channel/"+uintStr(channel.ID)+"/ptz", strings.NewReader(`{"action":"left","speed":1}`))
					ctx.Request.Header.Set("Content-Type", "application/json")
					controller.ControlPTZ(ctx)
				}()
			}
		}()
	}
	close(start)
	wg.Wait()
	require.Zero(t, fallbackSender.Calls(), "原子 runtime 快照不得暴露 sender-only 代际")
}

func (s *resourcePTZSender) SendMessageTracked(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	s.bodies = append(s.bodies, string(body))
	s.mu.Unlock()
	if s.err != nil {
		return uac.TrackedMessageResult{}, s.err
	}
	return uac.TrackedMessageResult{CallID: "call", CSeq: "1", StatusCode: 200}, nil
}

func newPTZResourceController(t *testing.T) (*gbcontrollers.DeviceMgmtController, *gorm.DB, *gbmodels.GbChannel, *resourcePTZSender) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// sqlite in-memory 每个连接独立 db,goroutine 拿新 conn 会看不到已迁移的表;
	// 限制单连接让主流程和异步对账共用同一 db 视图。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceFirmwareUpgrade{},
		&gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbPTZState{},
		&gbmodels.GbAlarmResource{}, &gbmodels.GbAlarmResourceParent{}, &gbmodels.GbAlarmBinding{},
		&gbmodels.GbDeviceGrant{}))
	device := &gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	sender := &resourcePTZSender{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZService(mustPTZService(t, db, sender))
	return controller, db, channel, sender
}

// newPTZResourceController2022 同上,但把设备登记成 **2022**。
//
// 巡航轨迹的**回读**(CruiseTrackListQuery/CruiseTrackQuery)是 2022 新增命令,
// 平台只在设备档案是 2022 时才自动对账(见 reconcileCruiseAsync 的版本门禁),
// 所以"创建/删除巡航会排一次对账查询"这类用例必须用 2022 设备。
//
// ⚠️ 别把默认 fixture 改成 2022:它被几十个用例共用,`EffectiveVersion` 一变
// `SupportsPrecisePTZ()` 等能力也跟着变(精确状态查询的 CmdType 就会换),
// 会连带改掉一批与本主题无关的用例的期望值。
func newPTZResourceController2022(t *testing.T) (*gbcontrollers.DeviceMgmtController, *gorm.DB, *gbmodels.GbChannel, *resourcePTZSender) {
	t.Helper()
	controller, db, channel, sender := newPTZResourceController(t)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).
		Where("device_id = ?", channel.DeviceID).
		Update("effective_version", "2022").Error)
	return controller, db, channel, sender
}

func TestDeviceMgmt_CreatePTZPreset_TriggersPresetQueryReconcile(t *testing.T) {
	// 老板选的落库策略:乐观入库 + 保存后自动查一次。
	// 查询属于 durable operation,由生产 scheduler 负责后续 SIP 下发。
	controller, db, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.POST("/channel/:id/ptz/presets", controller.CreatePTZPreset)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/presets",
		strings.NewReader(`{"name":"入口","presetId":2}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 等后台 goroutine 创建 queued PresetQuery operation。
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var count int64
		_ = db.Model(&gbmodels.GbPTZOperation{}).Where("action = ?", "refresh_presets").Count(&count).Error
		if count == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var reconcile gbmodels.GbPTZOperation
	require.NoError(t, db.Where("action = ?", "refresh_presets").First(&reconcile).Error)
	require.True(t, reconcile.ResponseRequired)
	require.Equal(t, 3, reconcile.MaxAttempts)
	require.Equal(t, gbmodels.PTZOperationQueued, reconcile.Status)
	sender.mu.Lock()
	require.Len(t, sender.bodies, 1, "主 preset_set 仍应立即发送,查询由 scheduler 发送")
	sender.mu.Unlock()
}

func TestDeviceMgmt_CreatePTZPreset_AllowsUnreportedPTZType(t *testing.T) {
	// 回归:预置位/巡航/辅助/看守位这些资源类命令共享 loadPTZTarget 工厂;
	// 上一轮修 ControlPTZ 时漏了这条路径,PTZType=0(未上报)仍被 service 层 validateTarget 拦
	controller, db, channel, sender := newPTZResourceController(t)
	channel.PTZType = 0
	require.NoError(t, db.Save(channel).Error)

	router := gin.New()
	router.POST("/channel/:id/ptz/presets", controller.CreatePTZPreset)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/presets",
		strings.NewReader(`{"name":"入口","presetId":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "未上报")
	require.NotEmpty(t, sender.bodies, "SIP 应下发到设备")
}

func TestDeviceMgmt_PTZResourceWriteEndpointsReturnOperations(t *testing.T) {
	controller, _, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.DELETE("/channel/:id/ptz/presets/:presetId", controller.DeletePTZPreset)
	router.POST("/channel/:id/ptz/cruise", controller.ControlPTZCruise)
	router.PATCH("/channel/:id/ptz/home-position", controller.UpdatePTZHomePosition)

	tests := []struct {
		method string
		path   string
		body   string
		want   string
	}{
		{http.MethodDelete, "/channel/" + uintStr(channel.ID) + "/ptz/presets/3", "", "A50F01830003003B"},
		{http.MethodPost, "/channel/" + uintStr(channel.ID) + "/ptz/cruise", `{"action":"start","trackId":4}`, "A50F018804000041"},
		{http.MethodPost, "/channel/" + uintStr(channel.ID) + "/ptz/cruise", `{"action":"start","trackId":0}`, "A50F01880000003D"},
		{http.MethodPost, "/channel/" + uintStr(channel.ID) + "/ptz/cruise", `{"action":"stop","trackId":0}`, "A50F0100000000B5"},
		{http.MethodPost, "/channel/" + uintStr(channel.ID) + "/ptz/cruise", `{"action":"delete","trackId":0}`, "A50F01850000003A"},
	}
	for _, tt := range tests {
		sender.mu.Lock()
		before := len(sender.bodies)
		sender.mu.Unlock()

		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		if tt.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, tt.path+": "+w.Body.String())
		require.Contains(t, w.Body.String(), "operationId")
		// 预置位改动会异步下发 PresetQuery 对账,顺序不定;只检查本次请求新增的报文,
		// 避免前面用例的历史报文掩盖当前请求实际下发的参数。
		sender.mu.Lock()
		bodies := append([]string(nil), sender.bodies[before:]...)
		sender.mu.Unlock()
		found := false
		for _, b := range bodies {
			if strings.Contains(b, tt.want) {
				found = true
				break
			}
		}
		require.True(t, found, "%s: want %q in sent bodies", tt.path, tt.want)
	}
}

func TestDeviceMgmt_ControlPTZExtendedRejectsAuxiliaryActions(t *testing.T) {
	controller, _, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/ptz/extended", controller.ControlPTZExtended)

	for _, action := range []string{"aux_on", "aux_off"} {
		t.Run(action, func(t *testing.T) {
			sender.mu.Lock()
			before := len(sender.bodies)
			sender.mu.Unlock()
			request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/extended",
				strings.NewReader(`{"action":"`+action+`","id":7}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Contains(t, response.Body.String(), `"code":1`)
			sender.mu.Lock()
			require.Len(t, sender.bodies, before)
			sender.mu.Unlock()
		})
	}
}

func TestDeviceMgmt_ControlPTZExtendedScanActions(t *testing.T) {
	controller, _, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/ptz/extended", controller.ControlPTZExtended)

	tests := []struct {
		name string
		body string
		want string
	}{
		{"start", `{"action":"scan_start","id":1}`, "A50F01890100003F"},
		// 89H 一个码管三件事,子动作在字节6(00 开始 / 01 左边界 / 02 右边界)。
		{"left bound", `{"action":"scan_set_left","id":1}`, "A50F018901010040"},
		{"right bound", `{"action":"scan_set_right","id":1}`, "A50F018901020041"},
		// 8AH 的速度是 12 位:低 8 位进字节6,高 4 位进字节7 高半字节。
		{"speed", `{"action":"scan_set_speed","id":1,"value":120}`, "A50F018A017800B8"},
		{"stop", `{"action":"scan_stop","id":1}`, "A50F0100000000B5"},
		// 扫描组号 0 是合法组(00H-FFH),与预置位/巡航的「0 = 哨兵」不同。
		{"group zero", `{"action":"scan_start","id":0}`, "A50F01890000003E"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender.mu.Lock()
			before := len(sender.bodies)
			sender.mu.Unlock()
			request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/extended",
				strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "operationId")
			sender.mu.Lock()
			bodies := append([]string(nil), sender.bodies[before:]...)
			sender.mu.Unlock()
			found := false
			for _, b := range bodies {
				if strings.Contains(b, tt.want) {
					found = true
					break
				}
			}
			require.True(t, found, "want %q in sent bodies, got %v", tt.want, bodies)
		})
	}
}

func TestDeviceMgmt_ControlPTZExtendedRejectsScanSpeedOutOfRange(t *testing.T) {
	controller, _, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/ptz/extended", controller.ControlPTZExtended)

	for _, body := range []string{`{"action":"scan_set_speed","id":1}`, `{"action":"scan_set_speed","id":1,"value":4096}`} {
		sender.mu.Lock()
		before := len(sender.bodies)
		sender.mu.Unlock()
		request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/extended", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Contains(t, response.Body.String(), `"code":1`, body)
		sender.mu.Lock()
		require.Len(t, sender.bodies, before, "非法的扫描速度不应下发 SIP")
		sender.mu.Unlock()
	}
}

func TestDeviceMgmt_ControlPTZCruiseRejectsNonStandardPauseAndResume(t *testing.T) {
	controller, _, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/ptz/cruise", controller.ControlPTZCruise)

	for _, action := range []string{"pause", "resume", "continue"} {
		t.Run(action, func(t *testing.T) {
			before := len(sender.bodies)
			req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise",
				strings.NewReader(`{"action":"`+action+`","trackId":0}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Contains(t, w.Body.String(), `"code":1`)
			require.Len(t, sender.bodies, before)
		})
	}
}

func TestDeviceMgmt_DeleteCruiseZeroClearsCacheAndSchedulesReconcile(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController2022(t)
	enabled := true
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: 1, ChannelID: channel.ID, TrackID: 0, Name: "零号轨迹", Enabled: &enabled,
	}).Error)
	router := gin.New()
	router.POST("/channel/:id/ptz/cruise", controller.ControlPTZCruise)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise",
		strings.NewReader(`{"action":"delete","trackId":0}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"action":"cruise_delete_path"`)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).
		Where("channel_id = ? AND track_id = ?", channel.ID, 0).Count(&count).Error)
	require.Zero(t, count)

	var reconcile gbmodels.GbPTZOperation
	require.Eventually(t, func() bool {
		return db.Where("action = ?", "refresh_cruise_tracks").First(&reconcile).Error == nil
	}, 500*time.Millisecond, 10*time.Millisecond)
	require.True(t, reconcile.ResponseRequired)
	require.Equal(t, 3, reconcile.MaxAttempts)
	require.Equal(t, gbmodels.PTZOperationQueued, reconcile.Status)
}

func TestDeviceMgmt_ListPTZPresetsRefreshReturnsFullStaleCacheAndOperation(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	old := time.Now().Add(-2 * time.Minute)
	for i := 1; i <= 20; i++ {
		require.NoError(t, db.Create(&gbmodels.GbPTZPreset{ChannelID: channel.ID, DeviceID: 1, PresetID: i, Name: "P" + strconv.Itoa(i), Status: gbmodels.PTZPresetActive, UpdatedAt: old}).Error)
	}
	router := gin.New()
	router.GET("/channel/:id/ptz/presets", controller.ListPTZPresets)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/presets?refresh=true", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var response struct {
		Data struct {
			List               []gbmodels.GbPTZPreset `json:"list"`
			Freshness          string                 `json:"freshness"`
			RefreshOperationID string                 `json:"refreshOperationId"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Data.List, 20)
	require.Equal(t, string(gbmodels.PTZFreshnessStale), response.Data.Freshness)
	require.NotEmpty(t, response.Data.RefreshOperationID)
}

func TestDeviceMgmt_PTZRefreshWithoutCacheIsUnknown(t *testing.T) {
	controller, _, channel, _ := newPTZResourceController(t)
	router := gin.New()
	router.GET("/channel/:id/ptz/cruise-tracks", controller.ListCruiseTracks)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/cruise-tracks?refresh=true", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"freshness":"unknown"`)
	require.Contains(t, w.Body.String(), "refreshOperationId")
}

func TestDeviceMgmt_PTZRefreshQueuesOperationAndKeepsOldCacheStale(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{ChannelID: channel.ID, DeviceID: 1, PresetID: 1, Status: gbmodels.PTZPresetActive, UpdatedAt: time.Now().Add(-2 * time.Minute)}).Error)
	router := gin.New()
	router.GET("/channel/:id/ptz/presets", controller.ListPTZPresets)
	req := httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/presets?refresh=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"freshness":"stale"`)
	require.Contains(t, w.Body.String(), "refreshOperationId")
	require.NotContains(t, w.Body.String(), "refreshError")
	var operation gbmodels.GbPTZOperation
	require.Eventually(t, func() bool {
		return db.Where("action = ?", "refresh_presets").First(&operation).Error == nil
	}, 500*time.Millisecond, 10*time.Millisecond)
	require.Equal(t, gbmodels.PTZOperationQueued, operation.Status)
	require.Equal(t, 3, operation.MaxAttempts)
}

func TestDeviceMgmt_PTZRefreshRejectsOtherDepartment(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{}, &gbmodels.GbDeviceGrant{}))
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", "D").Update("owner_dept_id", 20).Error)
	require.NoError(t, db.Model(channel).Update("owner_dept_id", 20).Error)
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.GET("/channel/:id/ptz/presets", controller.ListPTZPresets)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/presets?refresh=true", nil))
	require.Contains(t, w.Body.String(), `"code":1`)
	require.NotContains(t, w.Body.String(), "refreshOperationId")
	require.Empty(t, sender.bodies)
}

// failAfterNSender:前 N 次调用正常,第 N+1 次返回 err。用于验证中间失败场景。
type failAfterNSender struct {
	mu        sync.Mutex
	bodies    []string
	failAfter int // 允许通过的次数
	failErr   error
	callCount int
}

func (s *failAfterNSender) SendMessageTracked(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCount++
	s.bodies = append(s.bodies, string(body))
	if s.callCount > s.failAfter {
		return uac.TrackedMessageResult{}, s.failErr
	}
	return uac.TrackedMessageResult{CallID: "call", CSeq: strconv.Itoa(s.callCount), StatusCode: 200}, nil
}

func TestDeviceMgmt_CreateCruiseTrack_DispatchesAddStopSpeedDwellAndReconciles(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController2022(t)
	router := gin.New()
	router.POST("/channel/:id/ptz/cruise/tracks", controller.CreateCruiseTrack)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise/tracks",
		strings.NewReader(`{"trackId":2,"name":"夜间巡逻","speed":256,"dwellSec":5,"stops":[{"presetId":1},{"presetId":3},{"presetId":5}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"sent"`)
	require.Contains(t, w.Body.String(), `"completedStops":3`)
	require.Contains(t, w.Body.String(), `"totalStops":3`)
	require.Contains(t, w.Body.String(), `"reconciled":false`)
	require.Contains(t, w.Body.String(), `"reconcileScheduled":true`)

	// 所有子命令发完后才写待对账记录。「设备未确认」只能由 `source: reconcile-pending`
	// 表达 —— `enabled` 必须留 NULL(= 设备未上报),不能写 false。
	//
	// 写 false 的后果不是"保守一点":`<Enabled>` 在标准元素表里没有依据,真机与模拟器都不回,
	// 没有任何应答能把它翻回来,前端 `enabled !== false` 于是把 tile 永久焊死(操作员点不动
	// 自己刚建好的轨迹)。详见 upsertOptimisticCruise 的注释。
	var track gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Where("channel_id = ? AND track_id = ?", channel.ID, 2).First(&track).Error)
	require.Equal(t, "夜间巡逻", track.Name)
	require.Equal(t, "reconcile-pending", track.RawSummary)
	require.Nil(t, track.Enabled, "平台不得替设备编 enabled;设备未上报就是 NULL")
	require.Contains(t, track.DetailJSON, `"source":"reconcile-pending"`)
	require.Contains(t, track.DetailJSON, `"speed":256`)
	require.Contains(t, track.DetailJSON, `"dwellSec":5`)

	// 列表查询由 durable scheduler 发送,主流程只需保证 operation 已入队。
	var reconcile gbmodels.GbPTZOperation
	require.Eventually(t, func() bool {
		return db.Where("action = ?", "refresh_cruise_tracks").First(&reconcile).Error == nil
	}, 500*time.Millisecond, 10*time.Millisecond)
	require.True(t, reconcile.ResponseRequired)
	require.Equal(t, 3, reconcile.MaxAttempts)
	require.Equal(t, gbmodels.PTZOperationQueued, reconcile.Status)
	sender.mu.Lock()
	bodies := append([]string(nil), sender.bodies...)
	sender.mu.Unlock()
	require.Len(t, bodies, 5, "should dispatch 3 add_stop + speed + dwell; query由scheduler发送")

	// SIP body 是 GB2312 声明的 XML,直接判 hex 子串比走 charset-aware decoder 更简
	joined := strings.Join(bodies, "\n")
	// TrackID=2, PresetID=1/3/5:0x84 + P1=02 + P2=preset → 校验和拼接
	require.Contains(t, joined, "A50F01840201003C", "缺少 0x84 加站 preset=1")
	require.Contains(t, joined, "A50F01840203003E", "缺少 0x84 加站 preset=3")
	require.Contains(t, joined, "A50F018402050040", "缺少 0x84 加站 preset=5")
	require.Contains(t, joined, "A50F01860200104D", "缺少 0x86 速度 256")
	require.Contains(t, joined, "A50F018702050043", "缺少 0x87 停留 5 秒")
}

// 巡航对账的**版本门禁**:2016 设备不发 2022 的巡航回读查询,但控制层照发。
//
// 控制层(PTZCmd 0x84/0x86/0x87/0x88)2016 版就有,`CruiseTrackListQuery` /
// `CruiseTrackQuery` 才是 2022 新增(标准修订说明:9.5.3、A.2.4.10~A.2.4.14)。
// 发给 2016 设备只会烧掉一个 SN 和三次重试预算再超时,操作员那边表现为"卡了一下"。
//
// ⛔ 门禁**只挡自动对账**,不挡操作员点「同步」触发的那次(见 refreshPTZ —— 那条路径
// 刻意无门禁,是真的 2022 设备被误登记成 2016 时唯一的发现手段)。
func TestDeviceMgmt_CreateCruiseTrack_2016SkipsReconcileButStillSendsControl(t *testing.T) {
	// 默认 fixture 的设备没有 effective_version → 按 2016 兼容档案处理
	controller, db, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.POST("/channel/:id/ptz/cruise/tracks", controller.CreateCruiseTrack)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise/tracks",
		strings.NewReader(`{"trackId":3,"name":"legacy","speed":128,"dwellSec":30,"stops":[{"presetId":1},{"presetId":2}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	sender.mu.Lock()
	bodyCount := len(sender.bodies)
	sender.mu.Unlock()
	require.Equal(t, 4, bodyCount, "2 个 add_stop + speed + dwell 必须照常下发:控制层是 2016 能力,门禁不许拦")

	// 给足时间窗再断言"没有"—— 门禁在 reconcileCruiseAsync 里是**同步**早返回,
	// 所以这里不会 flaky;垫一下是为了万一将来门禁挪到 goroutine 里也不会假过。
	time.Sleep(200 * time.Millisecond)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
		Where("action IN ?", []string{"refresh_cruise_tracks", "refresh_cruise_track"}).
		Count(&count).Error)
	require.Zero(t, count, "2016 设备不应收到 2022 的巡航回读查询")
}

func TestDeviceMgmt_CreateCruiseTrack_ReturnsPartialWhenMidStopFails(t *testing.T) {
	// 前 1 次通过(第 1 个 add_stop),第 2 次开始报错。期望 status=add_stop_failed, completedStops=1
	controller, db, channel, _ := newPTZResourceController2022(t)
	failSender := &failAfterNSender{failAfter: 1, failErr: context.DeadlineExceeded}
	controller.SetPTZService(mustPTZService(t, db, failSender))
	router := gin.New()
	router.POST("/channel/:id/ptz/cruise/tracks", controller.CreateCruiseTrack)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise/tracks",
		strings.NewReader(`{"trackId":2,"stops":[{"presetId":1},{"presetId":3},{"presetId":5}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"status":"add_stop_failed"`)
	require.Contains(t, w.Body.String(), `"completedStops":1`)
	require.Contains(t, w.Body.String(), `"code":4200`)
	require.Contains(t, w.Body.String(), `"reconciled":false`)
	require.Contains(t, w.Body.String(), `"reconcileScheduled":true`)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZCruiseTrack{}).
		Where("channel_id = ? AND track_id = ?", channel.ID, 2).Count(&count).Error)
	require.Zero(t, count, "失败批次不能留下 enabled/pending 幽灵轨迹")

	var reconcile gbmodels.GbPTZOperation
	require.Eventually(t, func() bool {
		return db.Where("action = ?", "refresh_cruise_tracks").First(&reconcile).Error == nil
	}, 500*time.Millisecond, 10*time.Millisecond)
	require.True(t, reconcile.ResponseRequired)
	require.Equal(t, gbmodels.PTZOperationQueued, reconcile.Status)
}

func TestDeviceMgmt_CreateCruiseTrack_RecreateKeepsDeviceReportedEnabled(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	enabled := true
	require.NoError(t, db.Create(&gbmodels.GbPTZCruiseTrack{
		DeviceID: 1, ChannelID: channel.ID, TrackID: 3, Name: "旧名字", Enabled: &enabled,
	}).Error)

	router := gin.New()
	router.POST("/channel/:id/ptz/cruise/tracks", controller.CreateCruiseTrack)
	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise/tracks",
		strings.NewReader(`{"trackId":3,"name":"新名字","replaceExisting":true,"stops":[{"presetId":1}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var track gbmodels.GbPTZCruiseTrack
	require.NoError(t, db.Where("channel_id = ? AND track_id = ?", channel.ID, 3).First(&track).Error)
	require.Equal(t, "新名字", track.Name)
	require.Equal(t, "reconcile-pending", track.RawSummary)
	// ⛔ 重建**不碰** `enabled`:它是"设备上报的状态",平台重建自己那条轨迹不该把它抹掉。
	// 原来这里是 `require.False(t, *track.Enabled)`("设备详情回包前不能标 enabled"),
	// 那正是把 tile 焊死的那个写法的另一半 —— 它顺手也能把设备明确报过的 true 清成 false。
	require.NotNil(t, track.Enabled)
	require.True(t, *track.Enabled, "重建轨迹不得覆盖设备已上报的 enabled")
}

func TestDeviceMgmt_CreateCruiseTrack_RejectsInvalidInput(t *testing.T) {
	controller, _, channel, _ := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery()) // FailAndAbort 会 panic(RequestAborted),需 recovery
	router.POST("/channel/:id/ptz/cruise/tracks", controller.CreateCruiseTrack)

	cases := []struct {
		name string
		body string
	}{
		{"缺 stops", `{"trackId":1}`},
		{"trackId 越界", `{"trackId":256,"stops":[{"presetId":1}]}`},
		{"presetId 越界", `{"trackId":1,"stops":[{"presetId":300}]}`},
		{"speed 越界", `{"trackId":1,"speed":4096,"stops":[{"presetId":1}]}`},
		{"dwellSec 越界", `{"trackId":1,"dwellSec":4096,"stops":[{"presetId":1}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise/tracks",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Contains(t, w.Body.String(), `"code":1`, "%s should be rejected: %s", tc.name, w.Body.String())
		})
	}
}

type blockingFirstCruiseSender struct {
	mu           sync.Mutex
	bodies       []string
	firstEntered chan struct{}
	releaseFirst chan struct{}
	once         sync.Once
}

func (s *blockingFirstCruiseSender) SendMessageTracked(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	s.bodies = append(s.bodies, string(body))
	s.mu.Unlock()
	if strings.Contains(string(body), "<PTZCmd>") {
		s.once.Do(func() {
			close(s.firstEntered)
			<-s.releaseFirst
		})
	}
	return uac.TrackedMessageResult{CallID: "call", CSeq: "1", StatusCode: 200}, nil
}

func TestDeviceMgmt_CreateCruiseTrack_SerializesWholeBatchPerChannel(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	sender := &blockingFirstCruiseSender{firstEntered: make(chan struct{}), releaseFirst: make(chan struct{})}
	controller.SetPTZService(mustPTZService(t, db, sender))
	router := gin.New()
	router.POST("/channel/:id/ptz/cruise/tracks", controller.CreateCruiseTrack)

	request := func(body string, done chan<- *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/cruise/tracks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		done <- w
	}
	done := make(chan *httptest.ResponseRecorder, 2)
	go request(`{"trackId":1,"stops":[{"presetId":1},{"presetId":2}]}`, done)
	<-sender.firstEntered
	go request(`{"trackId":2,"stops":[{"presetId":3},{"presetId":4}]}`, done)
	time.Sleep(20 * time.Millisecond) // 让第二批到达通道锁
	close(sender.releaseFirst)
	for i := 0; i < 2; i++ {
		w := <-done
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	sender.mu.Lock()
	bodies := append([]string(nil), sender.bodies...)
	sender.mu.Unlock()
	tracks := make([]string, 0, 4)
	for _, body := range bodies {
		start := strings.Index(body, "<PTZCmd>")
		if start < 0 {
			continue
		}
		start += len("<PTZCmd>")
		if len(body) >= start+16 && body[start+6:start+8] == "84" {
			tracks = append(tracks, body[start+8:start+10])
		}
	}
	require.Equal(t, []string{"01", "01", "02", "02"}, tracks,
		"同一通道的两次 create 不能在子命令之间交错")
}
