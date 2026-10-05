package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordcache"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

// routeRecordCacheService 只实现路由用例关心的那几条：票据签发与认领。
// 其余方法返回零值 —— 这里验证的是"路由形态与鉴权边界"，不是业务。
type routeRecordCacheService struct {
	contentHasDeadline bool
	claimed            int
	claimIndex         int
	claimErr           error
}

func (routeRecordCacheService) Create(context.Context, recordcache.CreateRequest) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, nil
}
func (routeRecordCacheService) Detail(context.Context, string, recordcache.Scope) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, nil
}
func (routeRecordCacheService) List(context.Context, recordcache.ListQuery) (recordcache.PageView, error) {
	return recordcache.PageView{}, nil
}
func (routeRecordCacheService) Cancel(context.Context, string, recordcache.Scope) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, nil
}
func (routeRecordCacheService) Delete(context.Context, string, recordcache.Scope) error { return nil }
func (routeRecordCacheService) SetFavorite(_ context.Context, taskID string, _ recordcache.Scope, favorite bool) (recordcache.TaskView, error) {
	return recordcache.TaskView{TaskID: taskID, Favorite: favorite}, nil
}
func (routeRecordCacheService) Content(context.Context, http.ResponseWriter, string, recordcache.Scope, int, string) error {
	return nil
}
func (routeRecordCacheService) CreateDownload(context.Context, uint, string, recordcache.Scope, int) (gbrecording.DownloadTaskView, string, error) {
	return gbrecording.DownloadTaskView{TaskID: "dl-1"}, "ticket-1", nil
}
func (s *routeRecordCacheService) ClaimDownload(ctx context.Context, writer http.ResponseWriter, _, _ string, index int, _ string, onClaimed func()) error {
	_, s.contentHasDeadline = ctx.Deadline()
	s.claimed++
	s.claimIndex = index
	if s.claimErr != nil {
		return s.claimErr
	}
	if onClaimed != nil {
		onClaimed()
	}
	_, _ = writer.Write([]byte("streamed"))
	return nil
}
func (routeRecordCacheService) DownloadStatus(string, uint) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}
func (routeRecordCacheService) CancelDownload(string, uint) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}

// TestRecordCacheDownloadRoutesBypassGlobalTimeout 钉住两条边界：
//
//	① 带 JWT 的那条只负责**签发**凭据（`POST …/tasks/:taskId/downloads`）；
//	② 真正下文件的那条在裸 engine 上 —— 浏览器自己发起的下载带不了 JWT 头，
//	   走全局中间件（含请求超时）会让大文件下载被掐断。
func TestRecordCacheDownloadRoutesBypassGlobalTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &routeRecordCacheService{}
	gbcontrollers.SetRecordCacheRuntime(service)
	defer gbcontrollers.SetRecordCacheRuntime(nil)

	engine := gin.New()
	RegisterContentRoutes(engine)
	engine.Use(middleware.TimeoutMiddleware(time.Nanosecond))
	RegisterRoutes(engine.Group("/api"))

	want := map[string]bool{
		"POST /api/gb28181/record-cache/tasks":                                        false,
		"POST /api/gb28181/record-cache/tasks/:taskId/favorite":                       false,
		"POST /api/gb28181/record-cache/tasks/:taskId/downloads":                      false,
		"GET /api/gb28181/record-cache/downloads/:downloadId":                         false,
		"DELETE /api/gb28181/record-cache/downloads/:downloadId":                      false,
		"GET /api/gb28181/record-cache/downloads/:downloadId/content":                 false,
		"GET /api/gb28181/record-cache/downloads/:downloadId/segments/:index/content": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, found := range want {
		require.True(t, found, route)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/gb28181/record-cache/downloads/dl-1/content", nil)
	request.AddCookie(&http.Cookie{Name: "uvp_record_cache_download_dl-1", Value: "ticket-1"})
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "streamed", recorder.Body.String())
	require.Equal(t, recordcache.WholeTaskIndex, service.claimIndex, "整段入口的序号是 WholeTaskIndex")
	require.False(t, service.contentHasDeadline,
		"下载路由必须绕过全局请求超时：大文件下载会被掐断")
}

// TestRecordCacheSegmentDownloadCarriesIndexFromPath 钉住「按段下载」的地址形态。
//
// ⛔ 序号必须在**路径**里：票据 cookie 是按路径限定的，序号进路径这张凭据就只对
// 「那一段」有效；放进 query 则同一张凭据能被改成任意序号重放。
// ⛔ 非法序号必须 403 而不是被兜成整段 —— 兜底会让用户以为拿到了第 3 段，实际是整段。
func TestRecordCacheSegmentDownloadCarriesIndexFromPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &routeRecordCacheService{}
	gbcontrollers.SetRecordCacheRuntime(service)
	defer gbcontrollers.SetRecordCacheRuntime(nil)

	engine := gin.New()
	RegisterContentRoutes(engine)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/gb28181/record-cache/downloads/dl-2/segments/1/content", nil)
	request.AddCookie(&http.Cookie{Name: "uvp_record_cache_download_dl-2", Value: "ticket-2"})
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, service.claimIndex)

	for _, bad := range []string{"-1", "abc"} {
		t.Run("非法序号 "+bad, func(t *testing.T) {
			service.claimIndex = recordcache.WholeTaskIndex
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/gb28181/record-cache/downloads/dl-2/segments/"+bad+"/content", nil)
			request.AddCookie(&http.Cookie{Name: "uvp_record_cache_download_dl-2", Value: "ticket-2"})
			engine.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
			require.Equal(t, recordcache.WholeTaskIndex, service.claimIndex, "非法序号不能落到服务层")
		})
	}
}

// TestRecordCacheDownloadContentRejectsWeakCredentials 钉住票据这条路的三条拒收：
// 没有票据、带 query（防把凭据写进日志/历史）、票据已被用掉。
func TestRecordCacheDownloadContentRejectsWeakCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &routeRecordCacheService{}
	gbcontrollers.SetRecordCacheRuntime(service)
	defer gbcontrollers.SetRecordCacheRuntime(nil)

	engine := gin.New()
	RegisterContentRoutes(engine)

	cases := []struct {
		name   string
		target string
		cookie *http.Cookie
		err    error
	}{
		{name: "没有票据", target: "/api/gb28181/record-cache/downloads/dl-1/content"},
		{
			name:   "票据放进了 query（会被日志/历史记录留下）",
			target: "/api/gb28181/record-cache/downloads/dl-1/content?ticket=ticket-1",
			cookie: &http.Cookie{Name: "uvp_record_cache_download_dl-1", Value: "ticket-1"},
		},
		{
			name:   "票据已被认领过",
			target: "/api/gb28181/record-cache/downloads/dl-1/content",
			cookie: &http.Cookie{Name: "uvp_record_cache_download_dl-1", Value: "ticket-1"},
			err:    recordcache.ErrDownloadTicketInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service.claimErr = tc.err
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.cookie != nil {
				request.AddCookie(tc.cookie)
			}
			engine.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
		})
	}
}
