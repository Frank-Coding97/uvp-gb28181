package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
)

const workRecordingFormMaxBodyBytes = 64 * 1024

type saveWorkRecordingFormRequest struct {
	FormVersion *uint64             `json:"formVersion"`
	Form        *workrecording.Form `json:"form"`
}

// Form returns a visible work recording's durable form. Visibility is based
// on the channel; editability additionally requires the current user to be
// the job creator and the row to remain a draft.
func (c *WorkRecordingController) Form(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	job, ok := c.job(ctx)
	if !ok {
		return
	}
	record, err := workrecording.GetForm(ctx.Request.Context(), c.db(), job.ID)
	if err != nil {
		workFormFailure(ctx, err, "作业表单查询失败")
		return
	}
	record.Editable = record.CreatedBy == c.GetCurrentUserID(ctx) && record.FormState == workrecording.FormDraft
	c.Success(ctx, record)
}

// SaveForm accepts only a versioned draft form. The repository performs the
// compare-and-swap and updates form columns without touching recording facts.
func (c *WorkRecordingController) SaveForm(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	job, ok := c.job(ctx)
	if !ok {
		return
	}
	if job.CreatedBy != c.GetCurrentUserID(ctx) {
		workFailure(ctx, http.StatusForbidden, "仅作业发起人可以编辑表单")
		return
	}

	request, err := decodeSaveWorkRecordingForm(ctx)
	if err != nil {
		workFailure(ctx, http.StatusBadRequest, workrecording.ErrFormInvalid.Error())
		return
	}
	record, err := workrecording.SaveDraft(ctx.Request.Context(), c.db(), job.ID, c.GetCurrentUserID(ctx), *request.FormVersion, *request.Form)
	if err != nil {
		workFormFailure(ctx, err, "作业表单保存失败")
		return
	}
	record.Editable = record.FormState == workrecording.FormDraft
	c.Success(ctx, record)
}

func decodeSaveWorkRecordingForm(ctx *gin.Context) (*saveWorkRecordingFormRequest, error) {
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, workRecordingFormMaxBodyBytes))
	decoder.DisallowUnknownFields()
	var request saveWorkRecordingFormRequest
	if err := decoder.Decode(&request); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return nil, errors.New("表单请求存在多余内容")
		}
		return nil, err
	}
	if request.FormVersion == nil || request.Form == nil {
		return nil, errors.New("表单请求缺少版本或内容")
	}
	return &request, nil
}

func workFormFailure(ctx *gin.Context, err error, fallback string) {
	status := http.StatusServiceUnavailable
	message := fallback
	switch {
	case errors.Is(err, workrecording.ErrFormInvalid):
		status = http.StatusBadRequest
		message = workrecording.ErrFormInvalid.Error()
	case errors.Is(err, workrecording.ErrFormNotFound):
		status = http.StatusNotFound
		message = "作业不存在"
	case errors.Is(err, workrecording.ErrFormForbidden):
		status = http.StatusForbidden
		message = "仅作业发起人可以编辑表单"
	case errors.Is(err, workrecording.ErrFormSubmitted), errors.Is(err, workrecording.ErrFormNotEditable), errors.Is(err, workrecording.ErrVersionConflict):
		status = http.StatusConflict
		message = err.Error()
	}
	workFailure(ctx, status, message)
}
