package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeRecordQueryService struct {
	mu      sync.Mutex
	result  recordquery.QueryResult
	err     error
	calls   int
	request recordquery.QueryRequest
}

func (f *fakeRecordQueryService) Query(_ context.Context, request recordquery.QueryRequest) (recordquery.QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.request = request
	return f.result, f.err
}

func (f *fakeRecordQueryService) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type recordQueryFixture struct {
	router     *gin.Engine
	db         *gorm.DB
	controller *gbcontrollers.DeviceMgmtController
	service    *fakeRecordQueryService
	device     *gbmodels.GbDevice
	channel    *gbmodels.GbChannel
	audit      *map[string]any
}

func newRecordQueryFixture(t *testing.T, ownerDept uint) recordQueryFixture {
	t.Helper()
	db := newScopedDeviceDB(t)
	app.Response = response.NewResponseHandler()
	seedDeptScopedUser(t, db, 100, 10)
	device := &gbmodels.GbDevice{
		OwnerDeptID: ownerDept, DeviceID: "34020000002000000001", Name: "园区 NVR",
		IP: "192.0.2.10", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline,
	}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{
		OwnerDeptID: ownerDept, DeviceID: device.DeviceID, ChannelID: "34020000001320000001",
		Name: "东门", Status: gbmodels.ChannelStatusOnline, StreamID: "existing-live-stream",
	}
	require.NoError(t, db.Create(channel).Error)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	service := &fakeRecordQueryService{result: recordquery.QueryResult{
		QueryID: "query-1", Status: recordquery.QueryStatusComplete, DeclaredTotal: 1, ReceivedCount: 1,
		StartedAt: time.Date(2026, 8, 4, 10, 0, 0, 0, location), FinishedAt: time.Date(2026, 8, 4, 10, 0, 0, 250000000, location),
		Records: []recordquery.Record{{RecordKey: "opaque-key", RecordInfoItem: manscdp.RecordInfoItem{
			DeviceID: channel.ChannelID, Name: "敏感名称", FilePath: "/private/record/001.dav", Address: "机房地址",
			StartTime: "2026-08-04T08:00:00", EndTime: "2026-08-04T08:30:00", Secrecy: 0,
			Type: "time", RecorderID: "NVR-A", FileSize: int64Ptr(248635904), RecordLocation: "local", StreamNumber: intPtr(0),
		}}},
	}}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetRecordQueryRuntime(service, gbconfig.RecordQueryConfig{
		Timezone: "Asia/Shanghai", Location: location, TimeoutSec: 15, MaxRangeHours: 24,
		MaxActiveQueries: 128, MaxRecordsPerQuery: 20000, ResultTTLSec: 1800,
	}, recordquery.NewMetrics())
	audit := map[string]any(nil)
	fixture := recordQueryFixture{db: db, controller: controller, service: service, device: device, channel: channel, audit: &audit}
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100), func(c *gin.Context) {
		c.Next()
		if value, ok := c.Get("operation_log_sensitive_metadata"); ok {
			*fixture.audit, _ = value.(map[string]any)
		}
	})
	router.GET("/channel/:id/record-query/options", controller.GetRecordQueryOptions)
	router.POST("/channel/:id/record-query", controller.QueryDeviceRecords)
	fixture.router = router
	return fixture
}

func (f recordQueryFixture) request(method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	result := httptest.NewRecorder()
	f.router.ServeHTTP(result, request)
	return result
}

