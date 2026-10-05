package controllers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/recordcache"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type stubRecordCacheService struct {
	createdUserID    uint
	createdTaskID    string
	createdIndex     int
	claimed          string
	claimedTicket    string
	claimedIndex     int
	claimErr         error
	issueDownloadErr error

	favoriteCalled bool
	favoriteTaskID string
	favoriteValue  bool
	favoriteErr    error
}

func (s *stubRecordCacheService) Create(context.Context, recordcache.CreateRequest) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, nil
}
func (s *stubRecordCacheService) Detail(context.Context, string, recordcache.Scope) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, nil
}
func (s *stubRecordCacheService) List(context.Context, recordcache.ListQuery) (recordcache.PageView, error) {
	return recordcache.PageView{}, nil
}
func (s *stubRecordCacheService) Cancel(context.Context, string, recordcache.Scope) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, nil
}
func (s *stubRecordCacheService) Delete(context.Context, string, recordcache.Scope) error { return nil }
func (s *stubRecordCacheService) SetFavorite(_ context.Context, taskID string, _ recordcache.Scope, favorite bool) (recordcache.TaskView, error) {
	s.favoriteCalled, s.favoriteTaskID, s.favoriteValue = true, taskID, favorite
	if s.favoriteErr != nil {
		return recordcache.TaskView{}, s.favoriteErr
	}
	return recordcache.TaskView{TaskID: taskID, Favorite: favorite}, nil
}
func (s *stubRecordCacheService) Content(context.Context, http.ResponseWriter, string, recordcache.Scope, int, string) error {
	return nil
}
func (s *stubRecordCacheService) CreateDownload(_ context.Context, userID uint, taskID string, _ recordcache.Scope, index int) (gbrecording.DownloadTaskView, string, error) {
	s.createdUserID, s.createdTaskID, s.createdIndex = userID, taskID, index
	if s.issueDownloadErr != nil {
		return gbrecording.DownloadTaskView{}, "", s.issueDownloadErr
	}
	return gbrecording.DownloadTaskView{TaskID: "dl-1"}, "ticket-1", nil
}
func (s *stubRecordCacheService) ClaimDownload(_ context.Context, writer http.ResponseWriter, downloadID, ticket string, index int, _ string, onClaimed func()) error {
	s.claimed, s.claimedTicket, s.claimedIndex = downloadID, ticket, index
	if s.claimErr != nil {
		return s.claimErr
	}
	if onClaimed != nil {
		onClaimed()
	}
	_, _ = writer.Write([]byte("streamed"))
	return nil
}
func (s *stubRecordCacheService) DownloadStatus(string, uint) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}
func (s *stubRecordCacheService) CancelDownload(string, uint) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, nil
}

func newRecordCacheControllerContext(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	// ⛔ 同包的 zlm_node_test.go 在 init 里把全局 app.Response 换成了 mockResponse，
	// 而那个 mock 的 Fail 硬编码 c.JSON(http.StatusOK, …) 忽略 httpCode。
	// 这几条用例恰好要断言 403 / 200 的真实状态码，必须换回生产的 handler。
	prev := app.Response
	app.Response = response.NewResponseHandler()
	t.Cleanup(func() { app.Response = prev })

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
	return ctx, recorder
}

// TestRecordCacheDownloadTicketCookieIsPathScoped 把票据 cookie 的硬约束钉死。
//
// ⛔ Path 必须精确等于下载地址：放成 `/` 会让这张凭据跟着之后每一个请求发出去；
// ⛔ HttpOnly 必须开：票据是鉴权凭证，不能让脚本读走；
// ⛔ 一旦路径与路由注册的字符串不一致（任一侧改了字面量），下载会**静默**变成
// "没有凭据"的 403 —— 所以这里直接把它和路由字面量对照。
// ⛔ 整段与「第 N 段」必须是**两条不同的 Path**：否则一张整段凭据能被拿去下任意片段。
func TestRecordCacheDownloadTicketCookieIsPathScoped(t *testing.T) {
	wholePath := recordCacheDownloadContentPath("dl-1", recordcache.WholeTaskIndex)
	ctx, recorder := newRecordCacheControllerContext(t, "/")
	setRecordCacheDownloadTicketCookie(ctx, "dl-1", wholePath, "ticket-1", 60)

	raw := recorder.Header().Get("Set-Cookie")
	require.Contains(t, raw, recordCacheDownloadTicketCookieName("dl-1")+"=ticket-1")
	require.Contains(t, raw, "Path=/api/gb28181/record-cache/downloads/dl-1/content",
		"cookie 的作用域必须精确到这一次下载的地址")
	require.Contains(t, raw, "HttpOnly")
	require.Contains(t, raw, "SameSite=Strict")
	require.Contains(t, raw, "Max-Age=60", "票据必须很快过期：它是一次性凭据，不是长期会话")
	require.Equal(t, "/api/gb28181/record-cache/downloads/dl-1/content", wholePath)
	require.Equal(t, "/api/gb28181/record-cache/downloads/dl-1/segments/2/content",
		recordCacheDownloadContentPath("dl-1", 2))
}

