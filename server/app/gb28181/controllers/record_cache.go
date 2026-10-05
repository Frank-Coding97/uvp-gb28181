package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/gb28181/recordcache"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// RecordCacheAPI 是控制器依赖的服务能力（窄接口，便于用不可用实现兜底）。
type RecordCacheAPI interface {
	Create(context.Context, recordcache.CreateRequest) (recordcache.TaskView, error)
	Detail(context.Context, string, recordcache.Scope) (recordcache.TaskView, error)
	List(context.Context, recordcache.ListQuery) (recordcache.PageView, error)
	Cancel(context.Context, string, recordcache.Scope) (recordcache.TaskView, error)
	Delete(context.Context, string, recordcache.Scope) error
	// SetFavorite 收藏/取消收藏：收藏的任务不参与保留期自动清理。
	SetFavorite(context.Context, string, recordcache.Scope, bool) (recordcache.TaskView, error)
	Content(context.Context, http.ResponseWriter, string, recordcache.Scope, int, string) error
	// 票据下载：建任务 + 认领（浏览器原生下载用，见 recordcache/download.go）。
	// index 取 recordcache.WholeTaskIndex 表示整段。
	CreateDownload(context.Context, uint, string, recordcache.Scope, int) (gbrecording.DownloadTaskView, string, error)
	ClaimDownload(context.Context, http.ResponseWriter, string, string, int, string, func()) error
	DownloadStatus(string, uint) (gbrecording.DownloadTaskView, error)
	CancelDownload(string, uint) (gbrecording.DownloadTaskView, error)
}

// RecordCacheController 提供「录像缓存」任务的全部 HTTP 入口。
type RecordCacheController struct{ service RecordCacheAPI }

func NewRecordCacheController(service RecordCacheAPI) *RecordCacheController {
	return &RecordCacheController{service: service}
}

var recordCacheController = NewRecordCacheController(nil)

// RecordCacheRuntime 返回全局控制器实例（路由注册用）。
func RecordCacheRuntime() *RecordCacheController { return recordCacheController }

// SetRecordCacheRuntime 装配/卸载服务。传 nil 时接口一律 503，
// 而不是 panic —— 装配失败只该让这个功能不可用，不该把整个进程带崩。
func SetRecordCacheRuntime(service RecordCacheAPI) {
	if service == nil {
		recordCacheController = NewRecordCacheController(nil)
		return
	}
	recordCacheController = NewRecordCacheController(service)
}

type recordCacheCreateBody struct {
	ChannelID     uint   `json:"channelId"`
	RecordKey     string `json:"recordKey"`
	RecordType    string `json:"recordType"`
	PlayFrom      string `json:"playFrom"`
	DownloadSpeed int    `json:"downloadSpeed"`
}

func (c *RecordCacheController) Create(ctx *gin.Context) {
	claims := common.GetClaims(ctx)
	if claims == nil || claims.UserID == 0 {
		recordCacheFailure(ctx, http.StatusUnauthorized, "未登录")
		return
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 32*1024)
	var body recordCacheCreateBody
	if err := ctx.ShouldBindJSON(&body); err != nil || body.ChannelID == 0 || strings.TrimSpace(body.RecordKey) == "" {
		recordCacheFailure(ctx, http.StatusUnprocessableEntity, "缓存参数不合法")
		return
	}
	playFrom, err := parseRecordCacheTime(body.PlayFrom)
	if err != nil {
		recordCacheFailure(ctx, http.StatusUnprocessableEntity, "起始时间不合法")
		return
	}
	result, err := c.requireService().Create(ctx.Request.Context(), recordcache.CreateRequest{
		OwnerUserID: claims.UserID, OwnerName: claims.Username,
		ChannelID: body.ChannelID, RecordKey: strings.TrimSpace(body.RecordKey),
		RecordType: body.RecordType, PlayFrom: playFrom, DownloadSpeed: body.DownloadSpeed,
	})
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, result)
}

