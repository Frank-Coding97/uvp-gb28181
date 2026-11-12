package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func TestUpgradeCandidateMiddlewareAllowsUpgradeReadsAndBlocksBusinessWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("UVP_UPDATER_STATE", t.TempDir())
	root := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", root)
	store, err := upgrade.OpenGateStore(filepath.Join(root, "gate.json"))
	require.NoError(t, err)
	require.NoError(t, store.Save(upgrade.GateState{Mode: upgrade.GateCandidate}))

	engine := gin.New()
	engine.Use(UpgradeCandidateMiddleware())
	engine.GET("/api/system/upgrade/status", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.POST("/api/system/upgrade/check", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.POST("/api/system/upgrade/tasks", func(c *gin.Context) { c.Status(http.StatusCreated) })
	engine.PUT("/api/system/upgrade/policy", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.POST("/api/gb28181/devices", func(c *gin.Context) { c.Status(http.StatusCreated) })

	read := httptest.NewRecorder()
	engine.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/system/upgrade/status", nil))
	require.Equal(t, http.StatusOK, read.Code)
	check := httptest.NewRecorder()
	engine.ServeHTTP(check, httptest.NewRequest(http.MethodPost, "/api/system/upgrade/check", nil))
	require.Equal(t, http.StatusOK, check.Code)

	write := httptest.NewRecorder()
	engine.ServeHTTP(write, httptest.NewRequest(http.MethodPost, "/api/gb28181/devices", nil))
	require.Equal(t, http.StatusServiceUnavailable, write.Code)
	createTask := httptest.NewRecorder()
	engine.ServeHTTP(createTask, httptest.NewRequest(http.MethodPost, "/api/system/upgrade/tasks", nil))
	require.Equal(t, http.StatusServiceUnavailable, createTask.Code)
	changePolicy := httptest.NewRecorder()
	engine.ServeHTTP(changePolicy, httptest.NewRequest(http.MethodPut, "/api/system/upgrade/policy", nil))
	require.Equal(t, http.StatusServiceUnavailable, changePolicy.Code)
}

func TestUpgradeCandidateMiddlewarePassesThroughNormalGate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("UVP_UPDATER_STATE", root)
	store, err := upgrade.OpenGateStore(filepath.Join(root, "gate.json"))
	require.NoError(t, err)
	require.NoError(t, store.Save(upgrade.GateState{Mode: upgrade.GateNormal}))

	engine := gin.New()
	engine.Use(UpgradeCandidateMiddleware())
	engine.POST("/api/gb28181/devices", func(c *gin.Context) { c.Status(http.StatusCreated) })
	result := httptest.NewRecorder()
	engine.ServeHTTP(result, httptest.NewRequest(http.MethodPost, "/api/gb28181/devices", nil))
	require.Equal(t, http.StatusCreated, result.Code)
}

func TestUpgradeMiddlewareBlocksEveryBusinessIngressForNonNormalGate(t *testing.T) {
	for _, mode := range []upgrade.GateMode{upgrade.GateCandidate, upgrade.GateBlocked, upgrade.GateRecovery, upgrade.GateMode("unexpected")} {
		t.Run(string(mode), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("UVP_UPDATER_STATE", root)
			store, err := upgrade.OpenGateStore(filepath.Join(root, "gate.json"))
			require.NoError(t, err)
			require.NoError(t, store.Save(upgrade.GateState{Mode: mode}))
			invoked := false
			engine := gin.New()
			engine.Use(UpgradeCandidateMiddleware())
			for _, path := range []string{"/index.html", "/api/login", "/index/hook/on_publish", "/openapi/v1/devices", "/plugins/example"} {
				engine.Any(path, func(c *gin.Context) { invoked = true; c.Status(http.StatusOK) })
				result := httptest.NewRecorder()
				engine.ServeHTTP(result, httptest.NewRequest(http.MethodPost, path, nil))
				require.Equal(t, http.StatusServiceUnavailable, result.Code, path)
			}
			require.False(t, invoked)
			// Reads needed to inspect an existing upgrade remain available.
			engine.GET("/api/system/upgrade/status", func(c *gin.Context) { c.Status(http.StatusOK) })
			result := httptest.NewRecorder()
			engine.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/api/system/upgrade/status", nil))
			require.Equal(t, http.StatusOK, result.Code)
			require.NoError(t, store.Save(upgrade.GateState{Mode: upgrade.GateNormal}))
			resumed := httptest.NewRecorder()
			engine.ServeHTTP(resumed, httptest.NewRequest(http.MethodPost, "/api/login", nil))
			require.Equal(t, http.StatusOK, resumed.Code)
		})
	}
}
