package controllers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type workOrderAPIStub struct {
	starts    []workrecording.OrderStartRequest
	active    workrecording.BatchSnapshot
	activeErr error
	deletes   []workrecording.BatchDeleteRequest
	result    workrecording.BatchDeleteResult
	deleteErr error

	historyByField map[string][]workrecording.FormHistoryEntry
	historyErr     error
}

func (s *workOrderAPIStub) StartOrder(_ context.Context, _ uint, request workrecording.OrderStartRequest) (workrecording.BatchSnapshot, error) {
	s.starts = append(s.starts, request)
	return workrecording.BatchSnapshot{
		ID: "order-1", State: workrecording.StateRecording,
		FormState: workrecording.FormSubmitted, FormVersion: 1,
		ProjectName: request.Form.ProjectName,
	}, nil
}

func (s *workOrderAPIStub) Stop(context.Context, string) (workrecording.BatchSnapshot, error) {
	return workrecording.BatchSnapshot{}, nil
}

func (s *workOrderAPIStub) Get(context.Context, string) (workrecording.BatchSnapshot, error) {
	return workrecording.BatchSnapshot{}, nil
}

func (s *workOrderAPIStub) List(context.Context, workrecording.BatchListFilter) ([]workrecording.BatchSnapshot, int64, error) {
	return nil, 0, nil
}

func (s *workOrderAPIStub) Active(context.Context, uint) (workrecording.BatchSnapshot, error) {
	return s.active, s.activeErr
}

func (s *workOrderAPIStub) Delete(_ context.Context, _ uint, request workrecording.BatchDeleteRequest) (workrecording.BatchDeleteResult, error) {
	s.deletes = append(s.deletes, request)
	return s.result, s.deleteErr
}

func (s *workOrderAPIStub) ListFormHistory(_ context.Context, field string, _ int) ([]workrecording.FormHistoryEntry, error) {
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	return s.historyByField[field], nil
}

func init() {
	if app.Response == nil {
		app.Response = response.NewResponseHandler()
	}
}

func workOrderRouter(t *testing.T, db *gorm.DB, stub *workOrderAPIStub, actor uint) *gin.Engine {
	t.Helper()
	controller := gbcontrollers.NewWorkRecordingController(&workRecordingAPIFake{})
	controller.SetDB(func() *gorm.DB { return db })
	controller.SetBatchService(stub)
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(actor))
	router.POST("/work-orders", controller.CreateWorkOrder)
	router.GET("/work-orders/active", controller.ActiveWorkOrder)
	router.POST("/work-orders/batch-delete", controller.BatchDeleteWorkOrders)
	router.DELETE("/work-orders/:id", controller.DeleteWorkOrder)
	router.GET("/work-orders/form-history", controller.WorkOrderFormHistory)
	return router
}

func workOrderScopedDB(t *testing.T) (*gorm.DB, models.GbChannel) {
	t.Helper()
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	channel := models.GbChannel{DeviceID: "ours", ChannelID: "own", OwnerDeptID: 10}
	require.NoError(t, db.Create(&channel).Error)
	return db, channel
}

const workOrderCompleteForm = `"form":{"projectName":"沪宁线放线作业","stationArea":"南京南—江宁","anchorSectionNo":"A-12","workLeader":"张伟","workPersonnel":["张伟","李强"]}`

// "Fill the work order first, then record": an incomplete form must be rejected
// before the ledger or the recorder is touched.
func TestWorkOrderCreateRefusesAnIncompleteFormBeforeRecording(t *testing.T) {
	db, channel := workOrderScopedDB(t)
	stub := &workOrderAPIStub{}
	router := workOrderRouter(t, db, stub, 100)

	body := `{"requestId":"order-1","channelIds":[` + uintStr(channel.ID) + `],"form":{"projectName":"沪宁线放线作业"}}`
	response := workRequest(router, http.MethodPost, "/work-orders", body)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "站区")
	require.Contains(t, response.Body.String(), "锚段号")
	require.Contains(t, response.Body.String(), "作业负责人")
	require.Contains(t, response.Body.String(), "作业人员")
	require.Empty(t, stub.starts, "an incomplete form must not reach the recorder")
}

