package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/middleware"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var adminDBID atomic.Int64

type testAdminPermissions struct{ denied bool }

func (p *testAdminPermissions) Enforce(string, string, string, ...string) (bool, error) {
	return !p.denied, nil
}

func newAdminHTTP(t *testing.T, stores ...client.RevocationIntentStore) (*gorm.DB, *ClientAdminController, *testAdminPermissions) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:adminhttp%d?mode=memory&cache=shared", adminDBID.Add(1))), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Audit{}, &basemodels.SysOperationLog{}, &basemodels.User{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.SysDepartment{}))
	active := int8(1)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{{BaseModel: basemodels.BaseModel{ID: 10}, Name: "A", Status: &active}, {BaseModel: basemodels.BaseModel{ID: 20}, Name: "B", Status: &active}}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "admin-test", Password: "unused", Status: 1, DeptID: 10}).Error)
	require.NoError(t, db.Create(&basemodels.SysRole{BaseModel: basemodels.BaseModel{ID: 1}, Name: "limited", Status: 1, DataScope: 3}).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: 7, RoleID: 1}).Error)
	permissions := &testAdminPermissions{}
	keys, err := client.NewSecretManager(bytes.Repeat([]byte{1}, 32), "test")
	require.NoError(t, err)
	opts := []client.ServiceOption{client.WithManagementBoundary(client.NewManagementScopeBoundary(db, permissions))}
	if len(stores) > 0 {
		opts = append(opts, client.WithRevocationIntentStore(stores[0]))
	}
	svc, err := client.NewService(db, keys, opts...)
	require.NoError(t, err)
	return db, NewClientAdminController(db, svc, permissions, nil), permissions
}

type adminTestIntent struct {
	ID       uint `gorm:"primaryKey"`
	ClientID int64
}
type adminIntentStore struct{}

func (adminIntentStore) RecordRevocationIntent(ctx context.Context, tx *gorm.DB, intent client.RevocationIntent) error {
	return tx.WithContext(ctx).Create(&adminTestIntent{ClientID: intent.ClientID}).Error
}

func TestOpenAPIAdminHTTPMutationsAndFailClosedRevocation(t *testing.T) {
	db, ctrl, permissions := newAdminHTTP(t, adminIntentStore{})
	require.NoError(t, db.AutoMigrate(&adminTestIntent{}))
	view, secret, err := ctrl.service.Create(context.Background(), client.CreateRequest{Name: "A", OwnerDeptID: 10, CreatedBy: 7})
	require.NoError(t, err)
	base := fmt.Sprintf("/api/gb28181/openapi-clients/%d", view.ID)
	invoke := func(action, method, suffix, body string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		out, marked := adminRequest(t, ctrl, 7, action, method, base+suffix, body)
		require.Equal(t, expected, out.Code, out.Body.String())
		require.True(t, marked)
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		return out
	}
	invoke("scopes", "PUT", "/scopes", `{"rowVersion":1,"scopes":["device:list","invalid"]}`, 400)
	var count int64
	require.NoError(t, db.Model(&models.ClientScope{}).Count(&count).Error)
	require.Zero(t, count)
	invoke("scopes", "PUT", "/scopes", `{"rowVersion":1,"scopes":["device:list"]}`, 200)
	invoke("rotate", "POST", "/rotate-secret", `{"rowVersion":1}`, 409)
	rotated := invoke("rotate", "POST", "/rotate-secret", `{"rowVersion":2}`, 200)
	require.Contains(t, rotated.Body.String(), "secretKey")
	require.NotContains(t, rotated.Body.String(), secret)
	detail := invoke("detail", "GET", "", "", 200)
	require.NotContains(t, detail.Body.String(), "secretKey")
	invoke("revocation-status", "GET", "/revocation-status", "", 503)
	disabled := invoke("disable", "POST", "/disable", `{"rowVersion":3}`, 202)
	require.Contains(t, disabled.Body.String(), `"revocationStatus":"pending"`)
	require.NoError(t, db.Model(&adminTestIntent{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&basemodels.SysDepartment{}).Where("id = ?", 10).Update("status", 0).Error)
	invoke("enable", "POST", "/enable", `{"rowVersion":4}`, 404)
	invoke("audits", "GET", "/audits", "", 200)
	invoke("revoke", "POST", "/revoke", `{"rowVersion":4}`, 202)
	permissions.denied = true
	invoke("detail", "GET", "", "", 403)
	invoke("audits", "GET", "/audits", "", 403)

	_, noStore, _ := newAdminHTTP(t)
	v, _, err := noStore.service.Create(context.Background(), client.CreateRequest{Name: "B", OwnerDeptID: 10, CreatedBy: 7})
	require.NoError(t, err)
	for _, action := range []string{"disable", "revoke"} {
		out, _ := adminRequest(t, noStore, 7, action, "POST", fmt.Sprintf("/api/gb28181/openapi-clients/%d/%s", v.ID, action), `{"rowVersion":1}`)
		require.Equal(t, 503, out.Code)
	}
	unchanged, err := noStore.service.Get(context.Background(), v.ID)
	require.NoError(t, err)
	require.Equal(t, models.StatusActive, unchanged.Status)
	require.EqualValues(t, 1, unchanged.RowVersion)
}

func adminRequest(t *testing.T, ctrl *ClientAdminController, actor uint, action, method, path, body string) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	marked := false
	router.Use(func(c *gin.Context) {
		if actor != 0 {
			c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: actor}})
		}
		c.Next()
		_, marked = middleware.SensitiveOperationMetadata(c)
	})
	u, err := url.Parse(path)
	require.NoError(t, err)
	pattern := u.Path
	if action != "list" && action != "create" && action != "capabilities" {
		pattern = "/api/gb28181/openapi-clients/:id"
		if action != "detail" {
			suffix := action
			if action == "rotate" {
				suffix = "rotate-secret"
			}
			pattern += "/" + suffix
		}
	}
	router.Handle(method, pattern, ctrl.Handler(action))
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	return out, marked
}

