package routes

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginLogRoutesAreCasbinProtected(t *testing.T) {
	text, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	source := string(text)
	casbin := strings.Index(source, "protected.Use(middleware.CasbinMiddleware())")
	list := strings.Index(source, `sysLoginLog.GET("/list"`)
	detail := strings.Index(source, `sysLoginLog.GET("/:id"`)
	delete := strings.Index(source, `sysLoginLog.DELETE("/delete"`)
	clear := strings.Index(source, `sysLoginLog.POST("/clear"`)
	unlock := strings.Index(source, `sysLoginLog.POST("/unlock"`)
	require.Greater(t, list, casbin)
	require.Greater(t, detail, casbin)
	require.Greater(t, delete, casbin)
	require.Greater(t, clear, casbin)
	require.Greater(t, unlock, casbin)
}
