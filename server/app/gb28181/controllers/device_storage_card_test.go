package controllers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// storageCardFormatRouter 只注册本主题相关的两条路由（与生产 routes.go 的路径一致）。
// ⛔ 路径必须与 `models.StorageCardFormatRoutePath` 同形：这里手写是**故意的** ——
// 用例要证明"路由注册的路径就是权限检查用的那个全路径"，从被测对象自己取值断言自己等于没测。
func storageCardFormatRouter(f maintenanceFixture, middleware ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	if len(middleware) > 0 {
		router.Use(middleware...)
	}
	router.POST("/channel/:id/storage-cards/format", f.controller.FormatStorageCard)
	router.GET("/device/:id/maintenance-operations", f.controller.ListMaintenanceOperations)
	return router
}

func storageCardFormatRequest(t *testing.T, router *gin.Engine, channelID uint, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channelID)+"/storage-cards/format", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

// TestFormatStorageCardRequiresConfirmationBeforePermissionCheck 缺"显式确认"直接拒，
// 而且**不查权限** —— 顺序是刻意的：对既没权限又没带确认的调用者，先报缺确认不会泄露
// "这个账号有没有格式化权限"。
func TestFormatStorageCardRequiresConfirmationBeforePermissionCheck(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	seedMaintenanceAdmin(t, fixture.db)
	probe := &maintenancePermissionProbe{allowed: false}
	setMaintenancePermissionProbe(t, probe)
	router := storageCardFormatRouter(fixture, maintenanceClaims(17))

	response := storageCardFormatRequest(t, router, fixture.channel.ID, `{"cardIndex":1}`)
	require.Contains(t, response.Body.String(), "显式确认")
	require.Empty(t, probe.path, "缺确认时不应该先查权限（避免泄露授权信息）")
	require.Zero(t, fixture.sender.calls)

	var operations int64
	require.NoError(t, fixture.db.Model(&gbmodels.GbPTZOperation{}).Count(&operations).Error)
	require.Zero(t, operations, "被拒的请求不得落库成 operation")
}

// TestFormatStorageCardDeniedWithoutPermission 这是本功能存在的**唯一理由**：
// 只有 `gb28181:device:control` 的账号（能按录像、能布撤防）**不得**拿到格式化。
//
// 同时钉住权限检查用的路径：它必须由 `models.StorageCardFormatAPIPath` 派生、
// 且与迁移里登记的 sys_api 全路径一致 —— 不一致的表现是"有权限也恒 403"。
func TestFormatStorageCardDeniedWithoutPermission(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	seedMaintenanceAdmin(t, fixture.db)
	probe := &maintenancePermissionProbe{allowed: false}
	setMaintenancePermissionProbe(t, probe)
	router := storageCardFormatRouter(fixture, maintenanceClaims(17))

	response := storageCardFormatRequest(t, router, fixture.channel.ID,
		`{"cardIndex":1,"confirmed":true,"idempotencyKey":"denied"}`)
	require.Contains(t, response.Body.String(), "无存储卡格式化权限")
	require.Equal(t, "/api/gb28181/device-mgmt/channel/"+uintStr(fixture.channel.ID)+"/storage-cards/format", probe.path)
	require.Equal(t, http.MethodPost, probe.method)
	require.Zero(t, fixture.sender.calls)
}

// TestFormatStorageCardDeniedWhenPolicyLookupFails fail-closed：权限服务查不动**不许放行**。
func TestFormatStorageCardDeniedWhenPolicyLookupFails(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	seedMaintenanceAdmin(t, fixture.db)
	setMaintenancePermissionProbe(t, &maintenancePermissionProbe{allowed: true, err: errors.New("policy unavailable")})
	router := storageCardFormatRouter(fixture, maintenanceClaims(17))

	response := storageCardFormatRequest(t, router, fixture.channel.ID, `{"cardIndex":1,"confirmed":true}`)
	require.Contains(t, response.Body.String(), "校验存储卡格式化权限失败")
	require.Zero(t, fixture.sender.calls)
}

// TestFormatStorageCardRejectsAnonymousCaller 匿名调用必须在权限之前被挡。
func TestFormatStorageCardRejectsAnonymousCaller(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	router := storageCardFormatRouter(fixture) // 无 claims
	response := storageCardFormatRequest(t, router, fixture.channel.ID, `{"cardIndex":1,"confirmed":true}`)
	require.Contains(t, response.Body.String(), "未登录")
	require.Zero(t, fixture.sender.calls)
}

