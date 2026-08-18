package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeLoginLogQuery struct {
	items  []service.LoginLogListItem
	total  int64
	detail *service.LoginLogDetail
	err    error
	filter service.LoginLogFilter
}

type fakeLoginLogMutation struct {
	deleteIDs []uint
	deleteN   int64
	clearN    int64
	unlockID  uint
	err       error
}

func (f *fakeLoginLogMutation) Delete(_ context.Context, ids []uint) (int64, error) {
	f.deleteIDs = ids
	return f.deleteN, f.err
}

func (f *fakeLoginLogMutation) Clear(context.Context) (int64, error) { return f.clearN, f.err }

func (f *fakeLoginLogMutation) Unlock(_ context.Context, id uint) error {
	f.unlockID = id
	return f.err
}

func (f *fakeLoginLogQuery) List(_ context.Context, filter service.LoginLogFilter) ([]service.LoginLogListItem, int64, error) {
	f.filter = filter
	return f.items, f.total, f.err
}

func (f *fakeLoginLogQuery) Detail(context.Context, uint) (*service.LoginLogDetail, error) {
	return f.detail, f.err
}

func setupLoginLogControllerTest(t *testing.T) {
	t.Helper()
	oldResponse, oldLog := app.Response, app.ZapLog
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })
	app.Response = response.NewResponseHandler()
	app.ZapLog = zap.NewNop()
	gin.SetMode(gin.TestMode)
}

func TestLoginLogControllerListOmitsUserAgent(t *testing.T) {
	setupLoginLogControllerTest(t)
	fake := &fakeLoginLogQuery{total: 1, items: []service.LoginLogListItem{{
		ID: 2, Username: "alice", Result: service.LoginResultFailure,
		FailureReason: service.LoginFailurePasswordIncorrect, CreatedAt: time.Now(),
	}}}
	controller := newSysLoginLogControllerWithQuery(fake)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/sysLoginLog/list?pageNum=1&pageSize=20&username=alice", nil)
	controller.List(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "userAgent")
	require.Equal(t, "alice", fake.filter.Username)
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, float64(1), body["data"].(map[string]any)["total"])
}

func TestLoginLogControllerRejectsOversizedPage(t *testing.T) {
	setupLoginLogControllerTest(t)
	controller := newSysLoginLogControllerWithQuery(&fakeLoginLogQuery{})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/sysLoginLog/list?pageNum=1&pageSize=101", nil)
	func() {
		defer func() { _ = recover() }()
		controller.List(c)
	}()
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestLoginLogControllerDetailReturnsNotFound(t *testing.T) {
	setupLoginLogControllerTest(t)
	controller := newSysLoginLogControllerWithQuery(&fakeLoginLogQuery{err: service.ErrLoginLogNotFound})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "99"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/sysLoginLog/99", nil)
	func() {
		defer func() { _ = recover() }()
		controller.Detail(c)
	}()
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestLoginLogControllerMutations(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		mutation   *fakeLoginLogMutation
		wantStatus int
	}{
		{name: "delete", method: http.MethodDelete, path: "/api/sysLoginLog/delete", body: `{"ids":[2,3]}`, mutation: &fakeLoginLogMutation{deleteN: 2}, wantStatus: http.StatusOK},
		{name: "clear", method: http.MethodPost, path: "/api/sysLoginLog/clear", mutation: &fakeLoginLogMutation{clearN: 4}, wantStatus: http.StatusOK},
		{name: "unlock", method: http.MethodPost, path: "/api/sysLoginLog/unlock", body: `{"id":8}`, mutation: &fakeLoginLogMutation{}, wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupLoginLogControllerTest(t)
			controller := newSysLoginLogControllerWithDependencies(&fakeLoginLogQuery{}, tt.mutation)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			func() { defer func() { _ = recover() }(); controllerMethod(controller, tt.name, c) }()
			require.Equal(t, tt.wantStatus, recorder.Code)
		})
	}
}

func controllerMethod(controller *SysLoginLogController, name string, c *gin.Context) {
	switch name {
	case "delete":
		controller.Delete(c)
	case "clear":
		controller.Clear(c)
	case "unlock":
		controller.Unlock(c)
	}
}

func TestLoginLogControllerRejectsInvalidUnlockEvent(t *testing.T) {
	setupLoginLogControllerTest(t)
	controller := newSysLoginLogControllerWithDependencies(&fakeLoginLogQuery{}, &fakeLoginLogMutation{err: service.ErrLoginLogUnlockNotAllowed})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/sysLoginLog/unlock", strings.NewReader(`{"id":8}`))
	c.Request.Header.Set("Content-Type", "application/json")
	func() { defer func() { _ = recover() }(); controller.Unlock(c) }()
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
