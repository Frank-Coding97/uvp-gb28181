package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
)

type CloudRecordingCatalogAPI interface {
	ListFiles(context.Context, uint, gbrecording.FileQuery) (gbrecording.FileDTOPage, error)
	FileOptions(context.Context, uint, gbrecording.FileQuery) (gbrecording.CatalogOptionsDTO, error)
	FileDetail(context.Context, uint, uint64) (gbrecording.FileDTO, error)
	IssueAccess(context.Context, uint, uint64, string) (gbrecording.AccessDTO, error)
	ActiveSessions(context.Context, uint) ([]gbrecording.ActiveSessionDTO, error)
	Reconciliations(context.Context) ([]gbrecording.ReconciliationDTO, error)
	TriggerReconciliation([]int64, *time.Time, *time.Time) ([]int64, error)
	StreamContent(context.Context, http.ResponseWriter, string, string, string) error
}

type CloudRecordingCatalogController struct{ service CloudRecordingCatalogAPI }

func NewCloudRecordingCatalogController(service CloudRecordingCatalogAPI) *CloudRecordingCatalogController {
	return &CloudRecordingCatalogController{service: service}
}

func (c *CloudRecordingCatalogController) ListFiles(ctx *gin.Context) {
	query, err := bindCatalogFileQuery(ctx)
	if err != nil {
		catalogFailure(ctx, http.StatusBadRequest, "查询参数不合法")
		return
	}
	result, err := c.requireService().ListFiles(ctx.Request.Context(), common.GetCurrentUserID(ctx), query)
	if err != nil {
		respondCatalogError(ctx, err)
		return
	}
	catalogSuccess(ctx, http.StatusOK, result)
}

func (c *CloudRecordingCatalogController) FileOptions(ctx *gin.Context) {
	query, err := bindCatalogFileQuery(ctx)
	if err != nil {
		catalogFailure(ctx, http.StatusBadRequest, "查询参数不合法")
		return
	}
	result, err := c.requireService().FileOptions(ctx.Request.Context(), common.GetCurrentUserID(ctx), query)
	if err != nil {
		respondCatalogError(ctx, err)
		return
	}
	catalogSuccess(ctx, http.StatusOK, result)
}

func (c *CloudRecordingCatalogController) FileDetail(ctx *gin.Context) {
	id, ok := catalogFileID(ctx)
	if !ok {
		catalogFailure(ctx, http.StatusBadRequest, "文件 ID 不合法")
		return
	}
	result, err := c.requireService().FileDetail(ctx.Request.Context(), common.GetCurrentUserID(ctx), id)
	if err != nil {
		respondCatalogError(ctx, err)
		return
	}
	catalogSuccess(ctx, http.StatusOK, result)
}

func (c *CloudRecordingCatalogController) IssueAccess(ctx *gin.Context) {
	id, ok := catalogFileID(ctx)
	if !ok {
		catalogFailure(ctx, http.StatusBadRequest, "文件 ID 不合法")
		return
	}
	var request struct {
		Mode string `json:"mode"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil || (request.Mode != gbrecording.CapabilityModePlay && request.Mode != gbrecording.CapabilityModeDownload) {
		catalogFailure(ctx, http.StatusBadRequest, "访问模式不合法")
		return
	}
	result, err := c.requireService().IssueAccess(ctx.Request.Context(), common.GetCurrentUserID(ctx), id, request.Mode)
	if err != nil {
		respondCatalogError(ctx, err)
		return
	}
	catalogSuccess(ctx, http.StatusOK, result)
}

func (c *CloudRecordingCatalogController) ActiveSessions(ctx *gin.Context) {
	result, err := c.requireService().ActiveSessions(ctx.Request.Context(), common.GetCurrentUserID(ctx))
	if err != nil {
		respondCatalogError(ctx, err)
		return
	}
	catalogSuccess(ctx, http.StatusOK, gin.H{"list": result})
}

func (c *CloudRecordingCatalogController) Reconciliations(ctx *gin.Context) {
	result, err := c.requireService().Reconciliations(ctx.Request.Context())
	if err != nil {
		respondCatalogError(ctx, err)
		return
	}
	catalogSuccess(ctx, http.StatusOK, gin.H{"list": result})
}

func (c *CloudRecordingCatalogController) TriggerReconciliation(ctx *gin.Context) {
	var request struct {
		NodeIDs []int64 `json:"nodeIds"`
		Start   string  `json:"start"`
		End     string  `json:"end"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		catalogFailure(ctx, http.StatusBadRequest, "对账参数不合法")
		return
	}
	start, err := parseCatalogTime(request.Start)
	if err != nil {
		catalogFailure(ctx, http.StatusBadRequest, "对账开始时间不合法")
		return
	}
	end, err := parseCatalogTime(request.End)
	if err != nil || (start != nil && end != nil && !start.Before(*end)) {
		catalogFailure(ctx, http.StatusBadRequest, "对账结束时间不合法")
		return
	}
	accepted, err := c.requireService().TriggerReconciliation(request.NodeIDs, start, end)
	if err != nil {
		respondCatalogError(ctx, err)
		return
	}
	catalogSuccess(ctx, http.StatusAccepted, gin.H{"acceptedNodeIds": accepted})
}