// TestFormatStorageCardValidatesCardIndex 卡编号的三种情形：
//   - 字段缺失 → 报错（不能默认成 0，0 的含义是"格式化全部卡"）；
//   - 负数 → 报错（标准只给了 minInclusive=0）；
//   - **0 合法** → 下发，且 payload 里原样保留 0。
func TestFormatStorageCardValidatesCardIndex(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	seedMaintenanceAdmin(t, fixture.db)
	setMaintenancePermissionProbe(t, &maintenancePermissionProbe{allowed: true})
	router := storageCardFormatRouter(fixture, maintenanceClaims(17))

	response := storageCardFormatRequest(t, router, fixture.channel.ID, `{"confirmed":true}`)
	require.Contains(t, response.Body.String(), "缺少存储卡编号")

	response = storageCardFormatRequest(t, router, fixture.channel.ID, `{"confirmed":true,"cardIndex":-1}`)
	require.Contains(t, response.Body.String(), "存储卡编号不合法")

	response = storageCardFormatRequest(t, router, fixture.channel.ID, `{"confirmed":true,"cardIndex":0,"idempotencyKey":"all-cards"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var operation gbmodels.GbPTZOperation
	require.NoError(t, fixture.db.Where("idempotency_key = ?", "all-cards").First(&operation).Error)
	require.Equal(t, "format_sd", operation.Action)
	require.Contains(t, operation.PayloadJSON, `"cardIndex":0`, "0 表示全部卡，必须是显式传下去的 0")
}

// TestFormatStorageCardDispatchesAndReturnsOperation happy path：授权 + 已确认 + 合法编号。
func TestFormatStorageCardDispatchesAndReturnsOperation(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOnline)
	seedMaintenanceAdmin(t, fixture.db)
	setMaintenancePermissionProbe(t, &maintenancePermissionProbe{allowed: true})
	router := storageCardFormatRouter(fixture, maintenanceClaims(17))

	response := storageCardFormatRequest(t, router, fixture.channel.ID,
		`{"cardIndex":2,"confirmed":true,"idempotencyKey":"fmt-happy"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, "format_sd", envelope.Data["action"])
	require.NotEmpty(t, envelope.Data["operationId"])
	// 响应里不得出现 SIP 关联标识。
	require.NotContains(t, response.Body.String(), "Call-ID")

	var operation gbmodels.GbPTZOperation
	require.NoError(t, fixture.db.Where("operation_id = ?", envelope.Data["operationId"]).First(&operation).Error)
	require.Equal(t, "format_sd", operation.Action)
	// 9.3.1 d) / 表 1 序号 12：存储卡格式化是**无应答命令**，设备不会回业务应答。
	require.False(t, operation.ResponseRequired)
	require.Equal(t, 1, operation.MaxAttempts, "破坏性动作不得重发")
	require.Equal(t, fixture.channel.ChannelID, operation.TargetCode, "目标编码与 SDCardStatus 查询同源")
	require.Equal(t, uint(17), operation.ActorID)
	require.Contains(t, operation.PayloadJSON, `"cardIndex":2`)
	// 无应答命令由 Execute **同步**发出（"发出去"就是它全部的语义了），
	// 所以这里恰好是 1 次；有应答的那一族才会留在 queued 等 scheduler 发。
	require.Equal(t, 1, fixture.sender.calls)
}

// TestMaintenanceHistoryIncludesStorageCardFormat 维护记录的白名单要收 format_sd。
//
// ⛔ 这是"记入维护记录"这条验收的落点：写操作本身不难，难的是**事后查得到**。
// 白名单写错（或忘了加）的表现是接口 200、operation 落库了，但列表里没有，
// 且不报任何错 —— 只能靠这条用例发现。
func TestMaintenanceHistoryIncludesStorageCardFormat(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOffline, gbmodels.ChannelStatusOffline)
	seedMaintenanceAdmin(t, fixture.db)
	rows := []gbmodels.GbPTZOperation{
		{OperationID: "hist-reboot", IdempotencyKey: "h1", DeviceID: fixture.device.ID, Action: "teleboot", Status: gbmodels.PTZOperationAccepted, CreatedAt: time.Now()},
		{OperationID: "hist-format", IdempotencyKey: "h2", DeviceID: fixture.device.ID, Action: "format_sd", Status: gbmodels.PTZOperationAccepted, CreatedAt: time.Now()},
		// 非维护动作：既不是重启也不是格式化，不得混进维护记录。
		{OperationID: "hist-query", IdempotencyKey: "h3", DeviceID: fixture.device.ID, Action: "refresh_storage_cards", Status: gbmodels.PTZOperationAccepted, CreatedAt: time.Now()},
	}
	require.NoError(t, fixture.db.Create(&rows).Error)

	router := storageCardFormatRouter(fixture, maintenanceClaims(17))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/device/"+uintStr(fixture.device.ID)+"/maintenance-operations", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var envelope struct {
		Data struct {
			List []struct {
				OperationID string `json:"operationId"`
				Action      string `json:"action"`
			} `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	actions := make(map[string]string)
	for _, row := range envelope.Data.List {
		actions[row.OperationID] = row.Action
	}
	require.Equal(t, "teleboot", actions["hist-reboot"], "重启历史不受影响")
	require.Equal(t, "format_sd", actions["hist-format"], "格式化必须进维护记录")
	require.NotContains(t, actions, "hist-query", "非维护动作不得混进来")
}