func (c *RecordCacheController) List(ctx *gin.Context) {
	claims := common.GetClaims(ctx)
	if claims == nil || claims.UserID == 0 {
		recordCacheFailure(ctx, http.StatusUnauthorized, "未登录")
		return
	}
	query := recordcache.ListQuery{
		UserID: claims.UserID, Page: recordCacheInt(ctx.Query("page"), 1),
		PageSize: recordCacheInt(ctx.Query("size"), 20), State: ctx.Query("state"),
		Keyword: ctx.Query("keyword"), ChannelID: uint(recordCacheInt(ctx.Query("channelId"), 0)),
	}
	query.Scope = recordCacheScope(ctx)
	result, err := c.requireService().List(ctx.Request.Context(), query)
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, result)
}

func (c *RecordCacheController) Detail(ctx *gin.Context) {
	taskID, ok := recordCacheTaskID(ctx)
	if !ok {
		return
	}
	result, err := c.requireService().Detail(ctx.Request.Context(), taskID, recordCacheScope(ctx))
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, result)
}

func (c *RecordCacheController) Cancel(ctx *gin.Context) {
	taskID, ok := recordCacheTaskID(ctx)
	if !ok {
		return
	}
	result, err := c.requireService().Cancel(ctx.Request.Context(), taskID, recordCacheScope(ctx))
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, result)
}

func (c *RecordCacheController) Delete(ctx *gin.Context) {
	taskID, ok := recordCacheTaskID(ctx)
	if !ok {
		return
	}
	if err := c.requireService().Delete(ctx.Request.Context(), taskID, recordCacheScope(ctx)); err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, gin.H{"taskId": taskID})
}

// recordCacheFavoriteBody 是收藏开关的入参。
//
// ⛔ 必须用指针：`{"favorite": false}`（取消收藏）和不带这个字段是两回事。
// 用值类型时缺省会变成 false，等于"漏传参数就默默取消收藏"。
type recordCacheFavoriteBody struct {
	Favorite *bool `json:"favorite"`
}

// SetFavorite 收藏 / 取消收藏一个缓存任务。
//
// 收藏的语义只有一个：**这条录像不参与保留期自动清理**（只能手动删除）。
func (c *RecordCacheController) SetFavorite(ctx *gin.Context) {
	taskID, ok := recordCacheTaskID(ctx)
	if !ok {
		return
	}
	var body recordCacheFavoriteBody
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4*1024)
	if err := ctx.ShouldBindJSON(&body); err != nil || body.Favorite == nil {
		recordCacheFailure(ctx, http.StatusUnprocessableEntity, "收藏参数不合法")
		return
	}
	result, err := c.requireService().SetFavorite(ctx.Request.Context(), taskID, recordCacheScope(ctx), *body.Favorite)
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, result)
}

// Content 把任务的产出按 Range 透传出去（直连下载，带 JWT 头）。
//
// ⛔ 序号是**可选**的两种含义，不是"必填的分片号"：
// 不传 = 整段（多分片时是收尾期合好的那一个文件），传了 = 只要那一片。
// 以前把它写成"必填"，前端于是只能传 0 来表达"整段"，而多分片分支又忽略序号 ——
// 结果是「第 2 段」按钮实际发的是整段录像。
func (c *RecordCacheController) Content(ctx *gin.Context) {
	taskID, ok := recordCacheTaskID(ctx)
	if !ok {
		return
	}
	index, ok := recordCacheContentIndex(ctx.Query("index"))
	if !ok {
		recordCacheFailure(ctx, http.StatusBadRequest, "分片序号不合法")
		return
	}
	err := c.requireService().Content(ctx.Request.Context(), ctx.Writer, taskID, recordCacheScope(ctx), index, ctx.GetHeader("Range"))
	if err == nil || ctx.Writer.Written() {
		return
	}
	respondRecordCacheError(ctx, err)
}

// recordCacheDownloadBody 是签发下载时的可选入参。
//
// ⛔ Index 必须是指针：`{"index":0}`（第 1 段）和不带 body（整段）是两回事，
// 用 int 会让缺省值 0 把"整段"悄悄变成"第 1 段"。
type recordCacheDownloadBody struct {
	Index *int `json:"index"`
}