func TestOpenAPIAdminHTTPRejectsMissingIdentityAndPermission(t *testing.T) {
	db, ctrl, permissions := newAdminHTTP(t)
	for _, action := range []string{"list", "detail", "create", "scopes", "rotate", "enable", "disable", "revoke", "audits", "revocation-status", "capabilities"} {
		path := "/api/gb28181/openapi-clients"
		if action == "detail" {
			path += "/1"
		} else if action != "list" && action != "create" && action != "capabilities" {
			path += "/1/" + action
			if action == "rotate" {
				path += "-secret"
			}
		}
		out, _ := adminRequest(t, ctrl, 0, action, "GET", path, "")
		require.Equal(t, 403, out.Code, action)
	}
	permissions.denied = true
	out, _ := adminRequest(t, ctrl, 7, "create", "POST", "/api/gb28181/openapi-clients", `{"name":"x","ownerDeptId":10}`)
	require.Equal(t, 403, out.Code)
	var count int64
	require.NoError(t, db.Model(&models.Client{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestOpenAPIAdminHTTPDepartmentFilter(t *testing.T) {
	db, ctrl, _ := newAdminHTTP(t)
	for _, row := range []models.Client{{ID: 40, AK: "a", Name: "visible", OwnerDeptID: 10}, {ID: 50, AK: "b", Name: "hidden", OwnerDeptID: 20}} {
		row.SecretCiphertext, row.SecretIV, row.SecretKeyID = []byte{1}, []byte{1}, "test"
		require.NoError(t, db.Create(&row).Error)
	}
	for _, tc := range []struct {
		query  string
		status int
		total  int
	}{
		{"?ownerDeptId=10", 200, 1}, {"?ownerDeptId=20", 200, 0},
		{"?ownerDeptId=99", 200, 0}, {"?ownerDeptId=0", 400, 0},
		{"?ownerDeptId=10&ownerDeptId=20", 400, 0}, {"?pageSize=101", 400, 0},
		{"?ownerDeptId=-1", 400, 0}, {"?userId=7", 400, 0},
	} {
		out, _ := adminRequest(t, ctrl, 7, "list", "GET", "/api/gb28181/openapi-clients"+tc.query, "")
		require.Equal(t, tc.status, out.Code, out.Body.String())
		if tc.status == 200 {
			var body struct {
				Data struct {
					Total int `json:"total"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &body))
			require.Equal(t, tc.total, body.Data.Total)
			require.NotContains(t, out.Body.String(), "hidden")
		}
	}
}

func TestOpenAPIAdminHTTPRejectsEmptyManagementRange(t *testing.T) {
	db, ctrl, _ := newAdminHTTP(t)
	require.NoError(t, db.Model(&basemodels.User{}).Where("id = ?", 7).Update("dept_id", 0).Error)
	for _, action := range []string{"list", "capabilities"} {
		path := "/api/gb28181/openapi-clients"
		if action == "capabilities" {
			path += "/capabilities"
		}
		out, _ := adminRequest(t, ctrl, 7, action, "GET", path, "")
		require.Equal(t, 403, out.Code, out.Body.String())
	}
}

func TestOpenAPIAdminHTTPCreateSecretAndScopedReads(t *testing.T) {
	db, ctrl, _ := newAdminHTTP(t)
	out, marked := adminRequest(t, ctrl, 7, "create", "POST", "/api/gb28181/openapi-clients", `{"name":"A","ownerDeptId":10,"dataScope":4}`)
	require.Equal(t, 200, out.Code, out.Body.String())
	require.True(t, marked)
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	var response struct {
		Data struct {
			Client client.ClientView `json:"client"`
			Secret string            `json:"secretKey"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
	require.NotEmpty(t, response.Data.Secret)
	require.Equal(t, models.DataScopeDepartmentAndChildren, response.Data.Client.DataScope)
	require.NoError(t, db.Create(&models.Client{ID: 90, AK: "test-b", Name: "hidden", OwnerDeptID: 20, Status: models.StatusActive, SecretCiphertext: []byte{1}, SecretIV: []byte{1}, SecretKeyID: "test"}).Error)
	list, _ := adminRequest(t, ctrl, 7, "list", "GET", "/api/gb28181/openapi-clients", "")
	require.Equal(t, 200, list.Code)
	require.NotContains(t, list.Body.String(), "hidden")
	require.NotContains(t, list.Body.String(), response.Data.Secret)
	require.NotContains(t, list.Body.String(), "secretCiphertext")
	missing, _ := adminRequest(t, ctrl, 7, "detail", "GET", "/api/gb28181/openapi-clients/99", "")
	other, _ := adminRequest(t, ctrl, 7, "detail", "GET", "/api/gb28181/openapi-clients/90", "")
	require.Equal(t, 404, missing.Code)
	require.Equal(t, missing.Body.String(), other.Body.String())
	require.NoError(t, db.Model(&basemodels.User{}).Where("id = ?", 7).Update("status", 0).Error)
	require.NoError(t, db.Model(&basemodels.SysRole{}).Where("id = ?", 1).Update("data_scope", 1).Error)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("admin_mask_not_found", func(query *gorm.DB) { query.Statement.RaiseErrorOnNotFound = false }))
	denied, _ := adminRequest(t, ctrl, 7, "list", "GET", "/api/gb28181/openapi-clients", "")
	require.Equal(t, 403, denied.Code)
}

func TestOpenAPIAdminHTTPRejectsUnsupportedClientDataScope(t *testing.T) {
	_, ctrl, _ := newAdminHTTP(t)
	for _, dataScope := range []string{"1", "2", "5"} {
		out, _ := adminRequest(t, ctrl, 7, "create", "POST", "/api/gb28181/openapi-clients", `{"name":"A","ownerDeptId":10,"dataScope":`+dataScope+`}`)
		require.Equal(t, 400, out.Code, out.Body.String())
	}
}

func TestOpenAPIAdminHTTPRejectsAmbiguousCreate(t *testing.T) {
	db, ctrl, _ := newAdminHTTP(t)
	for _, body := range []string{`{"name":"A","ownerDeptId":10,"userId":7}`, `{"name":"A","ownerDeptId":10,"ownerDeptId":20}`, `{"name":"A","ownerDeptId":20}`} {
		out, _ := adminRequest(t, ctrl, 7, "create", "POST", "/api/gb28181/openapi-clients", body)
		require.Contains(t, []int{400, 404}, out.Code, out.Body.String())
	}
	var count int64
	require.NoError(t, db.Model(&models.Client{}).Count(&count).Error)
	require.Zero(t, count)
}
