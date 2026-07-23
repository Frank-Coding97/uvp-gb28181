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
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

type resourcePTZSender struct {
	mu     sync.Mutex
	bodies []string
	err    error
}

type cancellingResourcePTZSender struct{ cancel context.CancelFunc }

func (s cancellingResourcePTZSender) SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
	s.cancel()
	return uac.TrackedMessageResult{}, context.DeadlineExceeded
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
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{},
		&gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbPTZState{},
	))
	device := &gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline, PTZType: 1}
	require.NoError(t, db.Create(channel).Error)
	sender := &resourcePTZSender{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetPTZService(ptz.NewService(db, sender, time.Now))
	return controller, db, channel, sender
}

func TestDeviceMgmt_CreatePTZPreset_TriggersPresetQueryReconcile(t *testing.T) {
	// 老板选的落库策略:乐观入库 + 保存后自动查一次。
	// 断言 preset_set SIP 下发后,后台会异步下发一次 PresetQuery 对账。
	controller, _, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.POST("/channel/:id/ptz/presets", controller.CreatePTZPreset)

	req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/ptz/presets",
		strings.NewReader(`{"name":"入口","presetId":2}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 等一小会让 async goroutine 触发,再对 sender.bodies 快照。
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		sender.mu.Lock()
		count := len(sender.bodies)
		sender.mu.Unlock()
		if count >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sender.mu.Lock()
	bodies := append([]string(nil), sender.bodies...)
	sender.mu.Unlock()
	require.GreaterOrEqual(t, len(bodies), 2, "应包含 preset_set + 异步 PresetQuery")
	foundSet, foundQuery := false, false
	for _, body := range bodies {
		if strings.Contains(body, "<PTZCmd>") {
			foundSet = true
		}
		if strings.Contains(body, "<CmdType>PresetQuery</CmdType>") {
			foundQuery = true
		}
	}
	require.True(t, foundSet, "缺少 preset_set 主命令 SIP")
	require.True(t, foundQuery, "缺少异步 PresetQuery 对账 SIP")
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
	router.POST("/channel/:id/ptz/aux", controller.ControlPTZAux)
	router.PATCH("/channel/:id/ptz/home-position", controller.UpdatePTZHomePosition)

	tests := []struct {
		method string
		path   string
		body   string
		want   string
	}{
		{http.MethodDelete, "/channel/" + uintStr(channel.ID) + "/ptz/presets/3", "", "A50F01830003003B"},
		{http.MethodPost, "/channel/" + uintStr(channel.ID) + "/ptz/cruise", `{"action":"start","trackId":4}`, "A50F018804000041"},
		{http.MethodPost, "/channel/" + uintStr(channel.ID) + "/ptz/aux", `{"action":"on","auxiliaryId":7}`, "A50F018C07000048"},
		{http.MethodPatch, "/channel/" + uintStr(channel.ID) + "/ptz/home-position", `{"enabled":true,"resetTime":30,"presetId":3}`, "<HomePosition>"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		if tt.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, tt.path+": "+w.Body.String())
		require.Contains(t, w.Body.String(), "operationId")
		// 预置位改动会异步下发 PresetQuery 对账,顺序不定;检查任一 body 匹配即可。
		sender.mu.Lock()
		bodies := append([]string(nil), sender.bodies...)
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

func TestDeviceMgmt_PTZRefreshTimeoutKeepsOldCacheStale(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	require.NoError(t, db.Create(&gbmodels.GbPTZPreset{ChannelID: channel.ID, DeviceID: 1, PresetID: 1, Status: gbmodels.PTZPresetActive, UpdatedAt: time.Now().Add(-2 * time.Minute)}).Error)
	ctx, cancel := context.WithCancel(context.Background())
	controller.SetPTZService(ptz.NewService(db, cancellingResourcePTZSender{cancel: cancel}, time.Now))
	router := gin.New()
	router.GET("/channel/:id/ptz/presets", controller.ListPTZPresets)
	req := httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/ptz/presets?refresh=true", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"freshness":"stale"`)
	require.Contains(t, w.Body.String(), "refreshOperationId")
	require.Contains(t, w.Body.String(), "refreshError")
}

func TestDeviceMgmt_PTZRefreshRejectsOtherDepartment(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	require.NoError(t, db.AutoMigrate(&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{}))
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