// CreateDownload 建一次「票据下载」并返回免鉴权的下载地址。
//
// ⛔ 为什么要绕这一圈而不是直接用带 JWT 头的 XHR 下载：浏览器自己的下载
// （`<a href download>`）带不了自定义请求头，而录像接口是受保护的。
// 退而求其次只能"整包收进内存 Blob 再落盘" —— 没有下载栏条目、没有进度、
// 不能暂停续传、大文件还会把标签页拖崩。票据这条路同仓云录像已经跑通：
// 一次性、只对本任务路径有效、60 秒内必须用掉、HttpOnly 不给脚本读。
func (c *RecordCacheController) CreateDownload(ctx *gin.Context) {
	taskID, ok := recordCacheTaskID(ctx)
	if !ok {
		return
	}
	claims := common.GetClaims(ctx)
	if claims == nil || claims.UserID == 0 {
		recordCacheFailure(ctx, http.StatusUnauthorized, "未登录")
		return
	}
	index := recordcache.WholeTaskIndex
	// ⛔ 不用 ShouldBindJSON：这个 POST 允许完全没有 body（整段下载），
	// 而 ShouldBindJSON 在空 body 上直接报 EOF，会把"整段下载"打成 422。
	if ctx.Request.ContentLength != 0 {
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4*1024)
		var body recordCacheDownloadBody
		if err := ctx.ShouldBindJSON(&body); err != nil {
			recordCacheFailure(ctx, http.StatusUnprocessableEntity, "下载参数不合法")
			return
		}
		if body.Index != nil {
			index = *body.Index
		}
	}
	if index < recordcache.WholeTaskIndex {
		recordCacheFailure(ctx, http.StatusUnprocessableEntity, "分片序号不合法")
		return
	}
	result, ticket, err := c.requireService().CreateDownload(ctx.Request.Context(), claims.UserID, taskID, recordCacheScope(ctx), index)
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	contentPath := recordCacheDownloadContentPath(result.TaskID, index)
	setRecordCacheDownloadTicketCookie(ctx, result.TaskID, contentPath, ticket, 60)
	ctx.Header("Cache-Control", "no-store")
	app.Response.Success(ctx, gin.H{"task": result, "contentUrl": contentPath})
}

// DownloadContent 免鉴权的「整段」下载入口：只认 cookie 里的票据，单次消费。
//
// ⛔ 挂在**裸 engine** 上（不经 JWT / 请求超时中间件）—— 见 routes 里的说明。
// ⛔ 禁 query：票据在 cookie 里，允许 query 就等于允许把凭据写进日志/历史记录。
func (c *RecordCacheController) DownloadContent(ctx *gin.Context) {
	c.serveDownloadContent(ctx, recordcache.WholeTaskIndex)
}

// DownloadSegmentContent 免鉴权的「按段」下载入口：目标写进**路径**而不是 query。
//
// ⛔ 为什么序号必须在路径里：票据 cookie 是按路径限定的，序号进了路径，
// 这张凭据就只对"那一段"有效；放进 query 则同一张凭据能被改成任意序号重放。
func (c *RecordCacheController) DownloadSegmentContent(ctx *gin.Context) {
	index, ok := recordCacheContentIndex(ctx.Param("index"))
	if !ok || index == recordcache.WholeTaskIndex {
		recordCacheFailure(ctx, http.StatusForbidden, "下载凭据无效")
		return
	}
	c.serveDownloadContent(ctx, index)
}

func (c *RecordCacheController) serveDownloadContent(ctx *gin.Context, index int) {
	downloadID := ctx.Param("downloadId")
	if downloadID == "" || ctx.Request.URL.RawQuery != "" {
		recordCacheFailure(ctx, http.StatusForbidden, "下载凭据无效")
		return
	}
	ticket, err := ctx.Cookie(recordCacheDownloadTicketCookieName(downloadID))
	if err != nil || ticket == "" {
		recordCacheFailure(ctx, http.StatusForbidden, "下载凭据无效")
		return
	}
	err = c.requireService().ClaimDownload(ctx.Request.Context(), ctx.Writer, downloadID, ticket, index, ctx.GetHeader("Range"), func() {
		// 认领即作废：清了 cookie，浏览器里就不会留着一张还能用的凭据。
		setRecordCacheDownloadTicketCookie(ctx, downloadID, recordCacheDownloadContentPath(downloadID, index), "", -1)
	})
	if err == nil || ctx.Writer.Written() {
		return
	}
	if errors.Is(err, recordcache.ErrDownloadTicketInvalid) {
		recordCacheFailure(ctx, http.StatusForbidden, "下载凭据无效")
		return
	}
	respondRecordCacheError(ctx, err)
}

