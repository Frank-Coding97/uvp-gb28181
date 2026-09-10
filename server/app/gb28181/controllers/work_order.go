package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

// 作业单是录制的唯一入口。旧的单通道 /work-recordings 与批次台账路由已删除，
// 这组处理器就是仅有的对外入口。
const workOrderRequestBodyLimit = 8192

// CreateWorkOrder is the "fill the work order first, then record" entry point.
// The form is validated before anything is claimed on the recorder.
func (c *WorkRecordingController) CreateWorkOrder(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业单服务未就绪")
		return
	}
	var request workrecording.OrderStartRequest
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, workOrderRequestBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		workFailure(ctx, http.StatusBadRequest, "作业单参数不合法")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		workFailure(ctx, http.StatusBadRequest, "作业单参数不合法")
		return
	}
	// Report the missing form fields verbatim: the operator has to know which
	// field kept the recording from starting.
	if err := request.Validate(); err != nil {
		workFailure(ctx, http.StatusBadRequest, err.Error())
		return
	}
	for _, channelID := range request.ChannelIDs {
		if !c.visibleChannel(ctx, channelID) {
			return
		}
	}
	snapshot, err := service.StartOrder(ctx.Request.Context(), c.GetCurrentUserID(ctx), request)
	c.batchResult(ctx, snapshot, err)
}

// ActiveWorkOrder lets the multi-screen page restore its recording button from
// the server instead of guessing from a local flag that a reload wipes out.
func (c *WorkRecordingController) ActiveWorkOrder(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业单服务未就绪")
		return
	}
	snapshot, err := service.Active(ctx.Request.Context(), c.GetCurrentUserID(ctx))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.Success(ctx, nil)
		return
	}
	if err != nil {
		c.batchFailure(ctx, err)
		return
	}
	c.Success(ctx, snapshot)
}

// DeleteWorkOrder removes one settled work order. Anything still recording is
// refused rather than silently cancelled: deleting a live row would strand the
// recorder claim without any entry left to stop it from.
func (c *WorkRecordingController) DeleteWorkOrder(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	if !c.visibleBatch(ctx) {
		return
	}
	c.deleteWorkOrders(ctx, []string{ctx.Param("id")})
}

// BatchDeleteWorkOrders removes the work orders the operator ticked in the list.
// Recording rows are skipped and reported, so one live row cannot block the
// deletion of everything else the operator selected.
func (c *WorkRecordingController) BatchDeleteWorkOrders(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var request workrecording.BatchDeleteRequest
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, workOrderRequestBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		workFailure(ctx, http.StatusBadRequest, "删除参数不合法")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		workFailure(ctx, http.StatusBadRequest, "删除参数不合法")
		return
	}
	if err := request.Validate(); err != nil {
		workFailure(ctx, http.StatusBadRequest, err.Error())
		return
	}
	c.deleteWorkOrders(ctx, request.IDs)
}

func (c *WorkRecordingController) deleteWorkOrders(ctx *gin.Context, ids []string) {
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业单服务未就绪")
		return
	}
	result, err := service.Delete(ctx.Request.Context(), c.GetCurrentUserID(ctx), workrecording.BatchDeleteRequest{IDs: ids})
	if err != nil {
		c.batchFailure(ctx, err)
		return
	}
	if result.Deleted == 0 {
		if len(result.Skipped) > 0 {
			workFailure(ctx, http.StatusConflict, "正在录制的作业单不能删除，请先结束录像")
			return
		}
		workFailure(ctx, http.StatusNotFound, "作业单不存在")
		return
	}
	c.Success(ctx, result)
}

// WorkOrderDetail returns the ledger together with the operator's form in one
// response, so the detail page does not have to stitch two calls together.
func (c *WorkRecordingController) WorkOrderDetail(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	if !c.visibleBatch(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业单服务未就绪")
		return
	}
	snapshot, err := service.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		c.batchFailure(ctx, err)
		return
	}
	payload := gin.H{"snapshot": snapshot}
	// The form is part of the work order, but a missing one must not sink the
	// whole detail view.
	if form, formErr := workrecording.GetBatchForm(ctx.Request.Context(), c.db(), ctx.Param("id"), c.GetCurrentUserID(ctx)); formErr == nil {
		payload["form"] = form
	}
	c.Success(ctx, payload)
}
