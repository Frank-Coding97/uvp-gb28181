package controllers

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type WorkRecordingAPI interface {
	Start(context.Context, uint, workrecording.StartRequest, int) (workrecording.Snapshot, error)
	Stop(context.Context, string) (workrecording.Snapshot, error)
	Get(context.Context, string) (workrecording.Snapshot, error)
}
type WorkRecordingBatchAPI interface {
	Start(context.Context, uint, workrecording.BatchStartRequest) (workrecording.BatchSnapshot, error)
	Stop(context.Context, string) (workrecording.BatchSnapshot, error)
	Get(context.Context, string) (workrecording.BatchSnapshot, error)
	List(context.Context, uint, int, int) ([]workrecording.BatchSnapshot, int64, error)
}
type WorkRecordingController struct {
	controllers.Common
	service      WorkRecordingAPI
	batchService WorkRecordingBatchAPI
	db           func() *gorm.DB
}

func NewWorkRecordingController(service WorkRecordingAPI) *WorkRecordingController {
	return &WorkRecordingController{service: service, db: func() *gorm.DB { return app.DB() }}
}
func (c *WorkRecordingController) SetDB(provider func() *gorm.DB) { c.db = provider }
func (c *WorkRecordingController) SetBatchService(service WorkRecordingBatchAPI) {
	c.batchService = service
}
func (c *WorkRecordingController) ready(ctx *gin.Context) bool {
	if c.GetCurrentUserID(ctx) == 0 {
		workFailure(ctx, http.StatusUnauthorized, "请先登录")
		return false
	}
	if c.service == nil || c.db() == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业录像服务未就绪")
		return false
	}
	return true
}
func (c *WorkRecordingController) visibleChannel(ctx *gin.Context, id uint) bool {
	var channel models.GbChannel
	query := c.db().WithContext(ctx).Scopes(visibleScope(ctx)).Select("id").First(&channel, id)
	err := query.Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && query.RowsAffected == 0) {
		workFailure(ctx, http.StatusNotFound, "通道不存在")
		return false
	}
	if err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "通道权限查询失败")
		return false
	}
	return true
}
func (c *WorkRecordingController) Start(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var request workrecording.StartRequest
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 2048))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		workFailure(ctx, http.StatusBadRequest, "开始参数不合法")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		workFailure(ctx, http.StatusBadRequest, "开始参数不合法")
		return
	}
	if err := request.Validate(); err != nil {
		workFailure(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if !c.visibleChannel(ctx, request.ChannelID) {
		return
	}
	snapshot, err := c.service.Start(ctx.Request.Context(), c.GetCurrentUserID(ctx), request, 0)
	c.result(ctx, snapshot, err)
}

func (c *WorkRecordingController) StartBatch(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "录像批次服务未就绪")
		return
	}
	var request workrecording.BatchStartRequest
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		workFailure(ctx, http.StatusBadRequest, "录像批次参数不合法")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		workFailure(ctx, http.StatusBadRequest, "录像批次参数不合法")
		return
	}
	if err := request.Validate(); err != nil {
		workFailure(ctx, http.StatusBadRequest, err.Error())
		return
	}
	for _, channelID := range request.ChannelIDs {
		if !c.visibleChannel(ctx, channelID) {
			return
		}
	}
	snapshot, err := service.Start(ctx.Request.Context(), c.GetCurrentUserID(ctx), request)
	c.batchResult(ctx, snapshot, err)
}

func (c *WorkRecordingController) BatchDetail(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	if !c.visibleBatch(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "录像批次服务未就绪")
		return
	}
	snapshot, err := service.Get(ctx.Request.Context(), ctx.Param("batchId"))
	if err == nil {
		c.Success(ctx, snapshot)
		return
	}
	c.batchFailure(ctx, err)
}

func (c *WorkRecordingController) BatchList(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "录像批次服务未就绪")
		return
	}
	page, pageSize, valid := workRecordingListPagination(ctx)
	if !valid {
		workFailure(ctx, http.StatusBadRequest, "分页参数不合法")
		return
	}
	items, total, err := service.List(ctx.Request.Context(), c.GetCurrentUserID(ctx), page, pageSize)
	if err != nil {
		c.batchFailure(ctx, err)
		return
	}
	c.Success(ctx, gin.H{"items": items, "total": total, "page": page, "pageSize": pageSize})
}