func (c *RecordCacheController) DownloadStatus(ctx *gin.Context) {
	claims := common.GetClaims(ctx)
	if claims == nil || claims.UserID == 0 {
		recordCacheFailure(ctx, http.StatusUnauthorized, "未登录")
		return
	}
	result, err := c.requireService().DownloadStatus(strings.TrimSpace(ctx.Param("downloadId")), claims.UserID)
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, result)
}

func (c *RecordCacheController) CancelDownload(ctx *gin.Context) {
	claims := common.GetClaims(ctx)
	if claims == nil || claims.UserID == 0 {
		recordCacheFailure(ctx, http.StatusUnauthorized, "未登录")
		return
	}
	result, err := c.requireService().CancelDownload(strings.TrimSpace(ctx.Param("downloadId")), claims.UserID)
	if err != nil {
		respondRecordCacheError(ctx, err)
		return
	}
	app.Response.Success(ctx, result)
}

// recordCacheDownloadCookiePrefix 与云录像的票据前缀刻意不同：
// 两者是独立注册表、独立路径，共用前缀会让"清错 cookie"这类问题变得难查。
const recordCacheDownloadCookiePrefix = "uvp_record_cache_download_"

func recordCacheDownloadTicketCookieName(downloadID string) string {
	return recordCacheDownloadCookiePrefix + downloadID
}

// recordCacheDownloadContentPath 是票据被限定的那一条地址。
//
// ⛔ 必须与 routes 里注册的**字面量**逐字一致：cookie 的 Path 与路由不匹配时，
// 浏览器不会带上票据，表现是"点下载得到 403"，而日志里一切正常。
func recordCacheDownloadContentPath(downloadID string, index int) string {
	base := "/api/gb28181/record-cache/downloads/" + downloadID
	if index == recordcache.WholeTaskIndex {
		return base + "/content"
	}
	return base + "/segments/" + strconv.Itoa(index) + "/content"
}

// recordCacheContentIndex 解析下载序号：空 = 整段。
func recordCacheContentIndex(value string) (int, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return recordcache.WholeTaskIndex, true
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

// setRecordCacheDownloadTicketCookie 种/清票据。
// Path 精确限定到这一次下载的 content 地址：这张凭据在别处一律不会被带上。
func setRecordCacheDownloadTicketCookie(ctx *gin.Context, downloadID, contentPath, value string, maxAge int) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name: recordCacheDownloadTicketCookieName(downloadID), Value: value,
		Path: contentPath, MaxAge: maxAge,
		HttpOnly: true, Secure: downloadCookieSecure(ctx), SameSite: http.SameSiteStrictMode,
	})
}

func (c *RecordCacheController) requireService() RecordCacheAPI {
	if c != nil && c.service != nil {
		return c.service
	}
	return unavailableRecordCacheService{}
}

// recordCacheScope 构造数据权限 scope：缓存任务的可见性跟随它引用的通道/设备，
// 与设备录像回放页用的是同一条判据（否则会出现"能看设备却看不到自己的缓存"，
// 或更糟的"能看不属于自己的录像缓存"）。
//
// ⛔ 刻意用 VisibilityScope（不带 DB）而不是 VisibilityScopeWithDB(ctx, app.DB(), …)：
// 它返回的是**惰性闭包**，真到查权限时才拿调用方那条查询自己的连接，
// 于是这里完全不碰全局连接，与仓库里其余 20 多处调用点保持一致。
// 以前写成 app.DB() 会在单测里因为全局 ConfigYml/连接为 nil 直接 nil panic。
func recordCacheScope(ctx *gin.Context) recordcache.Scope {
	return datascope.VisibilityScope(ctx, "gb_record_cache_task.owner_dept_id", "gb_record_cache_task.device_id")
}

func recordCacheTaskID(ctx *gin.Context) (string, bool) {
	taskID := strings.TrimSpace(ctx.Param("taskId"))
	if taskID == "" || len(taskID) > 64 {
		recordCacheFailure(ctx, http.StatusBadRequest, "任务号不合法")
		return "", false
	}
	return taskID, true
}

func recordCacheInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return parsed
}

