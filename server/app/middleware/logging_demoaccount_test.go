package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/ginhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type demoAccountLoggingConfig struct {
	allowPathPrefixes []string
}

func (demoAccountLoggingConfig) ConfigFileChangeListen(...func()) {}
func (demoAccountLoggingConfig) Get(string) interface{}           { return nil }
func (demoAccountLoggingConfig) GetString(string) string          { return "" }
func (demoAccountLoggingConfig) GetBool(key string) bool {
	return key == "server.demoaccount.enabled"
}
func (demoAccountLoggingConfig) GetInt(string) int                { return 0 }
func (demoAccountLoggingConfig) GetInt32(string) int32            { return 0 }
func (demoAccountLoggingConfig) GetInt64(string) int64            { return 0 }
func (demoAccountLoggingConfig) GetFloat64(string) float64        { return 0 }
func (demoAccountLoggingConfig) GetDuration(string) time.Duration { return 0 }
func (c demoAccountLoggingConfig) GetStringSlice(key string) []string {
	if key != "server.demoaccount.allowpathprefixes" {
		return nil
	}
	return append([]string(nil), c.allowPathPrefixes...)
}
func (demoAccountLoggingConfig) GetUintSlice(key string) []uint {
	if key == "server.demoaccount.userids" {
		return []uint{7}
	}
	return nil
}
func (demoAccountLoggingConfig) Set(string, interface{}) {}
func (demoAccountLoggingConfig) SaveConfig() error       { return nil }

type demoAccountLoggingSink struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *demoAccountLoggingSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *demoAccountLoggingSink) Sync() error { return nil }
func (s *demoAccountLoggingSink) Close() error {
	return nil
}

func (s *demoAccountLoggingSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestLoggingDemoAccountSanitizesDynamicSecretPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldConfig, oldRuntime, oldLog := app.ConfigYml, app.LogRuntime, app.ZapLog
	t.Cleanup(func() { app.ConfigYml, app.LogRuntime, app.ZapLog = oldConfig, oldRuntime, oldLog })

	sink := &demoAccountLoggingSink{}
	runtime, err := logging.NewRuntime(logging.Options{
		Config: logging.Config{
			Outputs:      []string{"stdout"},
			Level:        zapcore.DebugLevel,
			Modules:      map[string]zapcore.Level{"access": zapcore.InfoLevel},
			FileFormat:   "json",
			StdoutFormat: "json",
			MaxSizeMB:    1,
			MaxBackups:   1,
			MaxAgeDays:   1,
		},
		Service:  "uvp-gb28181-test",
		Version:  "test",
		Instance: "demoaccount-test",
		Sinks:    map[string]zapcore.WriteSyncer{"stdout": sink},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close()) })
	app.LogRuntime = runtime
	app.ZapLog = runtime.Root
	const allowSecret = "allow-secret-value"
	const denySecret = "deny-secret-value"
	app.ConfigYml = demoAccountLoggingConfig{allowPathPrefixes: []string{"/demo/write/" + allowSecret}}

	engine := gin.New()
	engine.Use(ginhelper.RequestLogging(runtime.Root))
	engine.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7, Username: "demo"}})
		c.Next()
	})
	engine.Use(DemoAccountMiddleware())
	engine.POST("/demo/write/:secret", func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.POST("/demo/deny/:secret", func(c *gin.Context) { t.Fatal("denied demo request must not reach handler") })

	allowResponse := httptest.NewRecorder()
	allowRequest := httptest.NewRequest(http.MethodPost, "/demo/write/"+allowSecret, nil)
	allowRequest.Header.Set("X-Request-ID", "demo-allow-request")
	engine.ServeHTTP(allowResponse, allowRequest)
	denyResponse := httptest.NewRecorder()
	denyRequest := httptest.NewRequest(http.MethodPost, "/demo/deny/"+denySecret, nil)
	denyRequest.Header.Set("X-Request-ID", "demo-deny-request")
	engine.ServeHTTP(denyResponse, denyRequest)

	require.Equal(t, http.StatusOK, allowResponse.Code)
	require.Equal(t, http.StatusForbidden, denyResponse.Code)
	output := sink.String()
	require.NotContains(t, output, allowSecret)
	require.NotContains(t, output, denySecret)

	rows := demoAccountLogRows(t, output)
	allowLog := demoAccountFindEvent(t, rows, "auth.demo_account.allow_path")
	require.Equal(t, "/demo/write/:secret", allowLog["route"])
	require.Equal(t, true, allowLog["matched_prefix"])
	require.NotEmpty(t, allowLog["request_id"])
	require.Equal(t, "demo-allow-request", allowLog["client_request_id"])
	_, hasPath := allowLog["path"]
	require.False(t, hasPath)
	_, hasMatchedPrefix := allowLog["matchedPrefix"]
	require.False(t, hasMatchedPrefix)

	denyLog := demoAccountFindEvent(t, rows, "auth.demo_account.denied")
	require.Equal(t, "/demo/deny/:secret", denyLog["route"])
	require.NotEmpty(t, denyLog["request_id"])
	require.Equal(t, "demo-deny-request", denyLog["client_request_id"])
	_, hasDenyPath := denyLog["path"]
	require.False(t, hasDenyPath)
}

func demoAccountLogRows(t *testing.T, output string) []map[string]interface{} {
	t.Helper()
	var rows []map[string]interface{}
	for _, line := range bytes.Split([]byte(output), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]interface{}
		require.NoError(t, json.Unmarshal(line, &row))
		rows = append(rows, row)
	}
	return rows
}

func demoAccountFindEvent(t *testing.T, rows []map[string]interface{}, event string) map[string]interface{} {
	t.Helper()
	for _, row := range rows {
		if row["event"] == event {
			return row
		}
	}
	t.Fatalf("event %q not found in %#v", event, rows)
	return nil
}
