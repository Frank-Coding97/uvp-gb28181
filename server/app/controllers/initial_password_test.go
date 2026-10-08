package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"
	"uvplatform.com/uvp-gb28181/app/middleware"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/service"
	"uvplatform.com/uvp-gb28181/app/utils/passwordhelper"
	"uvplatform.com/uvp-gb28181/app/utils/response"
	"uvplatform.com/uvp-gb28181/app/utils/ymlconfig"
)

func initialPasswordFixture(t *testing.T) (*gorm.DB, *gin.Engine, *service.AuthSessionService) {
	t.Helper()
	oldDB, oldConfig, oldResponse, oldLog := app.GormDbMysql, app.ConfigYml, app.Response, app.ZapLog
	t.Cleanup(func() {
		app.GormDbMysql, app.ConfigYml, app.Response, app.ZapLog = oldDB, oldConfig, oldResponse, oldLog
	})
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte("gormv2:\n  usedbtype: mysql\nsafe:\n  minpasswordlength: 8\n  requirespecialchar: true\nserver:\n  demoaccount:\n    enabled: true\n    defaultusername: admin\n    defaultpassword: old-password!\n"), 0600))
	app.ConfigYml = ymlconfig.CreateYamlFactory(dir)
	app.Response, app.ZapLog = response.NewResponseHandler(), zap.NewNop()
	// 使用临时文件数据库，避免 SQLite 内存库在 GORM 连接池切换连接后看不到表。
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.SysUserSession{}))
	app.GormDbMysql = db
	hash, err := passwordhelper.HashPassword("old-password!")
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.User{BaseModel: models.BaseModel{ID: 1}, Username: "admin", Password: hash, Status: 1, MustChangePassword: true}).Error)
	sessions := service.NewAuthSessionService(db)
	now := time.Now().UTC()
	for _, sid := range []string{"sid-one", "sid-two"} {
		require.NoError(t, sessions.Create(context.Background(), &models.SysUserSession{SID: sid, UserID: 1, LoginAt: now, LastActiveAt: now, SessionExpiresAt: now.Add(time.Hour)}))
	}
	controller := &UserController{AuthSessions: sessions}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.Recovery(), func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 1, Username: "admin"}})
	})
	router.PUT("/api/users/changeInitialPassword", middleware.PasswordValidatorMiddleware(), controller.ChangeInitialPassword)
	router.GET("/api/config/get", NewConfigController().GetConfig)
	router.GET("/api/business", middleware.InitialPasswordMiddleware(), func(c *gin.Context) { c.Status(200) })
	return db, router, sessions
}

func TestInitialPasswordChangeValidationAndSessionRevocation(t *testing.T) {
	for _, tc := range []struct {
		name, password, confirm string
		status                  int
	}{
		{"empty", "", "", 400},
		{"mismatch", "new-password!", "other-password!", 400},
		{"same", "old-password!", "old-password!", 400},
		{"short", "a!", "a!", 400},
		{"no special", "newpassword", "newpassword", 400},
		{"success", "new-password!", "new-password!", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, router, sessions := initialPasswordFixture(t)
			body, err := json.Marshal(map[string]string{"password": tc.password, "confirmPassword": tc.confirm})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPut, "/api/users/changeInitialPassword", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			require.Equal(t, tc.status, res.Code)
			var stored models.User
			require.NoError(t, db.First(&stored, 1).Error)
			if tc.status == 200 {
				require.False(t, stored.MustChangePassword)
				require.NoError(t, passwordhelper.ComparePassword(stored.Password, tc.password))
				require.Error(t, passwordhelper.ComparePassword(stored.Password, "old-password!"))
				for _, sid := range []string{"sid-one", "sid-two"} {
					require.Error(t, sessions.ValidateSession(context.Background(), sid, 1))
				}
			} else {
				require.True(t, stored.MustChangePassword)
				var session models.SysUserSession
				require.NoError(t, db.Where("sid = ?", "sid-one").First(&session).Error)
				require.Nil(t, session.RevokedAt)
				require.NoError(t, passwordhelper.ComparePassword(stored.Password, "old-password!"))
			}
		})
	}
}

func TestInitialPasswordGateAndHintsFollowPersistedState(t *testing.T) {
	db, router, _ := initialPasswordFixture(t)
	for _, pending := range []bool{true, false} {
		require.NoError(t, db.Model(&models.User{}).Where("id = 1").Update("must_change_password", pending).Error)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("GET", "/api/business", nil))
		if pending {
			require.Equal(t, 403, res.Code)
		} else {
			require.Equal(t, 200, res.Code)
		}
		config := httptest.NewRecorder()
		router.ServeHTTP(config, httptest.NewRequest("GET", "/api/config/get", nil))
		require.Equal(t, 200, config.Code)
		var payload struct {
			Data struct {
				System struct {
					Username string `json:"defaultusername"`
					Password string `json:"defaultpassword"`
				} `json:"system"`
				Safe struct {
					Min int `json:"minPasswordLength"`
				} `json:"safe"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(config.Body.Bytes(), &payload))
		require.Equal(t, 8, payload.Data.Safe.Min)
		if pending {
			require.NotEmpty(t, payload.Data.System.Username)
			require.NotEmpty(t, payload.Data.System.Password)
		} else {
			require.Empty(t, payload.Data.System.Username)
			require.Empty(t, payload.Data.System.Password)
		}
	}
}

func TestInitialPasswordChangeRollsBackWhenSessionRevocationFails(t *testing.T) {
	db, router, _ := initialPasswordFixture(t)
	require.NoError(t, db.Migrator().DropTable(&models.SysUserSession{}))
	req := httptest.NewRequest(http.MethodPut, "/api/users/changeInitialPassword", bytes.NewBufferString(`{"password":"new-password!","confirmPassword":"new-password!"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	require.Equal(t, http.StatusServiceUnavailable, res.Code)
	var stored models.User
	require.NoError(t, db.First(&stored, 1).Error)
	require.True(t, stored.MustChangePassword)
	require.NoError(t, passwordhelper.ComparePassword(stored.Password, "old-password!"))
}

func TestInitialPasswordEndpointRejectsAlreadyCompletedSetup(t *testing.T) {
	db, router, _ := initialPasswordFixture(t)
	require.NoError(t, db.Model(&models.User{}).Where("id = 1").Update("must_change_password", false).Error)
	req := httptest.NewRequest(http.MethodPut, "/api/users/changeInitialPassword", bytes.NewBufferString(`{"password":"new-password!","confirmPassword":"new-password!"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	require.Equal(t, http.StatusBadRequest, res.Code)
	var stored models.User
	require.NoError(t, db.First(&stored, 1).Error)
	require.NoError(t, passwordhelper.ComparePassword(stored.Password, "old-password!"))
}