func recordQueryErrorCode(t *testing.T, result *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Data struct {
			ErrorCode string `json:"errorCode"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope), result.Body.String())
	return envelope.Data.ErrorCode
}

func int64Ptr(value int64) *int64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func TestDeviceRecordQueryOptionsAndQueryContract(t *testing.T) {
	fixture := newRecordQueryFixture(t, 10)
	path := "/channel/" + uintStr(fixture.channel.ID) + "/record-query"
	options := fixture.request(http.MethodGet, path+"/options", "")
	require.Equal(t, http.StatusOK, options.Code, options.Body.String())
	require.Contains(t, options.Body.String(), `"timezone":"Asia/Shanghai"`)
	require.Contains(t, options.Body.String(), `"maxRangeHours":24`)
	require.Contains(t, options.Body.String(), `"code":"34020000001320000001"`)

	before := *fixture.channel
	query := fixture.request(http.MethodPost, path, `{"startTime":"2026-08-04T08:00:00","endTime":"2026-08-04T09:00:00","type":"all","secrecy":0,"recorderId":""}`)
	require.Equal(t, http.StatusOK, query.Code, query.Body.String())
	require.Contains(t, query.Body.String(), `"queryId":"query-1"`)
	require.Contains(t, query.Body.String(), `"recordKey":"opaque-key"`)
	require.Contains(t, query.Body.String(), `"startTime":"2026-08-04T08:00:00+08:00"`)
	require.Contains(t, query.Body.String(), `"fileSize":248635904`)
	require.Contains(t, query.Body.String(), `"streamNumber":0`)
	require.Equal(t, 1, fixture.service.callCount())
	require.Equal(t, uint(100), fixture.service.request.OwnerUserID)
	require.Equal(t, fixture.device.DeviceID, fixture.service.request.DeviceCode)
	require.Equal(t, fixture.channel.ChannelID, fixture.service.request.ChannelCode)
	require.Equal(t, "192.0.2.10:5060", fixture.service.request.Destination)
	require.Equal(t, "UDP", fixture.service.request.Transport)
	require.NotNil(t, *fixture.audit)
	auditJSON, err := json.Marshal(*fixture.audit)
	require.NoError(t, err)
	require.NotContains(t, string(auditJSON), "private/record")
	require.NotContains(t, string(auditJSON), "敏感名称")
	require.NotContains(t, string(auditJSON), "机房地址")
	require.Contains(t, string(auditJSON), `"action":"record_query"`)
	require.Contains(t, string(auditJSON), `"result":"complete"`)

	var after gbmodels.GbChannel
	require.NoError(t, fixture.db.First(&after, fixture.channel.ID).Error)
	require.Equal(t, before.StreamID, after.StreamID)
	require.True(t, before.UpdatedAt.Equal(after.UpdatedAt))
}

func TestDeviceRecordQueryHidesMissingAndUnauthorizedTargetsBeforeService(t *testing.T) {
	fixture := newRecordQueryFixture(t, 20)
	body := `{"startTime":"2026-08-04T08:00:00","endTime":"2026-08-04T09:00:00","type":"all","secrecy":0,"recorderId":""}`
	for _, id := range []string{uintStr(fixture.channel.ID), "999999"} {
		result := fixture.request(http.MethodPost, "/channel/"+id+"/record-query", body)
		require.Equal(t, http.StatusNotFound, result.Code, result.Body.String())
		require.Equal(t, "record_query_target_not_found", recordQueryErrorCode(t, result))
	}
	require.Zero(t, fixture.service.callCount())
}

func TestDeviceRecordQueryRejectsInvalidInputBeforeService(t *testing.T) {
	fixture := newRecordQueryFixture(t, 10)
	path := "/channel/" + uintStr(fixture.channel.ID) + "/record-query"
	cases := []string{
		`{}`,
		`{"startTime":"2026-08-04T09:00:00","endTime":"2026-08-04T08:00:00","type":"all","secrecy":0,"recorderId":""}`,
		`{"startTime":"2026-08-03T08:00:00","endTime":"2026-08-04T09:00:00","type":"all","secrecy":0,"recorderId":""}`,
		`{"startTime":"2026-08-04T08:00:00","endTime":"2026-08-04T09:00:00","type":"time","secrecy":0,"recorderId":""}`,
		`{"startTime":"2026-08-04T08:00:00","endTime":"2026-08-04T09:00:00","type":"all","secrecy":0,"recorderId":"","deviceCode":"spoof"}`,
	}
	for _, body := range cases {
		result := fixture.request(http.MethodPost, path, body)
		require.Equal(t, http.StatusUnprocessableEntity, result.Code, result.Body.String())
		require.Equal(t, "record_query_invalid_argument", recordQueryErrorCode(t, result))
	}
	require.Zero(t, fixture.service.callCount())
}

func TestDeviceRecordQueryMapsOfflineAndServiceErrors(t *testing.T) {
	fixture := newRecordQueryFixture(t, 10)
	path := "/channel/" + uintStr(fixture.channel.ID) + "/record-query"
	body := `{"startTime":"2026-08-04T08:00:00","endTime":"2026-08-04T09:00:00","type":"all","secrecy":0,"recorderId":""}`

	fixture.channel.Status = gbmodels.ChannelStatusOffline
	require.NoError(t, fixture.db.Save(fixture.channel).Error)
	offline := fixture.request(http.MethodPost, path, body)
	require.Equal(t, http.StatusConflict, offline.Code, offline.Body.String())
	require.Equal(t, "record_query_device_offline", recordQueryErrorCode(t, offline))
	require.Zero(t, fixture.service.callCount())
	fixture.channel.Status = gbmodels.ChannelStatusOnline
	require.NoError(t, fixture.db.Save(fixture.channel).Error)

	cases := []struct {
		err       error
		status    int
		errorCode string
	}{
		{&recordquery.QueryError{Code: recordquery.ErrorCodeBusy, Err: recordquery.ErrBusy}, http.StatusTooManyRequests, "record_query_busy"},
		{&recordquery.QueryError{Code: recordquery.ErrorCodeSendFailed, Err: recordquery.ErrSendFailed}, http.StatusBadGateway, "record_query_send_failed"},
		{&recordquery.QueryError{Code: recordquery.ErrorCodeUnavailable, Err: recordquery.ErrUnavailable}, http.StatusServiceUnavailable, "record_query_unavailable"},
		{&recordquery.QueryError{Code: recordquery.ErrorCodeTimeout, Err: recordquery.ErrTimeout}, http.StatusGatewayTimeout, "record_query_timeout"},
	}
	for _, test := range cases {
		fixture.service.err = test.err
		result := fixture.request(http.MethodPost, path, body)
		require.Equal(t, test.status, result.Code, result.Body.String())
		require.Equal(t, test.errorCode, recordQueryErrorCode(t, result))
	}

	fixture.controller.SetRecordQueryRuntime(nil, gbconfig.RecordQueryConfig{}, nil)
	unavailable := fixture.request(http.MethodPost, path, body)
	require.Equal(t, http.StatusServiceUnavailable, unavailable.Code, unavailable.Body.String())
	require.Equal(t, "record_query_unavailable", recordQueryErrorCode(t, unavailable))
	require.False(t, errors.Is(fixture.service.err, context.Canceled))
}