func (c *CloudRecordingCatalogController) Content(ctx *gin.Context) {
	fileID := ctx.Param("id")
	capability := ctx.Query("cap")
	if fileID == "" || capability == "" {
		catalogFailure(ctx, http.StatusForbidden, "访问凭据无效")
		return
	}
	err := c.requireService().StreamContent(ctx.Request.Context(), ctx.Writer, fileID, capability, ctx.GetHeader("Range"))
	if err == nil || ctx.Writer.Written() {
		return
	}
	respondCatalogError(ctx, err)
}

func (c *CloudRecordingCatalogController) requireService() CloudRecordingCatalogAPI {
	if c != nil && c.service != nil {
		return c.service
	}
	return unavailableCloudRecordingCatalogService{}
}

type unavailableCloudRecordingCatalogService struct{}

func (unavailableCloudRecordingCatalogService) ListFiles(context.Context, uint, gbrecording.FileQuery) (gbrecording.FileDTOPage, error) {
	return gbrecording.FileDTOPage{}, gbrecording.ErrCatalogAccessUnavailable
}
func (unavailableCloudRecordingCatalogService) FileOptions(context.Context, uint, gbrecording.FileQuery) (gbrecording.CatalogOptionsDTO, error) {
	return gbrecording.CatalogOptionsDTO{}, gbrecording.ErrCatalogAccessUnavailable
}
func (unavailableCloudRecordingCatalogService) FileDetail(context.Context, uint, uint64) (gbrecording.FileDTO, error) {
	return gbrecording.FileDTO{}, gbrecording.ErrCatalogAccessUnavailable
}
func (unavailableCloudRecordingCatalogService) IssueAccess(context.Context, uint, uint64, string) (gbrecording.AccessDTO, error) {
	return gbrecording.AccessDTO{}, gbrecording.ErrCatalogAccessUnavailable
}
func (unavailableCloudRecordingCatalogService) ActiveSessions(context.Context, uint) ([]gbrecording.ActiveSessionDTO, error) {
	return nil, gbrecording.ErrCatalogAccessUnavailable
}
func (unavailableCloudRecordingCatalogService) Reconciliations(context.Context) ([]gbrecording.ReconciliationDTO, error) {
	return nil, gbrecording.ErrCatalogAccessUnavailable
}
func (unavailableCloudRecordingCatalogService) TriggerReconciliation([]int64, *time.Time, *time.Time) ([]int64, error) {
	return nil, gbrecording.ErrCatalogAccessUnavailable
}
func (unavailableCloudRecordingCatalogService) StreamContent(context.Context, http.ResponseWriter, string, string, string) error {
	return gbrecording.ErrCatalogAccessUnavailable
}

