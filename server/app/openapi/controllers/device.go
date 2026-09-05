package controllers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

// OwnerDeptResolver must return a department already authenticated by the
// OpenAPI middleware. It must not read caller-supplied department fields.
type OwnerDeptResolver func(*gin.Context) (uint, bool)

type DeviceController struct {
	service  *resource.Service
	resolver OwnerDeptResolver
}

func NewDeviceController(service *resource.Service, resolver ...OwnerDeptResolver) *DeviceController {
	var access OwnerDeptResolver
	if len(resolver) > 0 {
		access = resolver[0]
	}
	return &DeviceController{service: service, resolver: access}
}

func (dc *DeviceController) List(c *gin.Context) {
	ownerDeptID, ok := dc.trustedOwner(c)
	if !ok {
		writeOpenAPIError(c, http.StatusForbidden, "AUTH_REQUIRED", "authentication required")
		return
	}
	options, err := parseDeviceListOptions(c)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid request")
		return
	}
	page, err := dc.service.ListDevices(c.Request.Context(), ownerDeptID, options)
	if err != nil {
		writeResourceError(c, err)
		return
	}
	writeOpenAPISuccess(c, page)
}

func (dc *DeviceController) Get(c *gin.Context) {
	ownerDeptID, ok := dc.trustedOwner(c)
	if !ok {
		writeOpenAPIError(c, http.StatusForbidden, "AUTH_REQUIRED", "authentication required")
		return
	}
	device, err := dc.service.GetDevice(c.Request.Context(), ownerDeptID, c.Param("deviceId"))
	if err != nil {
		writeResourceError(c, err)
		return
	}
	writeOpenAPISuccess(c, device)
}

func (dc *DeviceController) Status(c *gin.Context) {
	ownerDeptID, ok := dc.trustedOwner(c)
	if !ok {
		writeOpenAPIError(c, http.StatusForbidden, "AUTH_REQUIRED", "authentication required")
		return
	}
	status, err := dc.service.GetDeviceStatus(c.Request.Context(), ownerDeptID, c.Param("deviceId"))
	if err != nil {
		writeResourceError(c, err)
		return
	}
	writeOpenAPISuccess(c, status)
}

func (dc *DeviceController) trustedOwner(c *gin.Context) (uint, bool) {
	if dc == nil || dc.service == nil || dc.resolver == nil || c == nil {
		return 0, false
	}
	ownerDeptID, ok := dc.resolver(c)
	if !ok || ownerDeptID == 0 {
		return 0, false
	}
	return ownerDeptID, true
}

func parseDeviceListOptions(c *gin.Context) (resource.DeviceListOptions, error) {
	values, err := parseResourceQuery(c)
	if err != nil {
		return resource.DeviceListOptions{}, err
	}
	page, pageSize, err := parsePageValues(values)
	if err != nil {
		return resource.DeviceListOptions{}, err
	}
	return resource.DeviceListOptions{Page: page, PageSize: pageSize, Keyword: values.Get("keyword"), Status: values.Get("status")}, nil
}

func parseResourceQuery(c *gin.Context) (url.Values, error) {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil, errors.New("missing request")
	}
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	allowed := map[string]struct{}{"page": {}, "pageSize": {}, "keyword": {}, "status": {}}
	for key, items := range values {
		if _, ok := allowed[key]; !ok || len(items) != 1 {
			return nil, errors.New("query field is not allowed")
		}
	}
	if keyword := values.Get("keyword"); !utf8.ValidString(keyword) || utf8.RuneCountInString(keyword) > 100 {
		return nil, errors.New("keyword is too long")
	}
	if status := values.Get("status"); status != "" && status != "online" && status != "offline" && status != "unknown" {
		return nil, errors.New("status is not allowed")
	}
	return values, nil
}

func parsePageValues(values url.Values) (int, int, error) {
	page, err := positiveQueryInt(values.Get("page"), 1)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := positiveQueryInt(values.Get("pageSize"), 20)
	if err != nil || pageSize > 100 {
		return 0, 0, errors.New("page size is invalid")
	}
	return page, pageSize, nil
}

func positiveQueryInt(value string, defaultValue int) (int, error) {
	if value == "" {
		return defaultValue, nil
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, errors.New("integer is invalid")
		}
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("integer is invalid")
	}
	return parsed, nil
}

type openAPIResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	Data      any    `json:"data"`
}

func writeOpenAPISuccess(c *gin.Context, data any) {
	c.JSON(http.StatusOK, openAPIResponse{Code: "OK", Message: "success", RequestID: requestID(c), Data: data})
}

func writeOpenAPIError(c *gin.Context, status int, code, message string) {
	c.JSON(status, openAPIResponse{Code: code, Message: message, RequestID: requestID(c), Data: nil})
}

func writeResourceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, resource.ErrResourceNotFound):
		writeOpenAPIError(c, http.StatusNotFound, "RESOURCE_NOT_FOUND", "resource not found")
	case errors.Is(err, resource.ErrInvalidListOptions):
		writeOpenAPIError(c, http.StatusBadRequest, "INVALID_REQUEST", "invalid request")
	default:
		writeOpenAPIError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "service unavailable")
	}
}

func requestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value := c.GetString("requestId"); value != "" {
		return value
	}
	return c.GetHeader("X-Request-Id")
}