func TestWorkOrderCreateDelegatesOnceTheFormIsComplete(t *testing.T) {
	db, channel := workOrderScopedDB(t)
	stub := &workOrderAPIStub{}
	router := workOrderRouter(t, db, stub, 100)

	body := `{"requestId":"order-1","channelIds":[` + uintStr(channel.ID) + `],` + workOrderCompleteForm + `}`
	response := workRequest(router, http.MethodPost, "/work-orders", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, stub.starts, 1)
	require.Equal(t, "沪宁线放线作业", stub.starts[0].Form.ProjectName)
	require.Equal(t, []string{"张伟", "李强"}, stub.starts[0].Form.WorkPersonnel)
	require.Equal(t, []uint{channel.ID}, stub.starts[0].ChannelIDs)
}

func TestWorkOrderCreateRejectsUnknownFieldsAndInvisibleChannels(t *testing.T) {
	db, channel := workOrderScopedDB(t)
	stub := &workOrderAPIStub{}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodPost, "/work-orders",
		`{"requestId":"order-1","channelIds":[`+uintStr(channel.ID)+`],`+workOrderCompleteForm+`,"extra":1}`)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Empty(t, stub.starts)

	// A channel owned by another department must not be reachable.
	foreign := models.GbChannel{DeviceID: "theirs", ChannelID: "foreign", OwnerDeptID: 99}
	require.NoError(t, db.Create(&foreign).Error)
	response = workRequest(router, http.MethodPost, "/work-orders",
		`{"requestId":"order-2","channelIds":[`+uintStr(foreign.ID)+`],`+workOrderCompleteForm+`}`)
	require.NotEqual(t, http.StatusOK, response.Code)
	require.Empty(t, stub.starts)
}

// The multi-screen page derives its recording button from this call, so "nothing
// running" has to be an explicit success with a null payload rather than an error.
func TestWorkOrderActiveReportsNothingRunningAsNull(t *testing.T) {
	db, _ := workOrderScopedDB(t)
	stub := &workOrderAPIStub{activeErr: gorm.ErrRecordNotFound}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodGet, "/work-orders/active", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.EqualValues(t, 0, body["code"])
	require.Nil(t, body["data"])
}

func TestWorkOrderActiveReturnsTheRunningOrder(t *testing.T) {
	db, _ := workOrderScopedDB(t)
	stub := &workOrderAPIStub{active: workrecording.BatchSnapshot{
		ID: "order-1", State: workrecording.StateRecording, ProjectName: "沪宁线放线作业",
	}}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodGet, "/work-orders/active", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "order-1")
	require.Contains(t, response.Body.String(), "沪宁线放线作业")
}

const (
	workOrderSettledID = "11111111-1111-1111-1111-111111111111"
	workOrderRunningID = "22222222-2222-2222-2222-222222222222"
)

// workOrderDeleteDB is the scoped DB plus the ledger tables the delete path
// reads to decide whether an order is still recording.
func workOrderDeleteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, _ := workOrderScopedDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbWorkRecordingBatch{}, &models.GbWorkRecording{}))
	require.NoError(t, db.Create(&models.GbWorkRecordingBatch{
		ID: workOrderSettledID, CreatedBy: 100, RequestID: "req-settled", State: workrecording.StateStopped, Version: 1, FormJSON: "{}",
	}).Error)
	return db
}

func TestWorkOrderDeleteForwardsTheSingleID(t *testing.T) {
	db := workOrderDeleteDB(t)
	stub := &workOrderAPIStub{result: workrecording.BatchDeleteResult{Deleted: 1}}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodDelete, "/work-orders/"+workOrderSettledID, "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, stub.deletes, 1)
	require.Equal(t, []string{workOrderSettledID}, stub.deletes[0].IDs)
}

func TestWorkOrderDeleteRefusesARunningOrder(t *testing.T) {
	db := workOrderDeleteDB(t)
	require.NoError(t, db.Create(&models.GbWorkRecordingBatch{
		ID: workOrderRunningID, CreatedBy: 100, RequestID: "req-running", State: workrecording.StateRecording, Version: 1, FormJSON: "{}",
	}).Error)
	stub := &workOrderAPIStub{result: workrecording.BatchDeleteResult{Skipped: []string{workOrderRunningID}}}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodDelete, "/work-orders/"+workOrderRunningID, "")
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "正在录制")
}

func TestWorkOrderDeleteReportsAnUnknownOrderAsNotFound(t *testing.T) {
	db := workOrderDeleteDB(t)
	stub := &workOrderAPIStub{}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodDelete, "/work-orders/"+workOrderRunningID, "")
	require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
}

