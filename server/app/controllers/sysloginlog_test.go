package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