// parseRecordCacheTime 解析前端给的录像起点。
// 只认 RFC3339（带时区）：录像时间轴必须无歧义，接受"裸时间"等于让它按服务端
// 本地时区猜一次，跨时区部署时会整体偏几个小时。
func parseRecordCacheTime(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, trimmed)
}

func respondRecordCacheError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, recordcache.ErrTaskNotFound):
		recordCacheFailure(ctx, http.StatusNotFound, "缓存任务不存在")
	case errors.Is(err, recordcache.ErrChannelBusy):
		recordCacheFailure(ctx, http.StatusTooManyRequests, "该通道已有缓存任务进行中")
	case errors.Is(err, recordcache.ErrInvalidRequest):
		recordCacheFailure(ctx, http.StatusUnprocessableEntity, "缓存参数不合法")
	case errors.Is(err, recordcache.ErrNotCancellable):
		recordCacheFailure(ctx, http.StatusConflict, "任务已结束，无法取消")
	case errors.Is(err, recordcache.ErrNotDownloadable):
		recordCacheFailure(ctx, http.StatusNotFound, "暂无可下载的文件")
	// ⛔ 与上面那条区分开：分段文件是好的，只是合不成单个文件。
	// 报成 404「暂无可下载的文件」会让用户以为录像丢了，实际它还在节点上。
	case errors.Is(err, recordcache.ErrMergeFailed):
		recordCacheFailure(ctx, http.StatusConflict, "该录像分段暂时无法合并成一个文件，请按分段下载")
	// 收尾整理还没完成：文件和"暂无可下载"是两回事，不能报 404 让人以为录像丢了。
	case errors.Is(err, recordcache.ErrTaskNotReady):
		recordCacheFailure(ctx, http.StatusConflict, "录像正在整理，请稍候再试")
	case errors.Is(err, recordcache.ErrDownloadTicketInvalid):
		recordCacheFailure(ctx, http.StatusForbidden, "下载凭据无效")
	case errors.Is(err, recordcache.ErrDownloadUnavailable):
		recordCacheFailure(ctx, http.StatusServiceUnavailable, "录像缓存下载未装配")
	case errors.Is(err, recordcache.ErrNodeUnavailable):
		recordCacheFailure(ctx, http.StatusBadGateway, "媒体节点不可用")
	case errors.Is(err, recordcache.ErrPlaybackUnavailable):
		recordCacheFailure(ctx, http.StatusServiceUnavailable, "录像缓存服务未装配")
	default:
		// 回放侧的忙/过期等原因由服务翻译后落在 LastError 上，
		// 这里只兜底成 502，避免把内部错误直接抛给前端。
		recordCacheFailure(ctx, http.StatusBadGateway, "录像缓存操作失败")
	}
}

func recordCacheFailure(ctx *gin.Context, status int, message string) {
	app.Response.Fail(ctx, message, status, 1, nil)
}

// unavailableRecordCacheService 在服务未装配时兜底，语义与其它控制器的
// unavailable* 一致：不 panic，返回"未装配"。
type unavailableRecordCacheService struct{}

func (unavailableRecordCacheService) Create(context.Context, recordcache.CreateRequest) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) Detail(context.Context, string, recordcache.Scope) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) List(context.Context, recordcache.ListQuery) (recordcache.PageView, error) {
	return recordcache.PageView{}, recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) Cancel(context.Context, string, recordcache.Scope) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) Delete(context.Context, string, recordcache.Scope) error {
	return recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) SetFavorite(context.Context, string, recordcache.Scope, bool) (recordcache.TaskView, error) {
	return recordcache.TaskView{}, recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) Content(context.Context, http.ResponseWriter, string, recordcache.Scope, int, string) error {
	return recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) CreateDownload(context.Context, uint, string, recordcache.Scope, int) (gbrecording.DownloadTaskView, string, error) {
	return gbrecording.DownloadTaskView{}, "", recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) ClaimDownload(context.Context, http.ResponseWriter, string, string, int, string, func()) error {
	return recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) DownloadStatus(string, uint) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, recordcache.ErrPlaybackUnavailable
}

func (unavailableRecordCacheService) CancelDownload(string, uint) (gbrecording.DownloadTaskView, error) {
	return gbrecording.DownloadTaskView{}, recordcache.ErrPlaybackUnavailable
}