func (c *WorkRecordingController) StopBatch(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	if !c.visibleBatch(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "录像批次服务未就绪")
		return
	}
	snapshot, err := service.Stop(ctx.Request.Context(), ctx.Param("batchId"))
	c.batchResult(ctx, snapshot, err)
}

func (c *WorkRecordingController) visibleBatch(ctx *gin.Context) bool {
	var batch models.GbWorkRecordingBatch
	query := c.db().WithContext(ctx).Where("id = ? AND created_by = ?", ctx.Param("batchId"), c.GetCurrentUserID(ctx)).First(&batch)
	if query.Error != nil || query.RowsAffected == 0 {
		if errors.Is(query.Error, gorm.ErrRecordNotFound) || query.RowsAffected == 0 {
			workFailure(ctx, http.StatusNotFound, "台账不存在")
		} else {
			workFailure(ctx, http.StatusServiceUnavailable, "台账查询失败")
		}
		return false
	}
	var jobs []models.GbWorkRecording
	if err := c.db().WithContext(ctx).Where("batch_id = ?", batch.ID).Find(&jobs).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "台账录像查询失败")
		return false
	}
	for _, job := range jobs {
		if !c.visibleChannel(ctx, job.ChannelID) {
			return false
		}
	}
	return true
}

// BatchDownload streams all completed local MP4 slices for one ledger batch
// as one ZIP. The source files stay on the recording node after download.
func (c *WorkRecordingController) BatchDownload(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var batch models.GbWorkRecordingBatch
	result := c.db().WithContext(ctx).Where("id = ? AND created_by = ?", ctx.Param("batchId"), c.GetCurrentUserID(ctx)).First(&batch)
	if result.Error != nil || result.RowsAffected == 0 {
		workFailure(ctx, http.StatusNotFound, "台账不存在")
		return
	}
	var jobs []models.GbWorkRecording
	if err := c.db().WithContext(ctx).Where("batch_id = ?", batch.ID).Order("channel_id ASC").Find(&jobs).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "查询台账录像失败")
		return
	}
	if len(jobs) == 0 || len(jobs) > workrecording.BatchMaxCameraCount {
		workFailure(ctx, http.StatusConflict, "录像批次没有有效的摄像头记录")
		return
	}
	for _, job := range jobs {
		if !c.visibleChannel(ctx, job.ChannelID) {
			return
		}
		if job.State != workrecording.StateStopped {
			workFailure(ctx, http.StatusConflict, "录像尚未全部结束")
			return
		}
	}
	type zipFile struct {
		job  models.GbWorkRecording
		file models.GbRecordingFile
		name string
	}
	files := make([]zipFile, 0)
	for _, job := range jobs {
		var links []models.GbWorkRecordingFile
		if err := c.db().WithContext(ctx).Where("work_recording_id = ?", job.ID).Find(&links).Error; err != nil {
			workFailure(ctx, http.StatusServiceUnavailable, "查询录像分片失败")
			return
		}
		for _, link := range links {
			var file models.GbRecordingFile
			if err := c.db().WithContext(ctx).First(&file, link.FileID).Error; err != nil {
				workFailure(ctx, http.StatusConflict, "录像分片尚未归档")
				return
			}
			if file.MissingAt != nil || file.MetadataState != models.RecordingMetadataComplete || !localWorkFile(job, file.FilePath) {
				workFailure(ctx, http.StatusConflict, "存在未完成或不可访问的录像分片")
				return
			}
			files = append(files, zipFile{job: job, file: file, name: zipEntryName(job.ChannelID, file.FileName, len(files))})
		}
	}
	if len(files) == 0 {
		workFailure(ctx, http.StatusConflict, "台账尚无可下载录像")
		return
	}
	ctx.Header("Content-Type", "application/zip")
	ctx.Header("Content-Disposition", `attachment; filename="work-recording-`+batch.ID+`.zip"`)
	zw := zip.NewWriter(ctx.Writer)
	for _, item := range files {
		src, err := os.Open(item.file.FilePath)
		if err != nil {
			_ = zw.Close()
			workFailure(ctx, http.StatusBadGateway, "读取录像分片失败")
			return
		}
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		dst, err := zw.CreateHeader(header)
		if err == nil {
			_, err = io.Copy(dst, src)
		}
		_ = src.Close()
		if err != nil {
			_ = zw.Close()
			return
		}
	}
	if err := zw.Close(); err != nil {
		return
	}
}