// TestRecordCacheCreateDownloadIssuesTicketAndURL 覆盖签发这一半：
// 必须是**当前登录用户**（不能信前端传的 userId）、cookie 与返回的 contentUrl 指向同一个地址。
func TestRecordCacheCreateDownloadIssuesTicketAndURL(t *testing.T) {
	service := &stubRecordCacheService{}
	controller := NewRecordCacheController(service)
	ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/tasks/t1/downloads")
	ctx.Params = gin.Params{{Key: "taskId", Value: "t1"}}

	controller.CreateDownload(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, uint(7), service.createdUserID, "必须用登录态里的 userId")
	require.Equal(t, "t1", service.createdTaskID)
	require.Equal(t, recordcache.WholeTaskIndex, service.createdIndex,
		"不带 body 的默认是**整段**；写成 0 会把整段悄悄变成第 1 片")
	require.Contains(t, recorder.Header().Get("Set-Cookie"), "uvp_record_cache_download_dl-1=ticket-1")
	require.Contains(t, recorder.Body.String(), "/api/gb28181/record-cache/downloads/dl-1/content")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Empty(t, common.GetClaims(ctx).Username, "测试桩不提供用户名，这里只是确认 claims 取到了用户")
}

// TestRecordCacheCreateDownloadAcceptsSegmentIndex 覆盖「按段下载」的签发：
// body 里给了 index 才是那一段，返回的 contentUrl 与 cookie 的 Path 必须一起变成按段地址。
func TestRecordCacheCreateDownloadAcceptsSegmentIndex(t *testing.T) {
	service := &stubRecordCacheService{}
	controller := NewRecordCacheController(service)
	ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/tasks/t1/downloads")
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/record-cache/tasks/t1/downloads",
		strings.NewReader(`{"index":1}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "taskId", Value: "t1"}}

	controller.CreateDownload(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, service.createdIndex)
	require.Contains(t, recorder.Body.String(), "/api/gb28181/record-cache/downloads/dl-1/segments/1/content")
	require.Contains(t, recorder.Header().Get("Set-Cookie"),
		"Path=/api/gb28181/record-cache/downloads/dl-1/segments/1/content")
}

// TestRecordCacheCreateDownloadReportsMergingAsConflict 整理中必须回 409 并说清原因。
// ⛔ 报成 404「暂无可下载的文件」会让用户以为录像丢了 —— 它其实只是还在拼。
func TestRecordCacheCreateDownloadReportsMergingAsConflict(t *testing.T) {
	service := &stubRecordCacheService{issueDownloadErr: recordcache.ErrTaskNotReady}
	controller := NewRecordCacheController(service)
	ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/tasks/t1/downloads")
	ctx.Params = gin.Params{{Key: "taskId", Value: "t1"}}

	controller.CreateDownload(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "整理")
}

// TestRecordCacheDownloadContentClearsTicketOnClaim 覆盖认领这一半：
// 认领成功必须**立刻**把 cookie 清空（Max-Age=-1），否则一张已用过的凭据
// 还会留在浏览器里。
func TestRecordCacheDownloadContentClearsTicketOnClaim(t *testing.T) {
	service := &stubRecordCacheService{}
	controller := NewRecordCacheController(service)
	ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/downloads/dl-1/content")
	ctx.Params = gin.Params{{Key: "downloadId", Value: "dl-1"}}
	ctx.Request.AddCookie(&http.Cookie{Name: "uvp_record_cache_download_dl-1", Value: "ticket-1"})

	controller.DownloadContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "dl-1", service.claimed)
	require.Equal(t, "ticket-1", service.claimedTicket)
	require.Equal(t, recordcache.WholeTaskIndex, service.claimedIndex)
	require.Equal(t, "streamed", recorder.Body.String())
	cleared := recorder.Header().Get("Set-Cookie")
	require.True(t, strings.Contains(cleared, "Max-Age=0") || strings.Contains(cleared, "Max-Age=-1"),
		"认领即作废，cookie 必须被清掉，实际: %q", cleared)
}

// TestRecordCacheDownloadSegmentContentPassesPathIndex 按段入口的序号取自**路径**。
func TestRecordCacheDownloadSegmentContentPassesPathIndex(t *testing.T) {
	service := &stubRecordCacheService{}
	controller := NewRecordCacheController(service)
	target := "/api/gb28181/record-cache/downloads/dl-1/segments/2/content"
	ctx, recorder := newRecordCacheControllerContext(t, target)
	ctx.Params = gin.Params{{Key: "downloadId", Value: "dl-1"}, {Key: "index", Value: "2"}}
	ctx.Request.AddCookie(&http.Cookie{Name: "uvp_record_cache_download_dl-1", Value: "ticket-1"})

	controller.DownloadSegmentContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 2, service.claimedIndex)
	require.Contains(t, recorder.Header().Get("Set-Cookie"), "Path="+target,
		"清 cookie 时 Path 必须与当初种下的完全一致，否则清不掉")
}

// TestRecordCacheSetFavoritePassesExplicitValue 收藏开关必须把**显式传的布尔值**原样传下去。
//
// ⛔ `{"favorite": false}`（取消收藏）和不带这个字段是两回事：入参用值类型时，
// 缺省零值 false 会让"漏传参数"变成"默默取消收藏"。所以实现用 *bool，且这里钉住
// 「false 也必须真的调用到服务」。
func TestRecordCacheSetFavoritePassesExplicitValue(t *testing.T) {
	for _, favorite := range []bool{true, false} {
		service := &stubRecordCacheService{}
		controller := NewRecordCacheController(service)
		ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/tasks/t1/favorite")
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/record-cache/tasks/t1/favorite",
			strings.NewReader(fmt.Sprintf(`{"favorite": %t}`, favorite)))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Params = gin.Params{{Key: "taskId", Value: "t1"}}

		controller.SetFavorite(ctx)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.True(t, service.favoriteCalled, "favorite=%t 时必须调用到服务", favorite)
		require.Equal(t, "t1", service.favoriteTaskID)
		require.Equal(t, favorite, service.favoriteValue)
	}
}

// TestRecordCacheSetFavoriteRejectsMissingValue 缺 favorite 字段必须 422，不能当成 false。
func TestRecordCacheSetFavoriteRejectsMissingValue(t *testing.T) {
	for _, body := range []string{"", "{}", `{"favorite": null}`} {
		service := &stubRecordCacheService{}
		controller := NewRecordCacheController(service)
		ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/tasks/t1/favorite")
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/record-cache/tasks/t1/favorite",
			strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Params = gin.Params{{Key: "taskId", Value: "t1"}}

		controller.SetFavorite(ctx)

		require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "body=%q 应被拒绝", body)
		require.False(t, service.favoriteCalled, "参数不合法时不能往下走到服务")
	}
}

// TestRecordCacheSetFavoriteMapsNotFound 任务不存在 → 404（而不是 502「操作失败」）。
func TestRecordCacheSetFavoriteMapsNotFound(t *testing.T) {
	service := &stubRecordCacheService{favoriteErr: recordcache.ErrTaskNotFound}
	controller := NewRecordCacheController(service)
	ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/tasks/t1/favorite")
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/record-cache/tasks/t1/favorite",
		strings.NewReader(`{"favorite": true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "taskId", Value: "t1"}}

	controller.SetFavorite(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
}

// TestRecordCacheDownloadContentRejectsTicketWhenServiceRefuses 服务说凭据无效时必须 403，
// 不能兜底成 502「操作失败」—— 那会让"链接过期了"看起来像"服务器坏了"。
func TestRecordCacheDownloadContentRejectsTicketWhenServiceRefuses(t *testing.T) {
	service := &stubRecordCacheService{claimErr: recordcache.ErrDownloadTicketInvalid}
	controller := NewRecordCacheController(service)
	ctx, recorder := newRecordCacheControllerContext(t, "/api/gb28181/record-cache/downloads/dl-1/content")
	ctx.Params = gin.Params{{Key: "downloadId", Value: "dl-1"}}
	ctx.Request.AddCookie(&http.Cookie{Name: "uvp_record_cache_download_dl-1", Value: "stale"})

	controller.DownloadContent(ctx)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
}
