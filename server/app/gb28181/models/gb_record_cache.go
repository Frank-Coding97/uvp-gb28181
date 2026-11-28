package models

// 设备录像缓存的对外契约（路径 / 菜单 / 权限）。
//
// ⛔ 这些字面量同时被三处消费，且**任一处写歪都不报错**：
//  1. 迁移与三方言快照里的 sys_api / sys_menu 种子（授权与菜单可见性）；
//  2. gin 路由注册（接口是否存在）；
//  3. 前端菜单与按钮（入口是否显示、点开是否空白）。
//
// 对不上的表现只有两种：接口恒 403（path 漂了），或菜单在但点开空白（component 漂了）。
// 因此 gb_record_cache_ddl_test.go 把它们钉到**迁移文本里的字面量**上，而不是自比自。
const (
	// RecordCacheAPIBase 是这组接口的公共前缀。
	RecordCacheAPIBase = "/api/gb28181/record-cache/tasks"

	// RecordCacheCreateAPIPath 创建缓存任务（POST）。
	RecordCacheCreateAPIPath = RecordCacheAPIBase
	// RecordCacheListAPIPath 缓存任务列表（GET）。
	RecordCacheListAPIPath = RecordCacheAPIBase
	// RecordCacheDetailAPIPath 任务详情/实时进度（GET）。
	RecordCacheDetailAPIPath = RecordCacheAPIBase + "/:taskId"
	// RecordCacheCancelAPIPath 取消任务（POST）。
	RecordCacheCancelAPIPath = RecordCacheAPIBase + "/:taskId/cancel"
	// RecordCacheDeleteAPIPath 删除任务与其文件（DELETE）。
	RecordCacheDeleteAPIPath = RecordCacheAPIBase + "/:taskId"
	// RecordCacheFavoriteAPIPath 收藏 / 取消收藏（POST，body `{"favorite": true|false}`）。
	// 收藏的录像不参与保留期自动清理，只有手动删除才会清掉文件。
	RecordCacheFavoriteAPIPath = RecordCacheAPIBase + "/:taskId/favorite"
	// RecordCacheContentAPIPath 下载已缓存的文件（GET，同源 + Range）。
	// ⛔ 这是**带 JWT 的旧入口**（浏览器 XHR 收 blob），新前端已不用；
	// 保留它是为了不破坏既有调用方与已授权的 casbin 策略。
	RecordCacheContentAPIPath = RecordCacheAPIBase + "/:taskId/content"

	// RecordCacheDownloadTicketAPIPath 签发一次「票据下载」（POST）。
	//
	// 与上面那条的区别：这条只**签发**一张一次性凭据（种成 HttpOnly cookie）并返回
	// 免鉴权下载地址，本身不传文件；真正下文件走 RecordCacheDownloadContentPath 系列。
	RecordCacheDownloadTicketAPIPath = RecordCacheAPIBase + "/:taskId/downloads"

	// RecordCacheDownloadAPIBase 是「票据下载」这组接口的公共前缀。
	// ⛔ 与 RecordCacheAPIBase 不同：票据按 downloadId 定位，不挂在 taskId 下。
	RecordCacheDownloadAPIBase = "/api/gb28181/record-cache/downloads"

	// RecordCacheDownloadStatusAPIPath 查询 / 取消一次票据下载（GET 与 DELETE 共用一条路径）。
	RecordCacheDownloadStatusAPIPath = RecordCacheDownloadAPIBase + "/:downloadId"
)

// 菜单四件套：path / name / component / title。
const (
	RecordCacheMenuID        = 140610
	RecordCacheMenuPath      = "/gb28181/record-cache"
	RecordCacheMenuName      = "gb28181-record-cache"
	RecordCacheMenuComponent = "gb28181/record-cache/index"
	RecordCacheMenuTitle     = "录像缓存"
	RecordCacheMenuSort      = 52
)

// 按钮权限点。
const (
	RecordCachePermissionView     = "gb28181:record-cache:view"
	RecordCachePermissionCreate   = "gb28181:record-cache:create"
	RecordCachePermissionCancel   = "gb28181:record-cache:cancel"
	RecordCachePermissionDownload = "gb28181:record-cache:download"
	RecordCachePermissionDelete   = "gb28181:record-cache:delete"
	// RecordCachePermissionFavorite 收藏 / 取消收藏。收藏后该录像不再被自动清理。
	RecordCachePermissionFavorite = "gb28181:record-cache:favorite"
)

// DeviceRecordDownloadPermission 是设备录像回放页「下载」入口原有的权限点。
// 缓存任务由该页发起，所以创建/进度接口额外挂到这个权限点下，
// 以免只在回放页有权限的用户点下载直接 403。
const DeviceRecordDownloadPermission = "gb28181:device-record:download"
