package routes

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginLogRoutesAreReadOnlyAndCasbinProtected(t *testing.T) {
	text, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	source := string(text)
	casbin := strings.Index(source, "protected.Use(middleware.CasbinMiddleware())")
	list := strings.Index(source, `sysLoginLog.GET("/list"`)
	detail := strings.Index(source, `sysLoginLog.GET("/:id"`)
	require.Greater(t, list, casbin)
	require.Greater(t, detail, casbin)
	require.NotContains(t, source, `sysLoginLog.DELETE(`)
	require.NotContains(t, source, `sysLoginLog.POST(`)
}