// 别人创建的作业单不可见，必须走 404 而不是被删掉。
func TestWorkOrderDeleteHidesAnotherOperatorsOrder(t *testing.T) {
	db := workOrderDeleteDB(t)
	require.NoError(t, db.Create(&models.GbWorkRecordingBatch{
		ID: workOrderRunningID, CreatedBy: 200, RequestID: "req-other", State: workrecording.StateStopped, Version: 1, FormJSON: "{}",
	}).Error)
	stub := &workOrderAPIStub{result: workrecording.BatchDeleteResult{Deleted: 1}}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodDelete, "/work-orders/"+workOrderRunningID, "")
	require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	require.Empty(t, stub.deletes)
}

func TestWorkOrderBatchDeleteForwardsEverySelectedID(t *testing.T) {
	db := workOrderDeleteDB(t)
	stub := &workOrderAPIStub{result: workrecording.BatchDeleteResult{Deleted: 1, Skipped: []string{workOrderRunningID}}}
	router := workOrderRouter(t, db, stub, 100)

	body := `{"ids":["` + workOrderSettledID + `","` + workOrderRunningID + `"]}`
	response := workRequest(router, http.MethodPost, "/work-orders/batch-delete", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Len(t, stub.deletes, 1)
	require.Equal(t, []string{workOrderSettledID, workOrderRunningID}, stub.deletes[0].IDs)
	require.Contains(t, response.Body.String(), "skipped")
}

func TestWorkOrderBatchDeleteRejectsMalformedBodies(t *testing.T) {
	db := workOrderDeleteDB(t)
	stub := &workOrderAPIStub{result: workrecording.BatchDeleteResult{Deleted: 1}}
	router := workOrderRouter(t, db, stub, 100)

	for _, body := range []string{`{"ids":[]}`, `{"ids":["not-a-uuid"]}`, `{"ids":["` + workOrderSettledID + `"],"extra":1}`, `{}`} {
		response := workRequest(router, http.MethodPost, "/work-orders/batch-delete", body)
		require.Equal(t, http.StatusBadRequest, response.Code, body)
	}
	require.Empty(t, stub.deletes, "非法请求不得触达删除逻辑")
}

// 表单历史值接口：field 必须在 4 个白名单内、limit 可选，items 透传 service。
// 非法 field 必须 400 防越权读取任意字符串作为「字段」查库。
func TestWorkOrderFormHistoryRejectsOutOfWhitelistField(t *testing.T) {
	db, _ := workOrderScopedDB(t)
	stub := &workOrderAPIStub{historyByField: map[string][]workrecording.FormHistoryEntry{}}
	router := workOrderRouter(t, db, stub, 100)

	for _, bad := range []string{"", "projectId", "userId", "formJson", "deleted_at", "id"} {
		response := workRequest(router, http.MethodGet, "/work-orders/form-history?field="+bad, "")
		t.Logf("field=%q body=%s code=%d", bad, response.Body.String(), response.Code)
		require.Equal(t, http.StatusBadRequest, response.Code, "field=%q 必须 400", bad)
	}
}

func TestWorkOrderFormHistoryReturnsItemsByField(t *testing.T) {
	db, _ := workOrderScopedDB(t)
	stub := &workOrderAPIStub{
		historyByField: map[string][]workrecording.FormHistoryEntry{
			"projectName": {
				{Value: "沪宁线放线作业", UseCount: 12},
				{Value: "京沪高铁维护", UseCount: 5},
			},
		},
	}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodGet, "/work-orders/form-history?field=projectName", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "沪宁线放线作业")
	require.Contains(t, response.Body.String(), "京沪高铁维护")
}

// service 端其他错误（DB 抖动）必须 500，让前端可以识别「服务端异常」并重试。
func TestWorkOrderFormHistorySurfacesServiceError(t *testing.T) {
	db, _ := workOrderScopedDB(t)
	stub := &workOrderAPIStub{historyErr: errors.New("db unreachable")}
	router := workOrderRouter(t, db, stub, 100)

	response := workRequest(router, http.MethodGet, "/work-orders/form-history?field=projectName", "")
	require.Equal(t, http.StatusInternalServerError, response.Code, "未知错误应返回 500 便于前端做重试/上报")
}
