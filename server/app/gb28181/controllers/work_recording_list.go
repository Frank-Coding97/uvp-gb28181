package controllers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

const (
	workRecordingListDefaultPage     = 1
	workRecordingListDefaultPageSize = 20
	workRecordingListMaxPageSize     = 50
)

type workRecordingListItem struct {
	ID            string     `json:"id"`
	ChannelID     uint       `json:"channelId"`
	ChannelName   string     `json:"channelName"`
	State         string     `json:"state"`
	Version       uint64     `json:"version"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
	StartedAt     *time.Time `json:"startedAt"`
	StoppedAt     *time.Time `json:"stoppedAt"`
	FileState     string     `json:"fileState"`
	FormState     string     `json:"formState"`
	LastError     string     `json:"lastError"`
}

// List returns durable work-recording snapshots for the current user. It is
// intentionally a database-only read: status refresh and ZLM observation are
// handled by the detail/status paths, so pagination never performs one call
// per row.
func (c *WorkRecordingController) List(ctx *gin.Context) {
	if c.GetCurrentUserID(ctx) == 0 {
		workFailure(ctx, http.StatusUnauthorized, "请先登录")
		return
	}
	// List is a database snapshot, but it still uses the controller readiness
	// guard so an unassembled route returns a bounded error instead of touching
	// the global DB provider. Keep the explicit auth check above because ready
	// also owns the service dependency, which listing itself does not call.
	if c.service == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业录像服务未就绪")
		return
	}
	if !c.ready(ctx) {
		return
	}
	db := c.db()
	if db == nil {
		workFailure(ctx, http.StatusServiceUnavailable, "作业录像服务未就绪")
		return
	}
	page, pageSize, ok := workRecordingListPagination(ctx)
	if !ok {
		workFailure(ctx, http.StatusBadRequest, "分页参数不合法")
		return
	}

	actor := c.GetCurrentUserID(ctx)
	query := db.WithContext(ctx).
		Model(&models.GbChannel{}).
		Joins("JOIN gb_work_recording ON gb_work_recording.channel_id = gb_channel.id").
		Where("gb_work_recording.created_by = ?", actor).
		// Both joined tables carry device_id/owner_dept_id. Use the same
		// visible-scope policy with qualified channel columns to avoid an
		// ambiguous predicate in COUNT and SELECT.
		Scopes(datascope.VisibilityScope(ctx, "gb_channel.owner_dept_id", "gb_channel.device_id"))
	state := strings.TrimSpace(ctx.Query("state"))
	switch {
	case state == "" || strings.EqualFold(state, "all"):
	case strings.EqualFold(state, "active"):
		query = query.Where("gb_work_recording.state IN ?", []string{
			workrecording.StateStarting,
			workrecording.StateRecording,
			workrecording.StateStopping,
			workrecording.StateUnknown,
		})
	default:
		workFailure(ctx, http.StatusBadRequest, "状态参数不合法")
		return
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "查询作业录像列表失败")
		return
	}

	items := make([]workRecordingListItem, 0)
	selectColumns := strings.Join([]string{
		"gb_work_recording.id",
		"gb_work_recording.channel_id",
		"gb_channel.name AS channel_name",
		"gb_work_recording.state",
		"gb_work_recording.version",
		"gb_work_recording.last_checked_at",
		"gb_work_recording.started_at",
		"gb_work_recording.stopped_at",
		"gb_work_recording.file_state",
		"gb_work_recording.form_state",
		"gb_work_recording.last_error",
	}, ", ")
	result := query.Select(selectColumns).
		Order("gb_work_recording.created_at DESC, gb_work_recording.id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items)
	if result.Error != nil {
		workFailure(ctx, http.StatusServiceUnavailable, "查询作业录像列表失败")
		return
	}

	c.Success(ctx, gin.H{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func workRecordingListPagination(ctx *gin.Context) (int, int, bool) {
	page, err := strconv.Atoi(ctx.DefaultQuery("page", strconv.Itoa(workRecordingListDefaultPage)))
	if err != nil || page < 1 {
		return 0, 0, false
	}
	pageSize, err := strconv.Atoi(ctx.DefaultQuery("pageSize", strconv.Itoa(workRecordingListDefaultPageSize)))
	if err != nil || pageSize < 1 || pageSize > workRecordingListMaxPageSize {
		return 0, 0, false
	}
	maxInt := int(^uint(0) >> 1)
	if page > 1 && page-1 > maxInt/pageSize {
		return 0, 0, false
	}
	return page, pageSize, true
}
