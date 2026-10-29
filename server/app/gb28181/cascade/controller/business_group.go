package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	globalapp "uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/middleware"
	"uvplatform.com/uvp-gb28181/app/utils/common"
	"uvplatform.com/uvp-gb28181/app/utils/datascope"
)

var (
	errCascadeManageDenied    = errors.New("missing cascade manage permission")
	errCascadePermissionError = errors.New("cascade permission checker unavailable")
)

type cascadePermissionChecker interface {
	Enforce(sub, obj, act string, domain ...string) (bool, error)
}

type businessGroupRequest struct {
	Nodes                      []repository.BusinessNodeInput           `json:"nodes"`
	Bindings                   []repository.BusinessBindingInput        `json:"bindings"`
	ShareChannelIDs            []uint64                                 `json:"shareChannelIds"`
	ResourceChanges            *repository.BusinessGroupResourceChanges `json:"resourceChanges"`
	ExpectedProjectionRevision uint64                                   `json:"expectedProjectionRevision"`
	PublishGroup               *bool                                    `json:"publishGroup,omitempty"`
	ExpectedConfigRevision     uint64                                   `json:"expectedConfigRevision,omitempty"`
}

type businessGroupResponse struct {
	PlatformID     uint64                           `json:"platformId"`
	Revision       uint64                           `json:"revision"`
	Nodes          []model.GbCascadeBusinessNode    `json:"nodes"`
	Bindings       []model.GbCascadeBusinessBinding `json:"bindings"`
	Channels       []shareChannelResponse           `json:"channels"`
	PublishGroup   bool                             `json:"publishGroup"`
	ConfigRevision uint64                           `json:"configRevision"`
	RuntimeSynced  *bool                            `json:"runtimeSynced,omitempty"`
	Warning        string                           `json:"warning,omitempty"`
}

func buildBusinessGroupResponse(id uint64, snapshot *repository.ProjectionSnapshot) businessGroupResponse {
	nodes := append([]model.GbCascadeBusinessNode{}, snapshot.Nodes...)
	bindings := append([]model.GbCascadeBusinessBinding{}, snapshot.Bindings...)
	return businessGroupResponse{PlatformID: id, Revision: snapshot.Revision, Nodes: nodes, Bindings: bindings, Channels: buildShareResponse(id, snapshot).Channels, PublishGroup: snapshot.Platform.PublishGroup, ConfigRevision: snapshot.Platform.ConfigRevision}
}

func (c *ManagementController) GetBusinessGroup(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	snapshot, err := c.service.Projection(ctx.Request.Context(), id)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, buildBusinessGroupResponse(id, snapshot))
}

