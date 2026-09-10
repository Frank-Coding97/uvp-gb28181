package controllers

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	gbzlm "uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type WorkRecordingAPI interface {
	Start(context.Context, uint, workrecording.StartRequest, int) (workrecording.Snapshot, error)
	Stop(context.Context, string) (workrecording.Snapshot, error)
	Get(context.Context, string) (workrecording.Snapshot, error)
}
type WorkRecordingBatchAPI interface {
	StartOrder(context.Context, uint, workrecording.OrderStartRequest) (workrecording.BatchSnapshot, error)
	Stop(context.Context, string) (workrecording.BatchSnapshot, error)
	Get(context.Context, string) (workrecording.BatchSnapshot, error)
	List(context.Context, workrecording.BatchListFilter) ([]workrecording.BatchSnapshot, int64, error)
	Active(context.Context, uint) (workrecording.BatchSnapshot, error)
	Delete(context.Context, uint, workrecording.BatchDeleteRequest) (workrecording.BatchDeleteResult, error)
	ListFormHistory(context.Context, string, int) ([]workrecording.FormHistoryEntry, error)
}
type WorkRecordingController struct {
	controllers.Common
	service      WorkRecordingAPI
	batchService WorkRecordingBatchAPI
	db           func() *gorm.DB
	fileSources  WorkRecordingFileSourceFactory
}

func NewWorkRecordingController(service WorkRecordingAPI) *WorkRecordingController {
	return &WorkRecordingController{service: service, db: func() *gorm.DB { return app.DB() }}
}
func (c *WorkRecordingController) SetDB(provider func() *gorm.DB) { c.db = provider }
func (c *WorkRecordingController) SetBatchService(service WorkRecordingBatchAPI) {
	c.batchService = service
}

// workRecordingContentProxy streams one recorded slice from the node that stores
// it, forwarding the node's own headers so range playback keeps working. The
// proxy carries no per-request state, so one instance serves every request.
var workRecordingContentProxy = gbrecording.NewContentProxy()

// WorkRecordingFileSource is the file-serving endpoint of the ZLM node that
// stores a recording. A work recording lives on that node's filesystem, so the
// control process can only read one by asking the node to serve it: opening the
// stored path locally fails for every remote node, which is the normal
// deployment and not a degraded one.
type WorkRecordingFileSource interface {
	DownloadFile(ctx context.Context, filePath, byteRange string) (*gbzlm.DownloadResponse, error)
}

// WorkRecordingFileSourceFactory resolves the node that stores a recording. It
// reports false when the node is unknown or currently unreachable.
type WorkRecordingFileSourceFactory func(nodeID int64) (WorkRecordingFileSource, bool)

// SetFileSourceFactory installs the node resolver used by every file download.
func (c *WorkRecordingController) SetFileSourceFactory(factory WorkRecordingFileSourceFactory) {
	c.fileSources = factory
}

func (c *WorkRecordingController) fileSource(nodeID int64) (WorkRecordingFileSource, bool) {
	if c == nil || c.fileSources == nil || nodeID <= 0 {
		return nil, false
	}
	return c.fileSources(nodeID)
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
func (c *WorkRecordingController) WorkOrderList(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	service, ok := c.batchService.(WorkRecordingBatchAPI)
	if !ok || service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业单服务未就绪")
		return
	}
	page, pageSize, valid := workRecordingListPagination(ctx)
	if !valid {
		workFailure(ctx, http.StatusBadRequest, "分页参数不合法")
		return
	}
	filter, ok := c.batchListFilterFromQuery(ctx, page, pageSize)
	if !ok {
		return
	}
	items, total, err := service.List(ctx.Request.Context(), filter)
	if err != nil {
		c.batchFailure(ctx, err)
		return
	}
	c.Success(ctx, gin.H{"items": items, "total": total, "page": page, "pageSize": pageSize})
}

