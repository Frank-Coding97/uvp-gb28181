package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func newPlaybackSchemeRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbPlaybackScheme{}, &gbmodels.GbPlaybackSchemeSlot{},
		&gbmodels.GbChannel{}, &gbmodels.GbDevice{},
		&basemodels.User{}, &basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{},
	))
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "owner", Password: "x", DeptID: 10}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 8}, Username: "other", Password: "x", DeptID: 20}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "34020000002000000001", Name: "东区设备", OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "34020000002000000001", ChannelID: "34020000001320000001", Name: "东门", Status: gbmodels.ChannelStatusOnline, AudioEnabled: true, OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "34020000002000000001", ChannelID: "34020000001320000002", Name: "西门", Status: gbmodels.ChannelStatusOffline, OwnerDeptID: 10}).Error)

	ctrl := NewPlaybackSchemeController(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(func(c *gin.Context) {
		userID, _ := strconv.ParseUint(c.GetHeader("X-Test-User"), 10, 64)
		if userID == 0 {
			userID = 7
		}
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: uint(userID)}})
		c.Next()
	})
	router.GET("/schemes", ctrl.List)
	router.GET("/schemes/:id", ctrl.Detail)
	router.POST("/schemes", ctrl.Create)
	router.PATCH("/schemes/:id", ctrl.Rename)
	router.PUT("/schemes/:id/layout", ctrl.ReplaceLayout)
	router.DELETE("/schemes/:id", ctrl.Delete)
	return router, db
}

func playbackSchemeRequest(t *testing.T, router *gin.Engine, method, path, body string, userID uint) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Test-User", strconv.Itoa(int(userID)))
	router.ServeHTTP(recorder, request)
	return recorder
}

func playbackSchemeData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	data, _ := payload["data"].(map[string]any)
	return data
}

func TestPlaybackSchemeControllerCRUDAndIsolation(t *testing.T) {
	router, db := newPlaybackSchemeRouter(t)

	empty := playbackSchemeRequest(t, router, http.MethodPost, "/schemes", `{"name":"空方案","layoutSize":4,"slots":[]}`, 7)
	require.Equal(t, http.StatusBadRequest, empty.Code)
	require.Contains(t, empty.Body.String(), "SCHEME_EMPTY")

	created := playbackSchemeRequest(t, router, http.MethodPost, "/schemes", `{
		"name":" 值班方案 ","layoutSize":4,"slots":[
			{"slotIndex":2,"deviceCode":"34020000002000000001","channelCode":"34020000001320000002"},
			{"slotIndex":0,"deviceCode":"34020000002000000001","channelCode":"34020000001320000001"}
		]}`, 7)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	createdData := playbackSchemeData(t, created)
	schemeID := uint(createdData["id"].(float64))
	require.Equal(t, "值班方案", createdData["name"])

	var stored gbmodels.GbPlaybackScheme
	require.NoError(t, db.First(&stored, schemeID).Error)
	require.Equal(t, uint(7), stored.OwnerUserID)
	require.Equal(t, uint(10), stored.OwnerDeptID)
	require.Equal(t, 2, stored.SlotCount)
	var slots []gbmodels.GbPlaybackSchemeSlot
	require.NoError(t, db.Where("scheme_id = ?", schemeID).Order("slot_index").Find(&slots).Error)
	require.Equal(t, []string{"东门", "西门"}, []string{slots[0].ChannelNameSnapshot, slots[1].ChannelNameSnapshot})

	duplicate := playbackSchemeRequest(t, router, http.MethodPost, "/schemes", `{"name":"值班方案","layoutSize":4,"slots":[{"slotIndex":0,"deviceCode":"34020000002000000001","channelCode":"34020000001320000001"}]}`, 7)
	require.Equal(t, http.StatusConflict, duplicate.Code)
	require.Contains(t, duplicate.Body.String(), "SCHEME_NAME_CONFLICT")

	otherUserSameName := playbackSchemeRequest(t, router, http.MethodPost, "/schemes", `{"name":"值班方案","layoutSize":4,"slots":[{"slotIndex":0,"deviceCode":"34020000002000000001","channelCode":"34020000001320000001"}]}`, 8)
	require.Equal(t, http.StatusBadRequest, otherUserSameName.Code)
	require.Contains(t, otherUserSameName.Body.String(), "SCHEME_CHANNEL_NOT_VISIBLE")

	detail := playbackSchemeRequest(t, router, http.MethodGet, "/schemes/"+strconv.Itoa(int(schemeID)), "", 7)
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	require.Contains(t, detail.Body.String(), `"slotIndex":0`)
	require.Contains(t, detail.Body.String(), `"availability":"available"`)
	require.Contains(t, detail.Body.String(), `"availability":"offline"`)

	forbidden := playbackSchemeRequest(t, router, http.MethodGet, "/schemes/"+strconv.Itoa(int(schemeID)), "", 8)
	require.Equal(t, http.StatusNotFound, forbidden.Code)
	require.Contains(t, forbidden.Body.String(), "SCHEME_NOT_FOUND")

	failedReplace := playbackSchemeRequest(t, router, http.MethodPut, "/schemes/"+strconv.Itoa(int(schemeID))+"/layout", `{"layoutSize":1,"slots":[{"slotIndex":4,"deviceCode":"34020000002000000001","channelCode":"34020000001320000001"}]}`, 7)
	require.Equal(t, http.StatusBadRequest, failedReplace.Code)
	require.Contains(t, failedReplace.Body.String(), "SCHEME_LAYOUT_INVALID")
	require.NoError(t, db.First(&stored, schemeID).Error)
	require.Equal(t, int16(4), stored.LayoutSize)
	require.NoError(t, db.Where("scheme_id = ?", schemeID).Find(&slots).Error)
	require.Len(t, slots, 2)

	deleted := playbackSchemeRequest(t, router, http.MethodDelete, "/schemes/"+strconv.Itoa(int(schemeID)), "", 7)
	require.Equal(t, http.StatusOK, deleted.Code, deleted.Body.String())
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPlaybackSchemeSlot{}).Where("scheme_id = ?", schemeID).Count(&count).Error)
	require.Zero(t, count)
}
