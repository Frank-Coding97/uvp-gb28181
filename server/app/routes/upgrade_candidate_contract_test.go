package routes

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

func TestProtectedRoutesInstallUpgradeCandidateGate(t *testing.T) {
	body, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, "protected.Use(middleware.UpgradeCandidateMiddleware())")
	require.True(t, strings.Index(text, "protected.Use(middleware.UpgradeCandidateMiddleware())") < strings.Index(text, "protected.Use(middleware.CasbinMiddleware())"))
}

func TestInitRoutesDoesNotStartAutomaticUpgradeScheduler(t *testing.T) {
	oldStart := startAutoScheduler
	called := false
	startAutoScheduler = func(context.Context) { called = true }
	t.Cleanup(func() { startAutoScheduler = oldStart })

	oldConfig := app.ConfigYml
	oldDB := app.GormDbMysql
	oldCasbin := app.CasbinV2
	oldToken := app.TokenService
	oldSession := app.SessionValidator
	app.ConfigYml = openAPIRootConfig{staticDir: t.TempDir()}
	app.GormDbMysql = openAPISecurityDB(t)
	app.CasbinV2 = openAPIRootCasbin{}
	app.TokenService = openAPIRootTokens{}
	app.SessionValidator = openAPIRootSessions{}
	t.Cleanup(func() {
		app.ConfigYml = oldConfig
		app.GormDbMysql = oldDB
		app.CasbinV2 = oldCasbin
		app.TokenService = oldToken
		app.SessionValidator = oldSession
	})

	gin.SetMode(gin.TestMode)
	InitRoutes(gin.New())
	require.False(t, called, "route construction must not start the scheduler before candidate admission")
}

func TestStartAutoSchedulerForwardsRuntimeContext(t *testing.T) {
	oldStart := startAutoScheduler
	var got context.Context
	startAutoScheduler = func(ctx context.Context) { got = ctx }
	t.Cleanup(func() { startAutoScheduler = oldStart })

	want := context.WithValue(context.Background(), struct{}{}, "maintenance")
	StartAutoScheduler(want)
	require.Same(t, want, got)
}
