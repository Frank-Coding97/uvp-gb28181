//go:build integration

package controllers

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/cachehelper"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/passwordhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

func TestLoginLogMySQLRealAuthenticationBranches(t *testing.T) {
	if os.Getenv("UVP_RUN_MYSQL_INTEGRATION") != "1" {
		t.Skip("set UVP_RUN_MYSQL_INTEGRATION=1 to verify the configured development MySQL")
	}
	oldDB, oldConfig, oldCache := app.GormDbMysql, app.ConfigYml, app.Cache
	oldResponse, oldLog, oldRecorder := app.Response, app.ZapLog, app.LoginLogRecorder
	t.Cleanup(func() {
		app.GormDbMysql, app.ConfigYml, app.Cache = oldDB, oldConfig, oldCache
		app.Response, app.ZapLog, app.LoginLogRecorder = oldResponse, oldLog, oldRecorder
	})
	app.ConfigYml = ymlconfig.CreateYamlFactory("../../config")
	app.ZapLog = zap.NewNop()
	db, err := gormhelper.GetOneMysqlClient()
	require.NoError(t, err)
	rawDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawDB.Close() })
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { tx.Rollback() })
	app.GormDbMysql = tx
	app.Cache = cachehelper.NewMemoryHelper()
	app.Response = response.NewResponseHandler()
	app.LoginLogRecorder = service.NewLoginLogService(tx)

	username := "login-log-integration-" + time.Now().Format("150405.000000000")
	password := "integration-password"
	hash, err := passwordhelper.HashPassword(password)
	require.NoError(t, err)
	user := &models.User{Username: username, Password: hash, Status: 1, Description: "login log integration"}
	require.NoError(t, tx.Create(user).Error)

	tokens := &tokenhelper.TokenService{JWTSecret: "integration_secret", TokenExpire: 3600, RefreshExpire: 86400}
	access, err := tokens.GenerateTokenForSession(&app.ClaimsUser{UserID: user.ID, Username: username}, "sid-integration", "access-jti")
	require.NoError(t, err)
	refresh, err := tokens.GenerateRefreshTokenForSessionUntil(user.ID, "sid-integration", "refresh-jti", time.Now().Add(time.Hour))
	require.NoError(t, err)
	controller := newAuthControllerWithDependencies(&fakeAuthSessionLifecycle{
		createPair: &service.SessionTokenPair{AccessToken: access, RefreshToken: refresh, SID: "sid-integration"},
	}, tokens, time.Hour)

	require.Equal(t, 200, invokeLogin(t, controller, username, password).Code)
	require.Equal(t, 400, invokeLogin(t, controller, username, "wrong-password").Code)
	require.Equal(t, 400, invokeLogin(t, controller, username+"-missing", "wrong-password").Code)

	var events []models.SysLoginLog
	require.NoError(t, tx.Where("username IN ?", []string{username, username + "-missing"}).Order("id").Find(&events).Error)
	require.Len(t, events, 3)
	require.Equal(t, service.LoginResultSuccess, events[0].Result)
	require.Equal(t, service.LoginFailurePasswordIncorrect, events[1].FailureReason)
	require.Equal(t, service.LoginFailureUserNotFound, events[2].FailureReason)
	require.Nil(t, events[2].UserID)
	for _, event := range events {
		require.NotContains(t, event.UserAgent, password)
		require.NotContains(t, event.UserAgent, "wrong-password")
	}
}
