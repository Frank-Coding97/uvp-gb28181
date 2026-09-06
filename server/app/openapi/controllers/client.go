package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/utils/common"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

type RevocationView struct {
	Status  string `json:"status"`
	Pending int64  `json:"pending"`
	Closed  int64  `json:"closed"`
}
type RevocationStatusReader func(context.Context, int64) (RevocationView, error)

type ClientAdminController struct {
	db          *gorm.DB
	service     *client.Service
	permissions client.ManagementPermissionAuthorizer
	revocation  RevocationStatusReader
}

func NewClientAdminController(db *gorm.DB, service *client.Service, permissions client.ManagementPermissionAuthorizer, revocation RevocationStatusReader) *ClientAdminController {
	return &ClientAdminController{db: db, service: service, permissions: permissions, revocation: revocation}
}

// Handler is mounted only by the explicit protected admin registrar. Claims are
// middleware-owned; payload user/department fields never establish operator scope.
func (a *ClientAdminController) Handler(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		middleware.MarkSensitiveOperation(c, map[string]any{"resource": "openapi-client", "action": action})
		actor := common.GetCurrentUserID(c)
		if actor == 0 {
			adminDenied(c)
			return
		}
		if a == nil || a.db == nil || a.service == nil || a.permissions == nil {
			adminError(c, client.ErrDependencyUnavailable)
			return
		}
		access, err := datascope.ResolveOwnerDeptAccessByUserID(c.Request.Context(), a.db, actor)
		if err != nil {
			if errors.Is(err, datascope.ErrOwnerDeptAccessDenied) {
				adminDenied(c)
			} else {
				adminError(c, err)
			}
			return
		}
		if !access.FullAccess && len(access.DeptIDs) == 0 {
			adminDenied(c)
			return
		}
		allowed, err := a.permissions.Enforce(fmt.Sprintf("user_%d", actor), c.FullPath(), c.Request.Method, "*")
		if err != nil {
			adminError(c, err)
			return
		}
		if !allowed {
			adminDenied(c)
			return
		}
		if action == "list" {
			a.list(c, access)
			return
		}
		if c.Request.URL.RawQuery != "" {
			adminError(c, client.ErrInvalidArgument)
			return
		}
		if action == "capabilities" {
			writeOpenAPISuccess(c, client.SupportedScopes())
			return
		}
		if action == "create" {
			var input struct {
				Name              string `json:"name"`
				OwnerDeptID       uint   `json:"ownerDeptId"`
				ResponsibleUserID uint   `json:"responsibleUserId"`
			}
			if err := decodeAdminBody(c, &input, "name", "ownerDeptId", "responsibleUserId"); err != nil || utf8.RuneCountInString(input.Name) > 100 {
				adminError(c, client.ErrInvalidArgument)
				return
			}
			if !adminOwns(access, input.OwnerDeptID) {
				adminError(c, client.ErrNotFound)
				return
			}
			view, secret, err := a.service.Create(c.Request.Context(), client.CreateRequest{Name: input.Name, OwnerDeptID: input.OwnerDeptID, ResponsibleUserID: input.ResponsibleUserID, CreatedBy: actor})
			if err != nil {
				adminError(c, err)
				return
			}
			writeOpenAPISuccess(c, gin.H{"client": view, "secretKey": secret})
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			adminError(c, client.ErrNotFound)
			return
		}
		var view client.ClientView
		result := a.scopedClients(c, access).Where("id = ?", id).Take(&view)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			adminError(c, result.Error)
			return
		}
		if result.RowsAffected != 1 {
			adminError(c, client.ErrNotFound)
			return
		}
		switch action {
		case "detail":
			scopes, err := a.service.ListScopes(c.Request.Context(), id)
			if err != nil {
				adminError(c, err)
				return
			}
			writeOpenAPISuccess(c, gin.H{"client": view, "scopes": scopes})
		case "audits":
			a.audits(c, id)
		case "revocation-status":
			if a.revocation == nil {
				adminError(c, client.ErrRevocationUnavailable)
				return
			}
			status, err := a.revocation(c.Request.Context(), id)
			if err != nil {
				adminError(c, err)
				return
			}
			writeOpenAPISuccess(c, status)
		case "scopes":
			var input struct {
				RowVersion int64    `json:"rowVersion"`
				Scopes     []string `json:"scopes"`
			}
			if decodeAdminBody(c, &input, "rowVersion", "scopes") != nil || input.Scopes == nil {
				adminError(c, client.ErrInvalidArgument)
				return
			}
			updated, err := a.service.SetScopes(c.Request.Context(), id, input.Scopes, input.RowVersion, actor)
			if err != nil {
				adminError(c, err)
				return
			}
			writeOpenAPISuccess(c, updated)
		case "rotate", "enable", "disable", "revoke":
			var input struct {
				RowVersion int64 `json:"rowVersion"`
			}
			if decodeAdminBody(c, &input, "rowVersion") != nil {
				adminError(c, client.ErrInvalidArgument)
				return
			}
			if action == "rotate" {
				updated, secret, err := a.service.RotateSecret(c.Request.Context(), id, input.RowVersion, actor)
				if err != nil {
					adminError(c, err)
					return
				}
				writeOpenAPISuccess(c, gin.H{"client": updated, "secretKey": secret})
				return
			}
			status := map[string]string{"enable": models.StatusActive, "disable": models.StatusDisabled, "revoke": models.StatusRevoked}[action]
			updated, err := a.service.SetStatus(c.Request.Context(), id, status, input.RowVersion, actor)
			if err != nil {
				adminError(c, err)
				return
			}
			if action != "enable" {
				c.JSON(http.StatusAccepted, openAPIResponse{Code: "OK", Message: "accepted", RequestID: requestID(c), Data: gin.H{"client": updated, "revocationStatus": "pending"}})
			} else {
				writeOpenAPISuccess(c, updated)
			}
		default:
			adminDenied(c)
		}
	}
}

