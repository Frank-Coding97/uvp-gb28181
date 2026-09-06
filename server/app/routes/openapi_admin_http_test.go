package routes

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	openapimodels "uvplatform.cn/uvp-gb28181/app/openapi/models"
	appservice "uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/casbinhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"

	"github.com/gin-gonic/gin"
)

const openAPIAdminHTTPModel = `
[request_definition]
r = sub, obj, act, dom
[policy_definition]
p = sub, obj, act, dom
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub, r.dom) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act) && (r.dom == p.dom || p.dom == "*")
`

const openAPIAdminHTTPPathPrefix = "/api/gb28181/openapi-clients"

type openAPIAdminHTTPConfig struct {
	app.YmlConfigInterf
	staticDir string
}

func (c openAPIAdminHTTPConfig) GetString(key string) string {
	switch key {
	case "httpserver.serverrootpath":
		return "/__static-admin-http"
	case "httpserver.serverroot":
		return c.staticDir
	case "openapi.audience":
		return "admin-http-audience"
	case "openapi.master_key_id":
		return "admin-http-test"
	case "gormv2.usedbtype":
		return "mysql"
	case "casbin.tableprefix":
		return "sys_"
	case "casbin.tablename":
		return "casbin_rule"
	default:
		return ""
	}
}

func (c openAPIAdminHTTPConfig) GetBool(key string) bool {
	return key == "openapi.enabled" || key == "server.syslog"
}

func (c openAPIAdminHTTPConfig) GetInt(key string) int {
	switch key {
	case "openapi.request_timeout_seconds":
		return 5
	case "httpserver.write_timeout":
		return 30
	default:
		return 0
	}
}

func (c openAPIAdminHTTPConfig) GetStringSlice(string) []string { return nil }
func (c openAPIAdminHTTPConfig) GetUintSlice(string) []uint     { return nil }

type openAPIAdminHTTPResponse struct {
	status  int
	headers http.Header
	body    []byte
}

type openAPIAdminHTTPFixture struct {
	db       *gorm.DB
	helper   *casbinhelper.CasbinHelper
	sessions *appservice.AuthSessionService
	server   *httptest.Server
	client   *http.Client

	adminToken    string
	adminSID      string
	noPermToken   string
	deadUserToken string

	requestCount atomic.Int64
}