func (c *ManagementController) ReplaceBusinessGroup(ctx *gin.Context) {
	if !c.ready(ctx) {
		return
	}
	id, ok := parseID(ctx)
	if !ok {
		return
	}
	var req businessGroupRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		c.fail(ctx, service.ErrInvalidBusinessGroup)
		return
	}
	middleware.MarkSensitiveOperation(ctx, map[string]any{"resource": "cascade_business_group", "platformId": id, "operation": "replace"})
	changes := repository.BusinessGroupResourceChanges{}
	if req.ResourceChanges != nil {
		changes = *req.ResourceChanges
	}
	publication := repository.BusinessGroupPublicationUpdate{PublishGroup: req.PublishGroup, ExpectedConfigRevision: req.ExpectedConfigRevision}
	if changes.PublishGroup != nil {
		if publication.PublishGroup != nil && *publication.PublishGroup != *changes.PublishGroup {
			c.fail(ctx, service.ErrInvalidBusinessGroup)
			return
		}
		if req.ExpectedConfigRevision != 0 && changes.ExpectedConfigRevision != 0 && req.ExpectedConfigRevision != changes.ExpectedConfigRevision {
			c.fail(ctx, service.ErrInvalidBusinessGroup)
			return
		}
		if publication.PublishGroup == nil {
			publishGroup := *changes.PublishGroup
			publication.PublishGroup = &publishGroup
		}
		if publication.ExpectedConfigRevision == 0 {
			publication.ExpectedConfigRevision = changes.ExpectedConfigRevision
		}
	}
	changes.PublishGroup = nil
	changes.ExpectedConfigRevision = 0
	var err error
	if publication.PublishGroup != nil {
		err = c.service.ReplaceBusinessGroupWithOptions(ctx.Request.Context(), id, req.ExpectedProjectionRevision, req.Nodes, req.Bindings, req.ShareChannelIDs, changes, publication, c.businessGroupSourceAuthorizer(ctx), func() error {
			return c.authorizeCascadeManage(ctx)
		})
	} else if req.ResourceChanges != nil {
		err = c.service.ReplaceBusinessGroupWithResourceChanges(ctx.Request.Context(), id, req.ExpectedProjectionRevision, req.Nodes, req.Bindings, req.ShareChannelIDs, changes, c.businessGroupSourceAuthorizer(ctx))
	} else if len(req.ShareChannelIDs) > 0 {
		err = c.service.ReplaceBusinessGroupWithShares(ctx.Request.Context(), id, req.ExpectedProjectionRevision, req.Nodes, req.Bindings, req.ShareChannelIDs, c.businessGroupSourceAuthorizer(ctx))
	} else {
		err = c.service.ReplaceBusinessGroup(ctx.Request.Context(), id, req.ExpectedProjectionRevision, req.Nodes, req.Bindings)
	}
	resourceMutation := req.ResourceChanges != nil || len(req.ShareChannelIDs) > 0 || publication.PublishGroup != nil
	runtimeSyncErr := err
	if err != nil && !(resourceMutation && errors.Is(err, service.ErrRuntimeSyncFailed)) {
		c.fail(ctx, err)
		return
	}
	snapshot, err := c.service.Projection(ctx.Request.Context(), id)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	response := buildBusinessGroupResponse(id, snapshot)
	if resourceMutation && errors.Is(runtimeSyncErr, service.ErrRuntimeSyncFailed) {
		runtimeSynced := false
		response.RuntimeSynced = &runtimeSynced
		response.Warning = "资源已保存，但运行时同步失败；可使用最新修订号重试保存以重新同步"
	}
	ctx.JSON(http.StatusOK, response)
}

func (c *ManagementController) authorizeCascadeManage(ctx *gin.Context) error {
	claims := common.GetClaims(ctx)
	if claims == nil || claims.UserID == 0 {
		return errCascadeManageDenied
	}
	checker := c.permissionChecker
	if checker == nil {
		checker = globalapp.CasbinV2
	}
	if checker == nil {
		return errCascadePermissionError
	}
	ok, err := checker.Enforce(fmt.Sprintf("user_%d", claims.UserID), "/api/gb28181/cascade/platforms/:id/publish-group", http.MethodPut, "*")
	if err != nil {
		return fmt.Errorf("%w: %v", errCascadePermissionError, err)
	}
	if !ok {
		return errCascadeManageDenied
	}
	return nil
}

func (c *ManagementController) businessGroupSourceAuthorizer(ctx *gin.Context) repository.BusinessGroupSourceAuthorizer {
	if c.db == nil {
		return nil
	}
	return func(tx *gorm.DB, sourceDeviceIDs, sourceChannelIDs []uint64) error {
		if len(sourceDeviceIDs) > 0 {
			var devices []gbmodels.GbDevice
			query := tx.Model(&gbmodels.GbDevice{}).
				Where("id IN ?", sourceDeviceIDs).
				Scopes(datascope.VisibilityScopeWithDB(ctx, tx, "owner_dept_id", "device_id"))
			if err := query.Find(&devices).Error; err != nil {
				return err
			}
			if len(devices) != len(sourceDeviceIDs) {
				return repository.ErrPlatformNotFound
			}
		}
		if len(sourceChannelIDs) > 0 {
			var channels []gbmodels.GbChannel
			query := tx.Model(&gbmodels.GbChannel{}).
				Where("id IN ?", sourceChannelIDs).
				Scopes(datascope.VisibilityScopeWithDB(ctx, tx, "owner_dept_id", "device_id"))
			if err := query.Find(&channels).Error; err != nil {
				return err
			}
			if len(channels) != len(sourceChannelIDs) {
				return repository.ErrPlatformNotFound
			}
		}
		return nil
	}
}
