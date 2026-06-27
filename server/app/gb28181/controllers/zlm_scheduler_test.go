package controllers_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// fakeLogRepo 给 scheduler.LogService 用的最小内存 repo
type fakeLogRepo struct {
	rows []scheduler.SchedulerLog
}

func (r *fakeLogRepo) Insert(_ context.Context, l scheduler.SchedulerLog) error {
	r.rows = append(r.rows, l)
	return nil
}
func (r *fakeLogRepo) List(_ context.Context, limit int) ([]scheduler.SchedulerLog, error) {
	if limit <= 0 || limit >= len(r.rows) {
		out := make([]scheduler.SchedulerLog, len(r.rows))
		copy(out, r.rows)
		return out, nil
	}
	out := make([]scheduler.SchedulerLog, limit)
	copy(out, r.rows[len(r.rows)-limit:])
	return out, nil
}
func (r *fakeLogRepo) PruneOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// fakeSettingWriter Controller.SwitchScheduler 用
type fakeSettingWriter struct {
	algo  string
	calls int
}

func (w *fakeSettingWriter) UpdateAlgorithm(_ context.Context, name string) error {
	w.algo = name
	w.calls++
	return nil
}

func setupSchedulerRouter(t *testing.T) (*gin.Engine, *scheduler.Manager, *fakeSettingWriter, *fakeLogRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 给 Manager 一个真实 Registry / Factory
	reg := node.NewRegistry(newMemoryRepo())
	_, err := reg.Add(context.Background(), node.Node{
		Name: "n1", MediaServerUUID: "u1", State: node.StateActive,
	})
	require.NoError(t, err)
	factory := scheduler.NewFactory(reg)
	mgr := scheduler.NewManager(factory)
	require.NoError(t, mgr.Switch("roundrobin"))

	// 日志服务
	logRepo := &fakeLogRepo{}
	logSvc := scheduler.NewLogService(logRepo, 100)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	logSvc.Start(ctx)
	t.Cleanup(logSvc.Stop)

	setting := &fakeSettingWriter{}
	ctrl := gbcontrollers.NewZLMSchedulerController(mgr, logSvc, setting)

	r := gin.New()
	r.Use(abortRecover())
	g := r.Group("/api/gb28181/zlm")
	{
		g.GET("/scheduler", ctrl.GetScheduler)
		g.PUT("/scheduler", ctrl.SwitchScheduler)
		g.GET("/scheduler/logs", ctrl.ListSchedulerLogs)
	}
	return r, mgr, setting, logRepo
}

func TestSchedulerController_Get(t *testing.T) {
	r, _, _, _ := setupSchedulerRouter(t)
	w, resp := do(t, r, "GET", "/api/gb28181/zlm/scheduler", nil)
	require.Equal(t, 200, w.Code)
	data := resp["data"].(map[string]any)
	require.Equal(t, "roundrobin", data["algorithm"])
	avail := data["available"].([]any)
	require.Len(t, avail, 3)
}

func TestSchedulerController_Switch(t *testing.T) {
	r, mgr, setting, _ := setupSchedulerRouter(t)
	w, _ := do(t, r, "PUT", "/api/gb28181/zlm/scheduler", map[string]any{"algorithm": "weighted"})
	require.Equal(t, 200, w.Code)
	require.Equal(t, "weighted", mgr.CurrentName())
	require.Equal(t, "weighted", setting.algo)
	require.Equal(t, 1, setting.calls)
}

func TestSchedulerController_Switch_Unsupported(t *testing.T) {
	r, mgr, _, _ := setupSchedulerRouter(t)
	w, resp := do(t, r, "PUT", "/api/gb28181/zlm/scheduler", map[string]any{"algorithm": "nonexistent"})
	require.NotEqual(t, http.StatusInternalServerError, w.Code)
	// FailAndAbort 走 mockResponse.Fail → code=1
	require.Equal(t, float64(1), resp["code"])
	// 算法未变
	require.Equal(t, "roundrobin", mgr.CurrentName())
}

func TestSchedulerController_ListLogs(t *testing.T) {
	r, _, _, logRepo := setupSchedulerRouter(t)
	// 预置 3 条日志
	logRepo.rows = []scheduler.SchedulerLog{
		{ID: 1, HappenedAt: time.Now(), Algorithm: "roundrobin", NodeID: 1, NodeName: "n1"},
		{ID: 2, HappenedAt: time.Now(), Algorithm: "roundrobin", NodeID: 1, NodeName: "n1"},
		{ID: 3, HappenedAt: time.Now(), Algorithm: "weighted", NodeID: 2, NodeName: "n2"},
	}
	w, resp := do(t, r, "GET", "/api/gb28181/zlm/scheduler/logs?limit=50", nil)
	require.Equal(t, 200, w.Code)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	require.Len(t, list, 3)
	require.Equal(t, float64(50), data["limit"])
}

func TestSchedulerController_ListLogs_DefaultLimit(t *testing.T) {
	r, _, _, _ := setupSchedulerRouter(t)
	_, resp := do(t, r, "GET", "/api/gb28181/zlm/scheduler/logs", nil)
	data := resp["data"].(map[string]any)
	require.Equal(t, float64(100), data["limit"])
}