func bindCatalogFileQuery(ctx *gin.Context) (gbrecording.FileQuery, error) {
	query := gbrecording.FileQuery{Page: 1, PageSize: 20}
	var err error
	if value := ctx.Query("page"); value != "" {
		query.Page, err = strconv.Atoi(value)
		if err != nil || query.Page <= 0 {
			return query, errors.New("invalid page")
		}
	}
	if value := ctx.Query("pageSize"); value != "" {
		query.PageSize, err = strconv.Atoi(value)
		if err != nil || query.PageSize <= 0 || query.PageSize > 200 {
			return query, errors.New("invalid page size")
		}
	}
	if value := ctx.Query("channelId"); value != "" {
		parsed, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil || parsed == 0 || parsed > uint64(^uint(0)) {
			return query, errors.New("invalid channel")
		}
		channelID := uint(parsed)
		query.ChannelID = &channelID
	}
	if value := ctx.Query("nodeId"); value != "" {
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || parsed <= 0 {
			return query, errors.New("invalid node")
		}
		query.NodeID = &parsed
	}
	query.DeviceID = strings.TrimSpace(ctx.Query("deviceId"))
	query.Keyword = strings.TrimSpace(ctx.Query("keyword"))
	query.Availability = strings.TrimSpace(ctx.Query("availability"))
	query.MetadataState = strings.TrimSpace(ctx.Query("metadataState"))
	if query.Availability != "" && !validCatalogAvailability(query.Availability) {
		return query, errors.New("invalid availability")
	}
	if query.MetadataState != "" && query.MetadataState != "complete" && query.MetadataState != "partial" {
		return query, errors.New("invalid metadata state")
	}
	query.Start, err = parseCatalogTime(ctx.Query("start"))
	if err != nil {
		return query, err
	}
	query.End, err = parseCatalogTime(ctx.Query("end"))
	if err != nil || (query.Start != nil && query.End != nil && !query.Start.Before(*query.End)) {
		return query, errors.New("invalid time range")
	}
	return query, nil
}

func validCatalogAvailability(value string) bool {
	switch value {
	case gbrecording.AvailabilityAvailable, gbrecording.AvailabilityNodeOffline, gbrecording.AvailabilityNodeMissing,
		gbrecording.AvailabilityFileMissing, gbrecording.AvailabilityAccessUnavailable:
		return true
	default:
		return false
	}
}

func parseCatalogTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func catalogFileID(ctx *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	return id, err == nil && id != 0
}

func respondCatalogError(ctx *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "云端录像请求失败"
	switch {
	case errors.Is(err, gbrecording.ErrRecordingFileNotFound), errors.Is(err, gbrecording.ErrCatalogFileMissing), errors.Is(err, zlm.ErrRecordingNotFound):
		status, message = http.StatusNotFound, "录像文件不存在"
	case errors.Is(err, gbrecording.ErrCatalogNodeMissing), errors.Is(err, gbrecording.ErrCatalogNodeOffline), errors.Is(err, zlm.ErrRecordingNodeUnavailable):
		status, message = http.StatusServiceUnavailable, "录像节点当前不可用"
	case errors.Is(err, gbrecording.ErrCatalogAccessUnavailable), errors.Is(err, zlm.ErrRecordingAccessUnavailable), errors.Is(err, gbrecording.ErrContentUpstream), errors.Is(err, gbrecording.ErrContentRedirect):
		status, message = http.StatusBadGateway, "录像内容当前不可访问"
	case errors.Is(err, gbrecording.ErrCatalogAccessRevoked), errors.Is(err, gbrecording.ErrCapabilityInvalid):
		status, message = http.StatusForbidden, "录像访问权限已失效"
	case errors.Is(err, gbrecording.ErrCapabilityExpired):
		status, message = http.StatusGone, "录像访问凭据已过期"
	case errors.Is(err, gbrecording.ErrContentRangeInvalid):
		status, message = http.StatusRequestedRangeNotSatisfiable, "请求的录像范围无效"
	case errors.Is(err, gbrecording.ErrCatalogSchedulerStopped):
		status, message = http.StatusServiceUnavailable, "录像对账服务未就绪"
	}
	catalogFailure(ctx, status, message)
}

func catalogSuccess(ctx *gin.Context, status int, data any) {
	ctx.JSON(status, gin.H{"code": 0, "message": "", "data": data})
}

func catalogFailure(ctx *gin.Context, status int, message string) {
	ctx.JSON(status, gin.H{"code": 1, "message": message, "data": nil})
	ctx.Abort()
}
