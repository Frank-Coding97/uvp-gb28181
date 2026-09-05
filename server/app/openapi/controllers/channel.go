package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

type ChannelController struct {
	service  *resource.Service
	resolver OwnerDeptResolver
}

func NewChannelController(service *resource.Service, resolver ...OwnerDeptResolver) *ChannelController {
	var access OwnerDeptResolver
	if len(resolver) > 0 {
		access = resolver[0]
	}
	return &ChannelController{service: service, resolver: access}
}

func (cc *ChannelController) List(c *gin.Context) {
	ownerDeptID, ok := cc.trustedOwner(c)
	if !ok {
		writeOpenAPIError(c, http.StatusForbidden, "AUTH_REQUIRED", "authentication required")
		return
	}
	values, err := parseResourceQuery(c)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid request")
		return
	}
	page, pageSize, err := parsePageValues(values)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid request")
		return
	}
	options := resource.ChannelListOptions{Page: page, PageSize: pageSize, Keyword: values.Get("keyword"), Status: values.Get("status")}
	result, err := cc.service.ListChannels(c.Request.Context(), ownerDeptID, c.Param("deviceId"), options)
	if err != nil {
		writeResourceError(c, err)
		return
	}
	writeOpenAPISuccess(c, result)
}

func (cc *ChannelController) Get(c *gin.Context) {
	ownerDeptID, ok := cc.trustedOwner(c)
	if !ok {
		writeOpenAPIError(c, http.StatusForbidden, "AUTH_REQUIRED", "authentication required")
		return
	}
	result, err := cc.service.GetChannel(c.Request.Context(), ownerDeptID, c.Param("deviceId"), c.Param("channelId"))
	if err != nil {
		writeResourceError(c, err)
		return
	}
	writeOpenAPISuccess(c, result)
}

func (cc *ChannelController) Status(c *gin.Context) {
	ownerDeptID, ok := cc.trustedOwner(c)
	if !ok {
		writeOpenAPIError(c, http.StatusForbidden, "AUTH_REQUIRED", "authentication required")
		return
	}
	result, err := cc.service.GetChannelStatus(c.Request.Context(), ownerDeptID, c.Param("deviceId"), c.Param("channelId"))
	if err != nil {
		writeResourceError(c, err)
		return
	}
	writeOpenAPISuccess(c, result)
}

func (cc *ChannelController) trustedOwner(c *gin.Context) (uint, bool) {
	if cc == nil || cc.service == nil || cc.resolver == nil || c == nil {
		return 0, false
	}
	ownerDeptID, ok := cc.resolver(c)
	if !ok || ownerDeptID == 0 {
		return 0, false
	}
	return ownerDeptID, true
}
