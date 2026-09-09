package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

type workRecordingAPIFake struct{ starts, stops, gets int }

func (f *workRecordingAPIFake) Start(_ context.Context, _ uint, r workrecording.StartRequest, _ int) (workrecording.Snapshot, error) {
	f.starts++
	return workrecording.Snapshot{ID: "job", ChannelID: r.ChannelID, State: workrecording.StateRecording, Version: 1}, nil
}
func (f *workRecordingAPIFake) Stop(context.Context, string) (workrecording.Snapshot, error) {
	f.stops++
	return workrecording.Snapshot{State: workrecording.StateStopped}, nil
}
func (f *workRecordingAPIFake) Get(context.Context, string) (workrecording.Snapshot, error) {
	f.gets++
	return workrecording.Snapshot{State: workrecording.StateRecording}, nil
}
func workRecordingRouter(t *testing.T, db *gorm.DB, api gbcontrollers.WorkRecordingAPI, actor uint) *gin.Engine {
	t.Helper()
	c := gbcontrollers.NewWorkRecordingController(api)
	c.SetDB(func() *gorm.DB { return db })
	r := gin.New()
	r.Use(gin.Recovery(), withClaims(actor))
	r.POST("/work-recordings", c.Start)
	r.POST("/work-recordings/:id/stop", c.Stop)
	r.GET("/work-recordings/status", c.Status)
	r.GET("/work-recordings", c.List)
	r.GET("/work-recordings/:id/form", c.Form)
	r.PUT("/work-recordings/:id/form", c.SaveForm)
	return r
}
func workRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(response, request)
	return response
}
func TestWorkRecordingControllerStartScopesAndRejectsExtraParameters(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	own := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	foreign := models.GbChannel{DeviceID: "theirs", ChannelID: "foreign", OwnerDeptID: 20}
	require.NoError(t, db.Create(&own).Error)
	require.NoError(t, db.Create(&foreign).Error)
	api := &workRecordingAPIFake{}
	r := workRecordingRouter(t, db, api, 100)
	response := workRequest(r, http.MethodPost, "/work-recordings", `{"channelId":`+uintStr(own.ID)+`,"requestId":"one"}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, api.starts)
	response = workRequest(r, http.MethodPost, "/work-recordings", `{"channelId":`+uintStr(foreign.ID)+`,"requestId":"two"}`)
	require.Equal(t, http.StatusNotFound, response.Code)
	response = workRequest(r, http.MethodPost, "/work-recordings", `{"channelId":`+uintStr(own.ID)+`,"requestId":"three","path":"/arbitrary"}`)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, 1, api.starts)
	response = workRequest(workRecordingRouter(t, db, api, 0), http.MethodPost, "/work-recordings", `{"channelId":1,"requestId":"four"}`)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, 1, api.starts)
}
func TestWorkRecordingControllerStopRequiresInitiatorAndChannelVisibility(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}))
	channel := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	job := models.GbWorkRecording{ID: "8d9b6784-df9a-4486-84ce-3e6eedb18280", ChannelID: channel.ID, CreatedBy: 200, RequestID: "one", State: "recording", DesiredAction: "start", Version: 1, FormJSON: "{}"}
	require.NoError(t, db.Create(&job).Error)
	api := &workRecordingAPIFake{}
	r := workRecordingRouter(t, db, api, 100)
	response := workRequest(r, http.MethodPost, "/work-recordings/"+job.ID+"/stop", "")
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Zero(t, api.stops)
	require.NoError(t, db.Model(&job).Update("created_by", 100).Error)
	response = workRequest(r, http.MethodPost, "/work-recordings/"+job.ID+"/stop", "")
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, api.stops)
}
func TestWorkRecordingControllerBulkRejectsUnauthorizedAndOversizedInputs(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	api := &workRecordingAPIFake{}
	r := workRecordingRouter(t, db, api, 100)
	response := workRequest(r, http.MethodGet, "/work-recordings/status?channelIds="+uintStr(channel.ID)+",999", "")
	require.Equal(t, http.StatusNotFound, response.Code)
	response = workRequest(r, http.MethodGet, "/work-recordings/status?channelIds=0", "")
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = workRequest(r, http.MethodGet, "/work-recordings/status?channelIds=abc", "")
	require.Equal(t, http.StatusBadRequest, response.Code)
	response = workRequest(r, http.MethodGet, "/work-recordings/status?channelIds="+strings.Repeat("1,", 64)+"1", "")
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Zero(t, api.gets)
}

func TestWorkRecordingControllerDoesNotExposeMismatchedClaimJob(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbRecorderClaim{}))
	channel := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&models.GbRecorderClaim{ResourceKey: workrecording.ChannelResource(channel.ID), ChannelID: channel.ID, OwnerKind: workrecording.OwnerWork, OwnerID: "wrong-job", State: workrecording.StateRecording, Version: 1}).Error)
	api := &workRecordingAPIFake{}
	response := workRequest(workRecordingRouter(t, db, api, 100), http.MethodGet, "/work-recordings/status?channelIds="+uintStr(channel.ID), "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Body.String(), "wrong-job")
}

type workHTTPRecorder struct {
	active        bool
	starts, stops int
	root          string
}

func (m *workHTTPRecorder) StartRecord(context.Context, string, string, string, int) error {
	panic("work must use its directory")
}
func (m *workHTTPRecorder) StartMP4RecordInDirectory(_ context.Context, _, _, _ string, _ int, root string) error {
	m.active = true
	m.starts++
	m.root = root
	return nil
}
func (m *workHTTPRecorder) StopRecord(context.Context, string, string, string) error {
	m.active = false
	m.stops++
	return nil
}
func (m *workHTTPRecorder) IsRecording(context.Context, string, string, string) (bool, error) {
	return m.active, nil
}
func TestWorkRecordingHTTPStartRefreshStopAndNextJob(t *testing.T) {
	db := newScopedDeviceDB(t)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("production:not_found", func(tx *gorm.DB) { tx.Statement.RaiseErrorOnNotFound = false }))
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecording{}, &models.GbRecorderClaim{}))
	channel := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	media := &workHTTPRecorder{}
	recorder := workrecording.NewRecorder(workrecording.NewClaims(db), func(context.Context, workrecording.MediaTarget) (func(), error) { return func() {}, nil }, func(workrecording.MediaTarget) (workrecording.MP4Client, error) { return media, nil })
	service := workrecording.NewService(db, recorder, func(_ context.Context, id uint, job string) (workrecording.PreparedRecording, error) {
		return workrecording.PreparedRecording{Target: workrecording.MediaTarget{NodeID: 1, VHost: "__defaultVhost__", App: "rtp", Stream: "ours", Generation: 1, RecordingRoot: "/node/work-recordings/" + job}, Release: func() {}}, nil
	})
	router := workRecordingRouter(t, db, service, 100)
	startBody := `{"channelId":` + uintStr(channel.ID) + `,"requestId":"first"}`
	response := workRequest(router, "POST", "/work-recordings", startBody)
	require.Equal(t, 200, response.Code, response.Body.String())
	var started struct {
		Data workrecording.Snapshot `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &started))
	require.NotEmpty(t, started.Data.ID)
	formPath := "/work-recordings/" + started.Data.ID + "/form"
	response = workRequest(router, "GET", formPath, "")
	require.Equal(t, 200, response.Code, response.Body.String())
	response = workRequest(router, "PUT", formPath, `{"formVersion":0,"form":{"projectName":"集成测试项目","workPersonnel":["张三","李四"]}}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "集成测试项目")
	require.True(t, media.active)
	require.Zero(t, media.stops)
	response = workRequest(router, "POST", "/work-recordings", startBody)
	require.Equal(t, 200, response.Code)
	require.Equal(t, 1, media.starts)
	// Recreate the HTTP controller as a new page would, querying durable state.
	refreshed := workRecordingRouter(t, db, service, 100)
	response = workRequest(refreshed, "GET", "/work-recordings/status?channelIds="+uintStr(channel.ID), "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), started.Data.ID)
	require.True(t, media.active)
	response = workRequest(refreshed, "POST", "/work-recordings/"+started.Data.ID+"/stop", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.False(t, media.active)
	response = workRequest(refreshed, "POST", "/work-recordings", strings.Replace(startBody, "first", "second", 1))
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, 2, media.starts)
	nextRoot := media.root
	response = workRequest(refreshed, "POST", "/work-recordings/"+started.Data.ID+"/stop", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, 1, media.stops)
	require.True(t, media.active)
	require.Equal(t, nextRoot, media.root)
	response = workRequest(refreshed, "GET", formPath, "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "集成测试项目")
	require.Contains(t, response.Body.String(), started.Data.ID)
	response = workRequest(refreshed, "GET", "/work-recordings?page=1&pageSize=10", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), started.Data.ID)
}

func TestWorkRecordingControllerProductionNotFoundHook(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.AutoMigrate(&models.GbRecorderClaim{}, &models.GbWorkRecording{}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("production:not_found", func(tx *gorm.DB) { tx.Statement.RaiseErrorOnNotFound = false }))
	channel := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	api := &workRecordingAPIFake{}
	router := workRecordingRouter(t, db, api, 100)
	response := workRequest(router, "GET", "/work-recordings/status?channelIds="+uintStr(channel.ID), "")
	require.Equal(t, 200, response.Code)
	var result struct {
		Data []workrecording.Snapshot `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Len(t, result.Data, 1)
	require.Equal(t, workrecording.StateIdle, result.Data[0].State)
	response = workRequest(router, "POST", "/work-recordings", `{"channelId":999999,"requestId":"missing"}`)
	require.Equal(t, 404, response.Code)
	require.Zero(t, api.starts)
	response = workRequest(router, "POST", "/work-recordings/missing/stop", "")
	require.Equal(t, 404, response.Code)
	require.Zero(t, api.stops)
}
