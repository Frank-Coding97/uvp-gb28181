package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/response"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func statusPagingTasks(count int) []upgrade.TaskRecord {
	records := make([]upgrade.TaskRecord, 0, count)
	for i := 0; i < count; i++ {
		records = append(records, upgrade.TaskRecord{
			TaskID:         fmt.Sprintf("%032d", i),
			IdempotencyKey: fmt.Sprintf("idem-%d", i),
			Actor:          "admin",
			TargetDigest:   strings.Repeat("a", 64),
			Version:        fmt.Sprintf("1.1.%d", i),
			BuildID:        fmt.Sprintf("1.1.%d-build", i),
			Phase:          upgrade.PhaseSucceeded,
			Committed:      true,
			ReleaseNotes:   "第 " + fmt.Sprint(i) + " 版更新说明",
			UpdatedAt:      fmt.Sprintf("2026-11-%02dT00:00:00Z", (i%28)+1),
		})
	}
	return records
}

// statusPagingRPC 模拟升级助手：status 回全量（老助手行为），
// task_list 按页回；rejectPaging=true 时模拟 1.1.28 及更早的助手
// （DisallowUnknownFields，收到 page 直接报 unknown field）。
type statusPagingRPC struct {
	tasks        []upgrade.TaskRecord
	rejectPaging bool
	pagedCalls   int
	statusCalls  int
}

func (f *statusPagingRPC) Call(_ context.Context, _ string, request upgrade.ProtocolRequest) (upgrade.ProtocolResponse, error) {
	response := upgrade.ProtocolResponse{Protocol: upgrade.UpdaterProtocolVersion, OK: true}
	switch request.Action {
	case upgrade.ActionStatus:
		f.statusCalls++
		response.Status = &upgrade.ControlStatus{
			Deployment: "standalone",
			Tasks:      f.tasks,
			TaskTotal:  len(f.tasks),
		}
	case upgrade.ActionTaskList:
		if f.rejectPaging && (request.Page != 0 || request.PageSize != 0) {
			return upgrade.ProtocolResponse{}, errors.New(`json: unknown field "page"`)
		}
		f.pagedCalls++
		if request.Page == 0 && request.PageSize == 0 {
			// 老助手的分页字段不存在，task_list 一直是「全量回传」。
			response.Tasks, response.TaskTotal = f.tasks, len(f.tasks)
			return response, nil
		}
		paged, total := upgrade.PageTasks(f.tasks, request.Page, request.PageSize)
		response.Tasks, response.TaskTotal = paged, total
	}
	return response, nil
}

func newStatusPagingController(t *testing.T, rpc serviceUpgradeRPC) *ServiceUpgradeController {
	t.Helper()
	controller := newServiceUpgradeController(serviceUpgradeOptions{VersionPath: filepath.Join(t.TempDir(), "version.json"), RPC: rpc})
	oldResponse, oldLog := app.Response, app.ZapLog
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	t.Cleanup(func() { app.Response, app.ZapLog = oldResponse, oldLog })
	return controller
}

func decodeStatusPayload(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}

// 状态接口必须与历史条数无关：只回最近摘要、不带更新说明，条数另有 taskTotal。
func TestUpgradeStatusKeepsTaskWindowIndependentOfHistorySize(t *testing.T) {
	rpc := &statusPagingRPC{tasks: statusPagingTasks(45)}
	controller := newStatusPagingController(t, rpc)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/system/upgrade/status", nil)

	controller.Status(ctx)

	payload := decodeStatusPayload(t, recorder)
	tasks, _ := payload["tasks"].([]any)
	require.Len(t, tasks, upgrade.StatusTaskLimit)
	require.EqualValues(t, 45, payload["taskTotal"])
	require.NotContains(t, payload, "history", "首屏轮询不该附带历史页")
	for _, item := range tasks {
		record := item.(map[string]any)
		require.NotContains(t, record, "release_notes", "状态摘要不该带更新说明正文")
	}

	// 回归眼：整包 JSON 必须远小于 64KB 的读取上限。
	require.Less(t, recorder.Body.Len(), 16<<10, "状态响应应该小而稳定")
}

func TestUpgradeStatusReturnsPagedHistoryOnDemand(t *testing.T) {
	rpc := &statusPagingRPC{tasks: statusPagingTasks(45)}
	controller := newStatusPagingController(t, rpc)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/system/upgrade/status?page=3&pageSize=10", nil)

	controller.Status(ctx)

	payload := decodeStatusPayload(t, recorder)
	history, ok := payload["history"].(map[string]any)
	require.True(t, ok, "带 page 时必须返回 history 块")
	require.EqualValues(t, 3, history["page"])
	require.EqualValues(t, 10, history["pageSize"])
	require.EqualValues(t, 45, history["total"])
	paged, _ := history["tasks"].([]any)
	require.Len(t, paged, 10)
	first := paged[0].(map[string]any)
	require.Contains(t, first, "release_notes", "历史分页必须带更新说明")
}

// 老助手（<=1.1.28）会拒收分页字段；必须退回不分页调用再本地切片，
// 否则升级窗口内历史面板整块报错。
func TestUpgradeStatusFallsBackWhenHelperRejectsPaging(t *testing.T) {
	rpc := &statusPagingRPC{tasks: statusPagingTasks(45), rejectPaging: true}
	controller := newStatusPagingController(t, rpc)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/system/upgrade/status?page=2&pageSize=10", nil)

	controller.Status(ctx)

	payload := decodeStatusPayload(t, recorder)
	history, ok := payload["history"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 2, history["page"])
	require.EqualValues(t, 45, history["total"])
	paged, _ := history["tasks"].([]any)
	require.Len(t, paged, 10)
	require.Equal(t, 1, rpc.pagedCalls, "回退路径只应成功调用一次 task_list")
}
