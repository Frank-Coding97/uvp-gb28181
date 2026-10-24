package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
	"uvplatform.com/uvp-gb28181/app/middleware"
)

type publishGroupRequest struct {
	PublishGroup     *bool   `json:"publishGroup"`
	ExpectedRevision *uint64 `json:"expectedRevision"`
}

// SetPublishGroup switches only business-directory publication. The runtime
// warning response intentionally includes the committed view so callers can
// retry synchronization with its latest ConfigRevision.
func (c *ManagementController) SetPublishGroup(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	var req publishGroupRequest
	decoder := json.NewDecoder(ctx.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req.PublishGroup == nil || req.ExpectedRevision == nil || *req.ExpectedRevision == 0 {
		c.fail(ctx, service.ErrInvalidPlatformConfig)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		c.fail(ctx, service.ErrInvalidPlatformConfig)
		return
	}
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "cascade_platform", "platformId": id, "operation": "publish_group"})
	platform, err := c.service.SetPublishGroup(ctx.Request.Context(), id, *req.ExpectedRevision, *req.PublishGroup)
	if err != nil {
		if errors.Is(err, service.ErrRuntimeSyncFailed) && platform != nil {
			ctx.JSON(http.StatusOK, gin.H{
				"platform": platform, "runtimeSynced": false,
				"warning": "发布设置已保存，运行时同步失败；请用最新版本重试以重新同步",
			})
			return
		}
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, platform)
}