// batchListFilterFromQuery reads the ledger list filters shared by the legacy
// /work-recordings/batches listing and the /work-orders listing, so both stay
// consistent as the legacy route is retired.
func (c *WorkRecordingController) batchListFilterFromQuery(ctx *gin.Context, page, pageSize int) (workrecording.BatchListFilter, bool) {
	filter := workrecording.BatchListFilter{
		Actor: c.GetCurrentUserID(ctx), Page: page, PageSize: pageSize,
		Keyword: strings.TrimSpace(ctx.Query("keyword")),
	}
	if states := strings.TrimSpace(ctx.Query("state")); states != "" {
		for _, state := range strings.Split(states, ",") {
			if trimmed := strings.TrimSpace(state); trimmed != "" {
				filter.States = append(filter.States, trimmed)
			}
		}
	}
	if raw := strings.TrimSpace(ctx.Query("channelId")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			workFailure(ctx, http.StatusBadRequest, "通道参数不合法")
			return workrecording.BatchListFilter{}, false
		}
		filter.ChannelID = uint(parsed)
	}
	for _, spec := range []struct {
		name   string
		target **time.Time
	}{{"startTime", &filter.From}, {"endTime", &filter.To}} {
		raw := strings.TrimSpace(ctx.Query(spec.name))
		if raw == "" {
			continue
		}
		parsed, err := parseWorkRecordingTime(raw)
		if err != nil {
			workFailure(ctx, http.StatusBadRequest, "时间参数不合法")
			return workrecording.BatchListFilter{}, false
		}
		*spec.target = &parsed
	}
	return filter, true
}

var workRecordingTimeLayouts = []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}

func parseWorkRecordingTime(value string) (time.Time, error) {
	for _, layout := range workRecordingTimeLayouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("无法解析的时间格式")
}

func (c *WorkRecordingController) StopWorkOrder(ctx *gin.Context) {
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
	snapshot, err := service.Stop(ctx.Request.Context(), ctx.Param("id"))
	c.batchResult(ctx, snapshot, err)
}

func (c *WorkRecordingController) visibleBatch(ctx *gin.Context) bool {
	var batch models.GbWorkRecordingBatch
	query := c.db().WithContext(ctx).Where("id = ? AND created_by = ?", ctx.Param("id"), c.GetCurrentUserID(ctx)).First(&batch)
	if query.Error != nil || query.RowsAffected == 0 {
		if errors.Is(query.Error, gorm.ErrRecordNotFound) || query.RowsAffected == 0 {
			workFailure(ctx, http.StatusNotFound, "作业单不存在")
		} else {
			workFailure(ctx, http.StatusServiceUnavailable, "作业单查询失败")
		}
		return false
	}
	var jobs []models.GbWorkRecording
	if err := c.db().WithContext(ctx).Where("batch_id = ?", batch.ID).Find(&jobs).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业单录像查询失败")
		return false
	}
	for _, job := range jobs {
		if !c.visibleChannel(ctx, job.ChannelID) {
			return false
		}
	}
	return true
}