func TestOpenAPIAdminRootRealHTTPBoundary(t *testing.T) {
	fixture := newOpenAPIAdminHTTPFixture(t)

	create := fixture.do(t, fixture.adminToken, http.MethodPost, "/api/gb28181/openapi-clients", `{"name":"现场集成客户端","ownerDeptId":10}`)
	require.Equal(t, http.StatusOK, create.status)
	require.Equal(t, "no-store", create.headers.Get("Cache-Control"))
	var createBody struct {
		Data struct {
			Client    client.ClientView `json:"client"`
			SecretKey string            `json:"secretKey"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(create.body, &createBody))
	require.True(t, createBody.Data.Client.ID > 0)
	require.True(t, createBody.Data.SecretKey != "")
	require.Equal(t, 1, strings.Count(string(create.body), `"secretKey"`))
	initialSecret := createBody.Data.SecretKey

	insertHiddenAdminClient(t, fixture.db)
	crossOwner := fixture.do(t, fixture.adminToken, http.MethodPost, "/api/gb28181/openapi-clients", `{"name":"越权客户端","ownerDeptId":20}`)
	require.Equal(t, http.StatusNotFound, crossOwner.status)

	list := fixture.do(t, fixture.adminToken, http.MethodGet, "/api/gb28181/openapi-clients?ownerDeptId=10", "")
	require.Equal(t, http.StatusOK, list.status)
	var listBody struct {
		Data struct {
			Total            int `json:"total"`
			OwnerDepartments []struct {
				ID uint `json:"id"`
			} `json:"ownerDepartments"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.body, &listBody))
	require.Equal(t, 1, listBody.Data.Total)
	require.Len(t, listBody.Data.OwnerDepartments, 1)
	require.EqualValues(t, 10, listBody.Data.OwnerDepartments[0].ID)
	require.False(t, strings.Contains(string(list.body), "hidden-admin-client"))
	require.False(t, strings.Contains(string(list.body), initialSecret))

	clientPath := "/api/gb28181/openapi-clients/" + strconv.FormatInt(createBody.Data.Client.ID, 10)
	detail := fixture.do(t, fixture.adminToken, http.MethodGet, clientPath, "")
	require.Equal(t, http.StatusOK, detail.status)
	require.False(t, strings.Contains(string(detail.body), `"secretKey"`))
	require.False(t, strings.Contains(string(detail.body), initialSecret))

	hiddenDetail := fixture.do(t, fixture.adminToken, http.MethodGet, "/api/gb28181/openapi-clients/90", "")
	require.Equal(t, http.StatusNotFound, hiddenDetail.status)

	grant := fixture.do(t, fixture.adminToken, http.MethodPut, clientPath+"/scopes", `{"rowVersion":1,"scopes":["device:list"]}`)
	require.Equal(t, http.StatusOK, grant.status)

	rotate := fixture.do(t, fixture.adminToken, http.MethodPost, clientPath+"/rotate-secret", `{"rowVersion":2}`)
	require.Equal(t, http.StatusOK, rotate.status)
	require.Equal(t, "no-store", rotate.headers.Get("Cache-Control"))
	var rotateBody struct {
		Data struct {
			SecretKey string `json:"secretKey"`
			Client    struct {
				RowVersion int64 `json:"rowVersion"`
			} `json:"client"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rotate.body, &rotateBody))
	require.True(t, rotateBody.Data.SecretKey != "")
	require.True(t, rotateBody.Data.SecretKey != initialSecret)
	require.Equal(t, 1, strings.Count(string(rotate.body), `"secretKey"`))
	require.Equal(t, int64(3), rotateBody.Data.Client.RowVersion)
	rotatedSecret := rotateBody.Data.SecretKey

	for _, action := range []string{"disable", "revoke"} {
		response := fixture.do(t, fixture.adminToken, http.MethodPost, clientPath+"/"+action, `{"rowVersion":3}`)
		require.Equal(t, http.StatusServiceUnavailable, response.status)
		require.Equal(t, "no-store", response.headers.Get("Cache-Control"))
	}
	var stored openapimodels.Client
	require.NoError(t, fixture.db.First(&stored, createBody.Data.Client.ID).Error)
	require.Equal(t, openapimodels.StatusActive, stored.Status)
	require.Equal(t, int64(3), stored.RowVersion)

	noPermission := fixture.do(t, fixture.noPermToken, http.MethodGet, "/api/gb28181/openapi-clients", "")
	require.Equal(t, http.StatusForbidden, noPermission.status)

	require.NoError(t, fixture.db.Model(&appmodels.User{}).Where("id = ?", 9).Update("status", 0).Error)
	deadUser := fixture.do(t, fixture.deadUserToken, http.MethodGet, "/api/gb28181/openapi-clients", "")
	require.Equal(t, http.StatusForbidden, deadUser.status)

	badTokenService := &tokenhelper.TokenService{JWTSecret: "another-fixed-jwt-test-key", TokenExpire: 3600}
	badJWT, err := badTokenService.GenerateTokenForSession(&app.ClaimsUser{UserID: 7, Username: "admin-http"}, fixture.adminSID, "bad-jti")
	require.NoError(t, err)
	invalidJWT := fixture.do(t, badJWT, http.MethodGet, "/api/gb28181/openapi-clients", "")
	require.Equal(t, http.StatusUnauthorized, invalidJWT.status)

	_, err = fixture.sessions.Revoke(context.Background(), fixture.adminSID, "http-integration-test", nil)
	require.NoError(t, err)
	revokedSession := fixture.do(t, fixture.adminToken, http.MethodGet, "/api/gb28181/openapi-clients", "")
	require.Equal(t, http.StatusUnauthorized, revokedSession.status)

	logs := waitForAdminOperationLogs(t, fixture.db, fixture.requestCount.Load())
	var httpLogCount, serviceLogCount int64
	for _, log := range logs {
		if isOpenAPIAdminHTTPLog(log) {
			httpLogCount++
		}
		if log.Method == "SERVICE" && isOpenAPIAdminHTTPPath(log.Path) {
			serviceLogCount++
		}
	}
	require.Equal(t, fixture.requestCount.Load(), httpLogCount)
	require.Equal(t, int64(3), serviceLogCount, "create, scope grant, and rotate write synchronous SERVICE logs")
	serializedLogs, err := json.Marshal(logs)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(serializedLogs), initialSecret))
	require.False(t, strings.Contains(string(serializedLogs), rotatedSecret))
	require.False(t, strings.Contains(string(serializedLogs), `"secretKey"`))
}

func newOpenAPIAdminHTTPFixture(t *testing.T) *openAPIAdminHTTPFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "openapi-admin-http.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(8)
	raw.SetMaxIdleConns(8)

	oldConfig, oldDB, oldSQLServer, oldPostgres, oldCasbin, oldToken, oldSession, oldLog := app.ConfigYml, app.GormDbMysql, app.GormDbSqlserver, app.GormDbPostgreSql, app.CasbinV2, app.TokenService, app.SessionValidator, app.ZapLog
	fixture := &openAPIAdminHTTPFixture{db: db}
	cleanup := func() {
		if fixture.server != nil {
			fixture.server.Close()
		}
		if fixture.requestCount.Load() > 0 {
			waitForAdminOperationLogsBestEffort(db, fixture.requestCount.Load())
		}
		if fixture.helper != nil {
			fixture.helper.Close()
		}
		app.ConfigYml, app.GormDbMysql, app.GormDbSqlserver, app.GormDbPostgreSql = oldConfig, oldDB, oldSQLServer, oldPostgres
		app.CasbinV2, app.TokenService, app.SessionValidator, app.ZapLog = oldCasbin, oldToken, oldSession, oldLog
		_ = raw.Close()
	}
	t.Cleanup(cleanup)

	config := openAPIAdminHTTPConfig{staticDir: t.TempDir()}
	app.ConfigYml, app.GormDbMysql, app.GormDbSqlserver, app.GormDbPostgreSql = config, db, nil, nil
	app.ZapLog = zap.NewNop()
	t.Setenv("UVP_OPENAPI_MASTER_KEY", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32)))
	require.NoError(t, seedOpenAPIAdminHTTPSchema(db))
	require.NoError(t, seedOpenAPIAdminHTTPData(db))

	helper := casbinhelper.NewCasbinHelper()
	app.CasbinV2 = helper
	require.NoError(t, helper.InitCasbin(db, openAPIAdminHTTPModel))
	fixture.helper = helper
	for _, userID := range []uint{7, 9} {
		require.NoError(t, helper.AddRolesForUserByID(userID, []uint{1}))
	}
	require.NoError(t, helper.AddRolesForUserByID(8, []uint{2}))
	for _, route := range openAPIAdminHTTPRoutes() {
		require.NoError(t, helper.AddPolicyForRole(1, route.path, route.method))
	}

	tokens := &tokenhelper.TokenService{JWTSecret: "fixed-admin-http-jwt-test-key", TokenExpire: 3600, RefreshExpire: 3600}
	sessions := appservice.NewAuthSessionService(db)
	app.TokenService, app.SessionValidator = tokens, sessions
	fixture.sessions = sessions
	fixture.adminToken, fixture.adminSID = createAdminHTTPLogin(t, db, sessions, tokens, 7)
	fixture.noPermToken, _ = createAdminHTTPLogin(t, db, sessions, tokens, 8)
	fixture.deadUserToken, _ = createAdminHTTPLogin(t, db, sessions, tokens, 9)

	gin.SetMode(gin.TestMode)
	root := gin.New()
	InitRoutes(root)
	fixture.server = httptest.NewTLSServer(root)
	fixture.client = fixture.server.Client()
	return fixture
}

type openAPIAdminHTTPRoute struct {
	path   string
	method string
}

func openAPIAdminHTTPRoutes() []openAPIAdminHTTPRoute {
	const base = openAPIAdminHTTPPathPrefix
	return []openAPIAdminHTTPRoute{
		{path: base, method: http.MethodGet},
		{path: base, method: http.MethodPost},
		{path: base + "/capabilities", method: http.MethodGet},
		{path: base + "/:id", method: http.MethodGet},
		{path: base + "/:id/scopes", method: http.MethodPut},
		{path: base + "/:id/rotate-secret", method: http.MethodPost},
		{path: base + "/:id/enable", method: http.MethodPost},
		{path: base + "/:id/disable", method: http.MethodPost},
		{path: base + "/:id/revoke", method: http.MethodPost},
		{path: base + "/:id/audits", method: http.MethodGet},
		{path: base + "/:id/revocation-status", method: http.MethodGet},
	}
}

func (f *openAPIAdminHTTPFixture) do(t *testing.T, token, method, path, body string) openAPIAdminHTTPResponse {
	t.Helper()
	f.requestCount.Add(1)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, f.server.URL+path, reader)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := f.client.Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	require.NoError(t, err)
	return openAPIAdminHTTPResponse{status: response.StatusCode, headers: response.Header.Clone(), body: data}
}

func seedOpenAPIAdminHTTPSchema(db *gorm.DB) error {
	return db.AutoMigrate(
		&openapimodels.SecurityState{},
		&openapimodels.Client{}, &openapimodels.ClientScope{}, &openapimodels.Nonce{}, &openapimodels.Audit{},
		&appmodels.SysDepartment{}, &appmodels.User{}, &appmodels.SysRole{}, &appmodels.SysUserRole{}, &appmodels.SysUserSession{}, &appmodels.SysOperationLog{},
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{},
	)
}

func seedOpenAPIAdminHTTPData(db *gorm.DB) error {
	if err := db.Create(&openapimodels.SecurityState{ID: 1}).Error; err != nil {
		return err
	}
	active := int8(1)
	departments := []appmodels.SysDepartment{
		{BaseModel: appmodels.BaseModel{ID: 10}, Name: "现场部门", Status: &active},
		{BaseModel: appmodels.BaseModel{ID: 20}, Name: "其他部门", Status: &active},
	}
	if err := db.Create(&departments).Error; err != nil {
		return err
	}
	users := []appmodels.User{
		{BaseModel: appmodels.BaseModel{ID: 7}, Username: "admin-http", Password: "fixture-only", Status: 1, DeptID: 10},
		{BaseModel: appmodels.BaseModel{ID: 8}, Username: "no-permission-http", Password: "fixture-only", Status: 1, DeptID: 10},
		{BaseModel: appmodels.BaseModel{ID: 9}, Username: "disabled-http", Password: "fixture-only", Status: 1, DeptID: 10},
	}
	if err := db.Create(&users).Error; err != nil {
		return err
	}
	roles := []appmodels.SysRole{
		{BaseModel: appmodels.BaseModel{ID: 1}, Name: "admin-http", Status: 1, DataScope: 2, CheckedDepts: "10"},
		{BaseModel: appmodels.BaseModel{ID: 2}, Name: "no-permission-http", Status: 1, DataScope: 2, CheckedDepts: "10"},
	}
	if err := db.Create(&roles).Error; err != nil {
		return err
	}
	return db.Create(&[]appmodels.SysUserRole{{UserID: 7, RoleID: 1}, {UserID: 8, RoleID: 2}, {UserID: 9, RoleID: 1}}).Error
}

func createAdminHTTPLogin(t *testing.T, db *gorm.DB, sessions *appservice.AuthSessionService, tokens *tokenhelper.TokenService, userID uint) (string, string) {
	t.Helper()
	var user appmodels.User
	require.NoError(t, db.First(&user, userID).Error)
	pair, err := sessions.CreateLogin(context.Background(), &user, appservice.LoginMetadata{ClientIP: "127.0.0.1", LoginLocation: "内网", UserAgent: "admin-http-test", Browser: "test", OS: "test"}, tokens, time.Hour)
	require.NoError(t, err)
	return pair.AccessToken, pair.SID
}

func insertHiddenAdminClient(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&openapimodels.Client{
		ID: 90, AK: "uvp_hidden_admin_http", Name: "hidden-admin-client", OwnerDeptID: 20, Status: openapimodels.StatusActive,
		SecretCiphertext: []byte{1}, SecretIV: []byte{2}, SecretKeyID: "fixture", SecretVersion: 1, AuthEpoch: 1,
		RateLimit: 10, Burst: 20, ViewerQuota: 10, RowVersion: 1, CreatedBy: 7, UpdatedBy: 7,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error)
}

func waitForAdminOperationLogs(t *testing.T, db *gorm.DB, minimum int64) []appmodels.SysOperationLog {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		logs, err := queryOpenAPIAdminHTTPLogs(db)
		if err == nil && int64(len(logs)) >= minimum {
			var allLogs []appmodels.SysOperationLog
			require.NoError(t, db.Order("id ASC").Find(&allLogs).Error)
			return allLogs
		}
		time.Sleep(10 * time.Millisecond)
	}
	var logs []appmodels.SysOperationLog
	require.NoError(t, db.Order("id ASC").Find(&logs).Error)
	var httpLogCount int64
	for _, log := range logs {
		if isOpenAPIAdminHTTPLog(log) {
			httpLogCount++
		}
	}
	require.GreaterOrEqual(t, httpLogCount, minimum)
	return logs
}

func waitForAdminOperationLogsBestEffort(db *gorm.DB, minimum int64) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		logs, err := queryOpenAPIAdminHTTPLogs(db)
		if err == nil && int64(len(logs)) >= minimum {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queryOpenAPIAdminHTTPLogs(db *gorm.DB) ([]appmodels.SysOperationLog, error) {
	var logs []appmodels.SysOperationLog
	result := db.Where("method <> ? AND (path = ? OR path LIKE ?)", "SERVICE", openAPIAdminHTTPPathPrefix, openAPIAdminHTTPPathPrefix+"/%").Order("id ASC").Find(&logs)
	return logs, result.Error
}

func isOpenAPIAdminHTTPPath(path string) bool {
	return path == openAPIAdminHTTPPathPrefix || strings.HasPrefix(path, openAPIAdminHTTPPathPrefix+"/")
}

func isOpenAPIAdminHTTPLog(log appmodels.SysOperationLog) bool {
	return log.Method != "SERVICE" && isOpenAPIAdminHTTPPath(log.Path)
}
