package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	workRecordingListDefaultPage     = 1
	workRecordingListDefaultPageSize = 20
	workRecordingListMaxPageSize     = 50
)

// workRecordingListPagination 校验并解析作业单列表的分页参数。
// 严格的边界检查避免超大的 page 乘以 pageSize 造成偏移量溢出。
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
