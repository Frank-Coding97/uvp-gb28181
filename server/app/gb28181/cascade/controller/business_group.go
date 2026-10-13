package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
	"uvplatform.com/uvp-gb28181/app/middleware"
)

type businessGroupRequest struct {
	Nodes                      []repository.BusinessNodeInput    `json:"nodes"`
	Bindings                   []repository.BusinessBindingInput `json:"bindings"`
	ExpectedProjectionRevision uint64                            `json:"expectedProjectionRevision"`
}

type businessGroupResponse struct {
	PlatformID uint64                           `json:"platformId"`
	Revision   uint64                           `json:"revision"`
	Nodes      []model.GbCascadeBusinessNode    `json:"nodes"`
	Bindings   []model.GbCascadeBusinessBinding `json:"bindings"`
	Channels   []shareChannelResponse           `json:"channels"`
}

func buildBusinessGroupResponse(id uint64, snapshot *repository.ProjectionSnapshot) businessGroupResponse {
	nodes := append([]model.GbCascadeBusinessNode{}, snapshot.Nodes...)
	bindings := append([]model.GbCascadeBusinessBinding{}, snapshot.Bindings...)
	return businessGroupResponse{PlatformID: id, Revision: snapshot.Revision, Nodes: nodes, Bindings: bindings, Channels: buildShareResponse(id, snapshot).Channels}
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
	if err := c.service.ReplaceBusinessGroup(ctx.Request.Context(), id, req.ExpectedProjectionRevision, req.Nodes, req.Bindings); err != nil {
		c.fail(ctx, err)
		return
	}
	snapshot, err := c.service.Projection(ctx.Request.Context(), id)
	if err != nil {
		c.fail(ctx, err)
		return
	}
	ctx.JSON(http.StatusOK, buildBusinessGroupResponse(id, snapshot))
}