// BatchDownload streams all completed local MP4 slices for one work ledger
// batch as a single stored ZIP. MP4 slices are already compressed, so deflating
// them only burns CPU and slows the transfer. Every slice is stat-ed before the
// response headers go out: a file that vanished from the recording node after it
// was archived has to fail the whole request with a bounded error, not truncate
// an archive the operator only discovers is broken after the transfer ends. The
// source files stay on the recording node after download.
func (c *WorkRecordingController) WorkOrderDownload(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var batch models.GbWorkRecordingBatch
	result := c.db().WithContext(ctx).Where("id = ? AND created_by = ?", ctx.Param("id"), c.GetCurrentUserID(ctx)).First(&batch)
	if result.Error != nil || result.RowsAffected == 0 {
		workFailure(ctx, http.StatusNotFound, "作业单不存在")
		return
	}
	var jobs []models.GbWorkRecording
	if err := c.db().WithContext(ctx).Where("batch_id = ?", batch.ID).Order("channel_id ASC").Find(&jobs).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "查询作业单录像失败")
		return
	}
	if len(jobs) == 0 || len(jobs) > workrecording.BatchMaxCameraCount {
		workFailure(ctx, http.StatusConflict, "作业单没有有效的摄像头记录")
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
	// Two batched reads replace the previous per-camera, per-slice lookups, so a
	// four-camera batch no longer costs one round-trip for every stored slice.
	jobIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
	}
	var links []models.GbWorkRecordingFile
	if err := c.db().WithContext(ctx).Where("work_recording_id IN ?", jobIDs).Find(&links).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "查询录像分片失败")
		return
	}
	linksByJob := make(map[string][]models.GbWorkRecordingFile, len(jobs))
	fileIDs := make([]uint64, 0, len(links))
	for _, link := range links {
		linksByJob[link.WorkRecordingID] = append(linksByJob[link.WorkRecordingID], link)
		fileIDs = append(fileIDs, link.FileID)
	}
	storedByID := make(map[uint64]models.GbRecordingFile, len(fileIDs))
	if len(fileIDs) > 0 {
		var stored []models.GbRecordingFile
		if err := c.db().WithContext(ctx).Where("id IN ?", fileIDs).Find(&stored).Error; err != nil {
			workFailure(ctx, http.StatusServiceUnavailable, "查询录像分片失败")
			return
		}
		for _, file := range stored {
			storedByID[file.ID] = file
		}
	}
	type zipFile struct {
		job    models.GbWorkRecording
		file   models.GbRecordingFile
		name   string
		source WorkRecordingFileSource
	}
	files := make([]zipFile, 0, len(fileIDs))
	for _, job := range jobs {
		for _, link := range linksByJob[job.ID] {
			file, ok := storedByID[link.FileID]
			if !ok {
				workFailure(ctx, http.StatusConflict, "录像分片尚未归档")
				return
			}
			if file.MissingAt != nil || file.MetadataState != models.RecordingMetadataComplete || !localWorkFile(job, file.FilePath) {
				workFailure(ctx, http.StatusConflict, "存在未完成或不可访问的录像分片")
				return
			}
			source, ok := c.fileSource(file.NodeID)
			if !ok {
				workFailure(ctx, http.StatusServiceUnavailable, "录像所在媒体节点不可用")
				return
			}
			// Ask the node to confirm the slice before committing to a 200 and a
			// ZIP header. The probe body is dropped because the slice is fetched
			// again while streaming, so a node that lost the file cannot turn the
			// download into a plausible-looking archive with a hole in it.
			probe, err := source.DownloadFile(ctx.Request.Context(), file.FilePath, "")
			if err != nil {
				workFailure(ctx, http.StatusBadGateway, "读取录像分片失败")
				return
			}
			closeDownloadBody(probe)
			if probe == nil || probe.StatusCode != http.StatusOK {
				workFailure(ctx, http.StatusConflict, "录像分片在媒体节点上不可读")
				return
			}
			files = append(files, zipFile{job: job, file: file, name: zipEntryName(job.ChannelID, file.FileName, len(files)), source: source})
		}
	}
	if len(files) == 0 {
		workFailure(ctx, http.StatusConflict, "作业单尚无可下载录像")
		return
	}
	ctx.Header("Content-Type", "application/zip")
	ctx.Header("Content-Disposition", workArchiveDisposition(batch))
	zw := zip.NewWriter(ctx.Writer)
	for _, item := range files {
		response, err := item.source.DownloadFile(ctx.Request.Context(), item.file.FilePath, "")
		if err != nil || response == nil || response.StatusCode != http.StatusOK {
			closeDownloadBody(response)
			// Headers are committed by now, so a JSON error body would be spliced
			// straight into the ZIP stream. Stop instead: the client observes a
			// truncated archive rather than a plausible-looking corrupted one.
			break
		}
		header := &zip.FileHeader{Name: item.name, Method: zip.Store}
		if item.file.StartTime != nil {
			header.SetModTime(item.file.StartTime.UTC())
		}
		dst, err := zw.CreateHeader(header)
		if err == nil {
			_, err = io.Copy(dst, response.Body)
		}
		closeDownloadBody(response)
		if err != nil {
			break
		}
	}
	_ = zw.Close()
}

// closeDownloadBody releases an opened node response, tolerating the nil result
// an error path can produce.
func closeDownloadBody(response *gbzlm.DownloadResponse) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