func adminOwns(access datascope.OwnerDeptAccess, id uint) bool {
	if id == 0 {
		return false
	}
	if access.FullAccess {
		return true
	}
	for _, allowed := range access.DeptIDs {
		if allowed == id {
			return true
		}
	}
	return false
}

const adminClientColumns = "id,ak,name,owner_dept_id,responsible_user_id,status,secret_version,auth_epoch,rate_limit,burst,viewer_quota,row_version,created_by,updated_by,created_at,updated_at"

func (a *ClientAdminController) scopedClients(c *gin.Context, access datascope.OwnerDeptAccess) *gorm.DB {
	query := a.db.WithContext(c.Request.Context()).Model(&models.Client{}).Select(adminClientColumns)
	if !access.FullAccess {
		query = query.Where("owner_dept_id IN ?", access.DeptIDs)
	}
	return query
}

type adminOwnerDepartment struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

func (a *ClientAdminController) list(c *gin.Context, access datascope.OwnerDeptAccess) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		adminError(c, client.ErrInvalidArgument)
		return
	}
	for key, items := range values {
		if (key != "page" && key != "pageSize" && key != "ownerDeptId") || len(items) != 1 {
			adminError(c, client.ErrInvalidArgument)
			return
		}
	}
	var ownerID uint64
	if values.Has("ownerDeptId") {
		ownerID, err = strconv.ParseUint(values.Get("ownerDeptId"), 10, strconv.IntSize)
		if err != nil || ownerID == 0 {
			adminError(c, client.ErrInvalidArgument)
			return
		}
	}
	page, size, err := parsePageValues(values)
	if err != nil || page-1 > int(^uint(0)>>1)/size {
		adminError(c, client.ErrInvalidArgument)
		return
	}
	items := []client.ClientView{}
	departments := []adminOwnerDepartment{}
	var total int64
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		scoped := *a
		scoped.db = tx
		query := func() *gorm.DB {
			q := scoped.scopedClients(c, access)
			if ownerID != 0 {
				q = q.Where("owner_dept_id = ?", ownerID)
			}
			return q
		}
		if err := query().Select("count(*)").Count(&total).Error; err != nil {
			return err
		}
		if err := query().Order("id ASC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
			return err
		}
		// Options are scoped server-side, independent from the current client
		// page/filter. Never expose the general unscoped department tree here.
		options := tx.Table("sys_department").Select("id, name").Where("status = ? AND deleted_at IS NULL", 1)
		if !access.FullAccess {
			options = options.Where("id IN ?", access.DeptIDs)
		}
		return options.Order("id ASC").Find(&departments).Error
	})
	if err != nil {
		adminError(c, err)
		return
	}
	writeOpenAPISuccess(c, gin.H{"items": items, "page": page, "pageSize": size, "total": total, "ownerDepartments": departments})
}

type adminAuditView struct {
	RequestID    string    `json:"requestId"`
	Scope        string    `json:"scope"`
	ResourceType string    `json:"resourceType"`
	ResourceID   string    `json:"resourceId"`
	Result       string    `json:"result"`
	ReasonClass  string    `json:"reasonClass"`
	Source       string    `json:"source"`
	LatencyMS    int64     `json:"latencyMs"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (a *ClientAdminController) audits(c *gin.Context, id int64) {
	items := []adminAuditView{}
	err := a.db.WithContext(c.Request.Context()).Model(&models.Audit{}).
		Select("request_id,scope,resource_type,resource_id,result,reason_class,source,latency_ms,created_at").
		Where("client_id = ?", id).Order("created_at DESC, id DESC").Limit(100).Find(&items).Error
	if err != nil {
		adminError(c, err)
		return
	}
	writeOpenAPISuccess(c, gin.H{"items": items})
}

func decodeAdminBody(c *gin.Context, out any, allowed ...string) error {
	if c.GetHeader("Content-Type") != "application/json" {
		return client.ErrInvalidArgument
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 65537))
	if err != nil || len(data) > 65536 {
		return client.ErrInvalidArgument
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return client.ErrInvalidArgument
	}
	fields := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		fields[key] = false
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return client.ErrInvalidArgument
		}
		seen, known := fields[key]
		if !known || seen {
			return client.ErrInvalidArgument
		}
		fields[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if _, err = decoder.Token(); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return client.ErrInvalidArgument
	}
	return json.Unmarshal(data, out)
}

func adminDenied(c *gin.Context) {
	writeOpenAPIError(c, http.StatusForbidden, "CAPABILITY_DENIED", "capability denied")
}
func adminError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, client.ErrNotFound), errors.Is(err, client.ErrManagementBoundaryDenied):
		writeOpenAPIError(c, 404, "RESOURCE_NOT_FOUND", "resource not found")
	case errors.Is(err, client.ErrInvalidArgument), errors.Is(err, client.ErrUnknownScope), errors.Is(err, client.ErrOwnerDeptImmutable):
		writeOpenAPIError(c, 400, "INVALID_REQUEST", "invalid request")
	case errors.Is(err, client.ErrConflict), errors.Is(err, client.ErrRevoked), errors.Is(err, client.ErrClientDisabled):
		writeOpenAPIError(c, 409, "CONFLICT", "conflict")
	default:
		writeOpenAPIError(c, 503, "SERVICE_UNAVAILABLE", "service unavailable")
	}
}