func (c *WorkRecordingController) BatchFile(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var batch models.GbWorkRecordingBatch
	result := c.db().WithContext(ctx).Where("id = ? AND created_by = ?", ctx.Param("batchId"), c.GetCurrentUserID(ctx)).First(&batch)
	if result.Error != nil || result.RowsAffected == 0 {
		workFailure(ctx, http.StatusNotFound, "台账不存在")
		return
	}
	fileID, err := strconv.ParseUint(ctx.Param("fileId"), 10, 64)
	if err != nil || fileID == 0 {
		workFailure(ctx, http.StatusBadRequest, "录像文件不合法")
		return
	}
	var file models.GbRecordingFile
	query := c.db().WithContext(ctx).Table("gb_recording_file").Joins("JOIN gb_work_recording_file ON gb_work_recording_file.file_id = gb_recording_file.id").Joins("JOIN gb_work_recording ON gb_work_recording.id = gb_work_recording_file.work_recording_id").Where("gb_recording_file.id = ? AND gb_work_recording.batch_id = ?", fileID, batch.ID).First(&file)
	if query.Error != nil || query.RowsAffected == 0 {
		workFailure(ctx, http.StatusNotFound, "录像文件不存在")
		return
	}
	var job models.GbWorkRecording
	if err := c.db().WithContext(ctx).First(&job, "id = (SELECT work_recording_id FROM gb_work_recording_file WHERE file_id = ?)", fileID).Error; err != nil || !localWorkFile(job, file.FilePath) {
		workFailure(ctx, http.StatusConflict, "录像文件不可访问")
		return
	}
	info, err := os.Stat(file.FilePath)
	if err != nil || info.IsDir() {
		workFailure(ctx, http.StatusNotFound, "录像文件尚未生成")
		return
	}
	ctx.Header("Content-Type", "video/mp4")
	ctx.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filepath.Base(file.FileName)))
	f, err := os.Open(file.FilePath)
	if err != nil {
		workFailure(ctx, http.StatusBadGateway, "读取录像文件失败")
		return
	}
	defer f.Close()
	http.ServeContent(ctx.Writer, ctx.Request, filepath.Base(file.FileName), info.ModTime().In(time.UTC), f)
}

func localWorkFile(job models.GbWorkRecording, filePath string) bool {
	root, file := filepath.Clean(job.RecordingRoot), filepath.Clean(filePath)
	if root == "." || file == "." || root == file {
		return false
	}
	rel, err := filepath.Rel(root, file)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !strings.ContainsRune(rel, 0)
}