// WorkOrderFormHistory 拉取某字段的历史值（项目名称/站区/作业负责人/作业人员），
// 给前端 a-auto-complete 的下拉框喂数据。field 在 4 个白名单内，否则 400。
// 直接用 AbortWithStatusJSON：c.Common.Fail 在测试环境里因为 app.Response 初始化路径不同，
// 会偶发不写状态码；这里用 gin 原生 API 把契约固化在控制器层。
func (c *WorkRecordingController) WorkOrderFormHistory(ctx *gin.Context) {
	field := strings.TrimSpace(ctx.Query("field"))
	if !models.IsValidFormHistoryField(field) {
		ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": "field 必须在 4 个白名单字段内"})
		return
	}
	limit := 0
	if raw := strings.TrimSpace(ctx.Query("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			limit = v
		}
	}
	entries, err := c.batchService.ListFormHistory(ctx, field, limit)
	if err != nil {
		if errors.Is(err, workrecording.ErrFormHistoryInvalidField) {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": 1, "message": "field 必须在 4 个白名单字段内"})
			return
		}
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": 1, "message": "读取表单历史值失败: " + err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"items": entries}})
}

func (c *WorkRecordingController) WorkOrderFile(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	var batch models.GbWorkRecordingBatch
	result := c.db().WithContext(ctx).Where("id = ? AND created_by = ?", ctx.Param("id"), c.GetCurrentUserID(ctx)).First(&batch)
	if result.Error != nil || result.RowsAffected == 0 {
		workFailure(ctx, http.StatusNotFound, "作业单不存在")
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
	source, ok := c.fileSource(file.NodeID)
	if !ok {
		workFailure(ctx, http.StatusServiceUnavailable, "录像所在媒体节点不可用")
		return
	}
	// The proxy validates the client range, asks the node for exactly those bytes
	// and forwards the node's own headers, so playback works without this process
	// ever touching the recording's filesystem.
	err = workRecordingContentProxy.Stream(ctx.Request.Context(), ctx.Writer, source, gbrecording.ContentRequest{
		FilePath: file.FilePath,
		FileName: filepath.Base(file.FileName),
		FileSize: file.FileSize,
		Mode:     gbrecording.CapabilityModePlay,
		Range:    ctx.GetHeader("Range"),
	})
	if err != nil && !ctx.Writer.Written() {
		// Only reachable before the body starts: once headers are committed, a
		// JSON error would be spliced straight into the media stream.
		workFailure(ctx, http.StatusConflict, "录像文件在媒体节点上不可读")
	}
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

// workArchiveDisposition names the download after the work ledger instead of its
// raw UUID, so operators can tell which site and which day an archive belongs
// to. mime.FormatMediaType emits the RFC 5987 form on its own, which is required
// because the project names these ledgers carry are usually Chinese.
func workArchiveDisposition(batch models.GbWorkRecordingBatch) string {
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": workArchiveName(batch)})
	if disposition == "" {
		return `attachment; filename="work-recording.zip"`
	}
	return disposition
}

func workArchiveName(batch models.GbWorkRecordingBatch) string {
	project := ""
	var form workrecording.Form
	if err := json.Unmarshal([]byte(batch.FormJSON), &form); err == nil {
		project = sanitizeArchiveSegment(form.ProjectName)
	}
	if project == "" {
		project = "作业录像"
	}
	day := batch.CreatedAt
	if day.IsZero() {
		day = time.Now()
	}
	serial := strings.ReplaceAll(batch.ID, "-", "")
	if len(serial) > 8 {
		serial = serial[:8]
	}
	if serial == "" {
		serial = "unknown"
	}
	return fmt.Sprintf("%s_%s_%s.zip", project, day.Format("20060102"), serial)
}

// sanitizeArchiveSegment replaces characters that are illegal in a filename on
// any supported client OS and bounds the result to a sane length.
func sanitizeArchiveSegment(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r < 0x20 || r == 0x7f:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			builder.WriteRune('_')
		default:
			builder.WriteRune(r)
		}
	}
	trimmed := strings.Trim(builder.String(), " .")
	if trimmed == "" {
		return ""
	}
	if runes := []rune(trimmed); len(runes) > 60 {
		trimmed = string(runes[:60])
	}
	return strings.Trim(trimmed, " .")
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
	response := gin.H{"code": status, "message": "作业单操作未完成，请查询作业单状态"}
	if len(snapshots) > 0 && snapshots[0].ID != "" {
		response["data"] = snapshots[0]
	}
	ctx.JSON(status, response)
}

func workFailure(ctx *gin.Context, status int, message string) {
	ctx.JSON(status, gin.H{"code": status, "message": message})
}
