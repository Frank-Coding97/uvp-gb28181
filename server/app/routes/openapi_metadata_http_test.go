package routes

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type openAPIEnabledConfig struct{ openAPIRootConfig }

func (c openAPIEnabledConfig) GetString(key string) string {
	switch key {
	case "openapi.audience":
		return "root-audience"
	case "openapi.master_key_id":
		return "test"
	case "gormv2.usedbtype":
		return "mysql"
	default:
		return c.openAPIRootConfig.GetString(key)
	}
}
func (c openAPIEnabledConfig) GetBool(key string) bool {
	return key == "server.syslog" || c.openAPIRootConfig.GetBool(key)
}
func (c openAPIEnabledConfig) GetInt(key string) int {
	if key == "httpserver.write_timeout" {
		return 30
	}
	return 0
}

func TestOpenAPIRootMetadataUsesHMACAndExactOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "http.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}, &basemodels.SysDepartment{}, &basemodels.SysOperationLog{}, &gbmodels.GbDevice{}, &gbmodels.GbChannel{}))
	active := int8(1)
	require.NoError(t, db.Create(&[]basemodels.SysDepartment{{BaseModel: basemodels.BaseModel{ID: 10}, Status: &active}, {BaseModel: basemodels.BaseModel{ID: 20}, Status: &active}}).Error)
	const device = "34020000002000000010"
	const other = "34020000002000000020"
	const channel = "34020000001320000010"
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{{DeviceID: device, Name: "owned", OwnerDeptID: 10, Status: 1}, {DeviceID: other, Name: "hidden", OwnerDeptID: 20, Status: 1}}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: device, ChannelID: channel, Name: "owned-channel", OwnerDeptID: 10, Status: 1}).Error)
	master := bytes.Repeat([]byte{1}, 32)
	t.Setenv("UVP_OPENAPI_MASTER_KEY", base64.RawURLEncoding.EncodeToString(master))
	keys, err := client.NewSecretManager(master, "test")
	require.NoError(t, err)
	ak := "uvp_000102030405060708090a0b0c0d0e0f"
	sk := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	ciphertext, iv, err := keys.Encrypt(1, ak, 1, sk)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.Client{ID: 1, AK: ak, Name: "machine", OwnerDeptID: 10, Status: models.StatusActive, SecretCiphertext: ciphertext, SecretIV: iv, SecretKeyID: "test"}).Error)
	for _, scope := range client.SupportedScopes() {
		require.NoError(t, db.Create(&models.ClientScope{ClientID: 1, Scope: scope, Enabled: true}).Error)
	}
	oldConfig, oldDB, oldCasbin, oldLog := app.ConfigYml, app.GormDbMysql, app.CasbinV2, app.ZapLog
	app.ConfigYml = openAPIEnabledConfig{openAPIRootConfig{staticDir: t.TempDir(), enabled: true}}
	app.GormDbMysql, app.CasbinV2, app.ZapLog = db, openAPIRootCasbin{}, zap.NewNop()
	t.Cleanup(func() { app.ConfigYml, app.GormDbMysql, app.CasbinV2, app.ZapLog = oldConfig, oldDB, oldCasbin, oldLog })
	gin.SetMode(gin.TestMode)
	root := gin.New()
	InitRoutes(root)
	server := httptest.NewTLSServer(root)
	defer server.Close()
	httpClient := server.Client()
	sequence := 0
	call := func(method, path string, signed bool) (int, string) {
		t.Helper()
		sequence++
		request, err := http.NewRequest(method, server.URL+path, nil)
		require.NoError(t, err)
		request.Header.Set("X-Request-Id", "forged-client-id")
		if signed {
			input := auth.SignatureInput{Method: method, Path: request.URL.Path, RawQuery: request.URL.RawQuery, AccessKey: ak, Timestamp: fmt.Sprint(time.Now().Unix()), Nonce: fmt.Sprintf("%032x", sequence), Audience: "root-audience"}
			signature, err := auth.Sign(input, sk)
			require.NoError(t, err)
			for key, value := range map[string]string{"X-UVP-Sign-Version": "1", "X-UVP-Access-Key": ak, "X-UVP-Timestamp": input.Timestamp, "X-UVP-Nonce": input.Nonce, "X-UVP-Signature": signature} {
				request.Header.Set(key, value)
			}
		}
		response, err := httpClient.Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 65536))
		require.NoError(t, err)
		require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
		require.NotContains(t, string(body), "forged-client-id")
		return response.StatusCode, string(body)
	}
	for _, path := range []string{"/devices", "/devices/" + device, "/devices/" + device + "/status", "/devices/" + device + "/channels", "/devices/" + device + "/channels/" + channel, "/devices/" + device + "/channels/" + channel + "/status"} {
		status, body := call("GET", "/openapi/v1"+path, true)
		require.Equal(t, 200, status, body)
		require.NotContains(t, body, "hidden")
		require.NotContains(t, body, "ownerDeptId")
	}
	status, body := call("GET", "/openapi/v1/devices/"+other, true)
	require.Equal(t, 404, status, body)
	status, body = call("GET", "/openapi/v1/devices", false)
	require.Equal(t, 401, status, body)
	require.NoError(t, db.Where("client_id = ? AND scope = ?", 1, "device:list").Delete(&models.ClientScope{}).Error)
	status, body = call("GET", "/openapi/v1/devices", true)
	require.Equal(t, 403, status, body)
	status, body = call("POST", "/openapi/v1/devices/"+device+"/channels/"+channel+"/live-authorizations", false)
	require.Equal(t, 503, status, body)
	var n int64
	require.NoError(t, db.Model(&models.Nonce{}).Count(&n).Error)
	require.EqualValues(t, 6, n)
	require.NoError(t, db.Model(&models.Audit{}).Where("result = ?", "success").Count(&n).Error)
	require.EqualValues(t, 6, n)
	require.NoError(t, db.Model(&basemodels.SysOperationLog{}).Count(&n).Error)
	require.Zero(t, n, "external traffic must bypass global body logging")
	require.False(t, strings.Contains(body, sk))
}
