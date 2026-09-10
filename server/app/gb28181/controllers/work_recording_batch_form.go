package controllers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

type saveWorkRecordingBatchFormRequest struct {
	FormVersion *uint64             `json:"formVersion"`
	Form        *workrecording.Form `json:"form"`
}

func (c *WorkRecordingController) BatchForm(ctx *gin.Context) {
	if !c.ready(ctx) || !c.visibleBatch(ctx) {
		return
	}
	record, err := workrecording.GetBatchForm(ctx.Request.Context(), c.db(), ctx.Param("batchId"), c.GetCurrentUserID(ctx))
	if err != nil {
		workFormFailure(ctx, err, "台账表单查询失败")
		return
	}
	c.Success(ctx, record)
}

func (c *WorkRecordingController) SaveBatchForm(ctx *gin.Context) {
	if !c.ready(ctx) || !c.visibleBatch(ctx) {
		return
	}
	var request saveWorkRecordingBatchFormRequest
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, workRecordingFormMaxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		workFailure(ctx, http.StatusBadRequest, workrecording.ErrFormInvalid.Error())
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		workFailure(ctx, http.StatusBadRequest, workrecording.ErrFormInvalid.Error())
		return
	}
	if request.FormVersion == nil || request.Form == nil {
		workFailure(ctx, http.StatusBadRequest, workrecording.ErrFormInvalid.Error())
		return
	}
	record, err := workrecording.SaveBatchDraft(ctx.Request.Context(), c.db(), ctx.Param("batchId"), c.GetCurrentUserID(ctx), *request.FormVersion, *request.Form)
	if err != nil {
		workFormFailure(ctx, err, "台账表单保存失败")
		return
	}
	c.Success(ctx, record)
}