func zipEntryName(channelID uint, fileName string, index int) string {
	base := filepath.Base(fileName)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = fmt.Sprintf("slice-%d.mp4", index+1)
	}
	return fmt.Sprintf("camera-%d/%s", channelID, base)
}
func (c *WorkRecordingController) job(ctx *gin.Context) (*models.GbWorkRecording, bool) {
	var job models.GbWorkRecording
	query := c.db().WithContext(ctx).First(&job, "id = ?", ctx.Param("id"))
	if err := query.Error; err != nil || query.RowsAffected == 0 {
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil {
			workFailure(ctx, http.StatusNotFound, "作业不存在")
		} else {
			workFailure(ctx, http.StatusServiceUnavailable, "作业查询失败")
		}
		return nil, false
	}
	if !c.visibleChannel(ctx, job.ChannelID) {
		return nil, false
	}
	return &job, true
}
func (c *WorkRecordingController) Stop(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	job, ok := c.job(ctx)
	if !ok {
		return
	}
	// Until a cross-user policy is agreed, a work job is stopped by its initiator.
	if job.CreatedBy != c.GetCurrentUserID(ctx) {
		workFailure(ctx, http.StatusForbidden, "仅作业发起人可以停止录像")
		return
	}
	snapshot, err := c.service.Stop(ctx.Request.Context(), job.ID)
	c.result(ctx, snapshot, err)
}
func (c *WorkRecordingController) Detail(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	job, ok := c.job(ctx)
	if !ok {
		return
	}
	snapshot, err := c.service.Get(ctx.Request.Context(), job.ID)
	c.result(ctx, snapshot, err)
}
func (c *WorkRecordingController) Status(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	values := strings.Split(ctx.Query("channelIds"), ",")
	if len(values) == 0 || len(values) > 64 {
		workFailure(ctx, http.StatusBadRequest, "通道数量不合法")
		return
	}
	ids := make([]uint, 0, len(values))
	seen := map[uint]bool{}
	for _, value := range values {
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil || parsed == 0 {
			workFailure(ctx, http.StatusBadRequest, "通道ID不合法")
			return
		}
		id := uint(parsed)
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	var channels []models.GbChannel
	if err := c.db().WithContext(ctx).Scopes(visibleScope(ctx)).Select("id", "cloud_recording_enabled").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "通道权限查询失败")
		return
	}
	if len(channels) != len(ids) {
		workFailure(ctx, http.StatusNotFound, "通道不存在")
		return
	}
	cloudRecordingEnabled := make(map[uint]bool, len(channels))
	for _, channel := range channels {
		cloudRecordingEnabled[channel.ID] = channel.CloudRecordingEnabled
	}
	snapshots := make([]workrecording.Snapshot, 0, len(ids))
	for _, id := range ids {
		var claim models.GbRecorderClaim
		query := c.db().WithContext(ctx).First(&claim, "resource_key = ?", workrecording.ChannelResource(id))
		err := query.Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			workFailure(ctx, http.StatusServiceUnavailable, "录像状态查询失败")
			return
		}
		snapshot := workrecording.Snapshot{ChannelID: id, State: workrecording.StateIdle}
		if err == nil && query.RowsAffected > 0 && claim.State != workrecording.StateIdle {
			snapshot.State = workrecording.StateUnknown
			snapshot.Version = claim.Version
			snapshot.LastError = "该通道存在其他录像占用，请先核实原录像状态"
			if claim.OwnerKind == workrecording.OwnerLegacy && cloudRecordingEnabled[id] {
				snapshot.LastError = "该通道已启用云端连续录像，请先关闭云端录像再开始作业录像"
			}
			if claim.OwnerKind == workrecording.OwnerWork {
				snapshot, err = c.service.Get(ctx.Request.Context(), claim.OwnerID)
				if err != nil || snapshot.ChannelID != id || snapshot.ID != claim.OwnerID {
					workFailure(ctx, http.StatusServiceUnavailable, "作业状态待核实")
					return
				}
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	c.Success(ctx, snapshots)
}
func (c *WorkRecordingController) result(ctx *gin.Context, snapshot workrecording.Snapshot, err error) {
	if err == nil {
		c.Success(ctx, snapshot)
		return
	}
	status := http.StatusServiceUnavailable
	if errors.Is(err, workrecording.ErrInvalidRequest) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, workrecording.ErrOwnerConflict) || errors.Is(err, workrecording.ErrVersionConflict) || errors.Is(err, workrecording.ErrRequestConflict) || errors.Is(err, workrecording.ErrAttributionUnknown) {
		status = http.StatusConflict
	}
	ctx.JSON(status, gin.H{"code": status, "message": "作业录像操作未完成，请查询当前状态", "data": snapshot})
}

func (c *WorkRecordingController) batchResult(ctx *gin.Context, snapshot workrecording.BatchSnapshot, err error) {
	if err == nil {
		c.Success(ctx, snapshot)
		return
	}
	c.batchFailure(ctx, err, snapshot)
}

func (c *WorkRecordingController) batchFailure(ctx *gin.Context, err error, snapshots ...workrecording.BatchSnapshot) {
	status := http.StatusServiceUnavailable
	if errors.Is(err, workrecording.ErrBatchInvalid) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, workrecording.ErrOwnerConflict) || errors.Is(err, workrecording.ErrBatchIncomplete) || errors.Is(err, workrecording.ErrAttributionUnknown) {
		status = http.StatusConflict
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	response := gin.H{"code": status, "message": "录像批次操作未完成，请查询台账状态"}
	if len(snapshots) > 0 && snapshots[0].ID != "" {
		response["data"] = snapshots[0]
	}
	ctx.JSON(status, response)
}
func workFailure(ctx *gin.Context, status int, message string) {
	ctx.JSON(status, gin.H{"code": status, "message": message})
}
