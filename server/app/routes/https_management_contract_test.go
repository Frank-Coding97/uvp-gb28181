package routes

import (
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
)

func TestHTTPSManagementRoutesUseProtectedGroup(t *testing.T) {
	source, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	text := string(source)
	anchor := strings.Index(text, "httpsManagement := protected.Group(\"/system/https\")")
	for _, gate := range []string{"JWTAuthMiddleware", "InitialPasswordMiddleware", "DemoAccountMiddleware", "UpgradeCandidateMiddleware", "CasbinMiddleware"} {
		require.Greater(t, anchor, strings.Index(text, "protected.Use(middleware."+gate+"())"))
	}
	for _, route := range []string{"httpsManagement.GET(\"/status\"", "httpsManagement.POST(\"/check-port\"", "httpsManagement.POST(\"/certificate\"", "httpsManagement.POST(\"/apply\""} {
		require.Contains(t, text, route)
	}
}
