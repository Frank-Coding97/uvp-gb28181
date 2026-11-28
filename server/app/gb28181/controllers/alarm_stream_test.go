package controllers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/alarmpopup"
)

// 告警实时流复用 GET /alarms（?stream=1）：casbin 只认 URL.Path，
// 复用已验证的「查询告警列表」策略，避免新路由漏登记导致线上 403。
// 本用例锁住「同一个 handler 按参数分流」这个结构。
func TestAlarmStreamSharesListRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	controller := NewAlarmController()
	controller.SetDB(func() *gorm.DB { return db })

	router := gin.New()
	router.GET("/alarms", controller.List)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/alarms?stream=1", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/event-stream", response.Header.Get("Content-Type"))
	require.Equal(t, "no", response.Header.Get("X-Accel-Buffering"))

	lines := readStreamLines(response)
	// 首帧必须是 ready：前端据此知道连接成立（且不会把"没消息"误判成断线）
	require.True(t, waitForLine(t, lines, "event: ready"), "应立刻收到 ready 帧")
}

func TestAlarmStreamDeliversPublishedAlarm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	controller := NewAlarmController()
	controller.SetDB(func() *gorm.DB { return db })

	router := gin.New()
	router.GET("/alarms", controller.List)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/alarms?stream=1", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()

	lines := readStreamLines(response)
	// 读到 ready 就等于确认 handler 已经订阅（订阅发生在发送 ready 之前）
	require.True(t, waitForLine(t, lines, "event: ready"))

	priority := 1
	alarmpopup.Publish(alarmpopup.Event{
		AlarmID:    12,
		DeviceDBID: 3,
		DeviceCode: "34020000001320000001",
		DeviceName: "东门摄像机",
		Priority:   &priority,
		ReceivedAt: time.Now(),
	})

	require.True(t, waitForLine(t, lines, "event: alarm"), "发布后应推送到客户端")
	require.True(t, waitForLine(t, lines, `"deviceName":"东门摄像机"`), "推送体积里应带设备名，前端不用再回查")
}

// 可见性判定的三条边界：取不到设备主键必须丢弃（fail-closed）、
// 归属部门命中直接放行（零查询）、全量权限不过滤。
func TestAlarmEventVisibleBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	controller := NewAlarmController()
	allowed := map[uint]bool{7: true}

	require.False(t, controller.alarmEventVisible(c, nil, alarmpopup.Event{DeviceDBID: 0}, true, allowed),
		"需要按部门过滤但拿不到设备主键时，必须不推")
	require.True(t, controller.alarmEventVisible(c, nil, alarmpopup.Event{DeviceDBID: 5, OwnerDeptID: 7}, true, allowed),
		"归属部门可见时不该再去查库")
	require.True(t, controller.alarmEventVisible(c, nil, alarmpopup.Event{DeviceDBID: 0}, false, nil),
		"全量权限（datascope 表示不过滤）时一律放行")
}

// readStreamLines 把响应体按行喂进 channel；连接关闭时关闭 channel。
func readStreamLines(response *http.Response) <-chan string {
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return lines
}

// waitForLine 在超时内等待包含 fragment 的一行。
func waitForLine(t *testing.T, lines <-chan string, fragment string) bool {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return false
			}
			if strings.Contains(line, fragment) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
