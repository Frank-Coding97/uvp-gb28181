package gb28181

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	gbzlmmanagement "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	gbzlmrepo "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
	gbzlmsvc "uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type t14ExistingRecordingService struct{}

func (t14ExistingRecordingService) Enable(context.Context, uint) (*gbmodels.GbChannel, error) {
	return &gbmodels.GbChannel{}, nil
}
func (t14ExistingRecordingService) BeginPlayback(context.Context, string) error { return nil }
func (t14ExistingRecordingService) Disable(context.Context, uint) (*gbmodels.GbChannel, error) {
	return &gbmodels.GbChannel{}, nil
}

type t14StreamLocation struct{ nodeID int64 }

func (l t14StreamLocation) Lookup(string) (int64, bool) { return l.nodeID, l.nodeID > 0 }

func newT14ManagementCore(t *testing.T) (*zlmManagementCoreRuntime, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbzlmrepo.MetaNode{}, &gbmodels.GbZLMManagedResource{}))
	reg := node.NewRegistry(gbzlmrepo.NewMetaNodeRepo(db))
	current, err := reg.Add(context.Background(), node.Node{
		Name: "n1", Host: "127.0.0.1", APIPort: 18080, APISecret: "secret",
		MediaServerUUID: "uuid-1", State: node.StateActive,
	})
	require.NoError(t, err)
	require.NotZero(t, current.ID)
	executor := gbzlmmanagement.NewNodeExecutor(reg, func(*node.Node) *zlm.Client { return nil })
	runtime := gbzlmmanagement.NewRuntimeReader(executor)
	// `overview` 是**核级**依赖（采样循环要跨 SIP 重载存活，不能在
	// newZLMManagementBundle 里逐次重建 -- 见 886ba24f）。生产路径
	// setupZLMManagementCore 一定会建它，所以「非 nil 的 core 必有非 nil 的
	// overview」是这个类型的不变量；夹具原先漏了这一项，等于造出一个生产里
	// 不存在的 core，于是 bundle.Overview 装进了「接口非 nil、内部指针 nil」的
	// *OverviewSampler —— 控制器那句 `bundle.Overview == nil` 拦不住，
	// 请求直接进 GetOverview 解引用 panic。
	//
	// cache 显式传 nil（sampler 对 nil cache 是容错的）：故意不用 app.Cache，
	// 它一旦被本包别的用例填上，这个夹具就会去连真 Redis。
	overview := gbzlmmanagement.NewOverviewSampler(
		gbzlmmanagement.NewOverviewService(gbzlmmanagement.OverviewDependencies{
			Registry: reg,
			Runtime:  runtime,
			Media:    runtime,
		}), nil)
	return &zlmManagementCoreRuntime{
		registry: reg,
		executor: executor,
		runtime:  runtime,
		ledger:   gbzlmrepo.NewManagedResourceRepo(db),
		restart:  gbzlmsvc.NewRestartCoordinator(reg),
		overview: overview,
	}, db
}

func TestZLMManagementT14_BuildsTypedCoreAndLeavesUnsafeOptionalCapabilitiesUnavailable(t *testing.T) {
	core, _ := newT14ManagementCore(t)
	defer core.restart.Close()

	bundle := newZLMManagementBundle(core, zlmManagementBusinessRuntime{})
	require.NotNil(t, bundle)
	require.NotNil(t, bundle.Overview)
	require.NotNil(t, bundle.Streams)
	require.NotNil(t, bundle.Sessions)
	require.NotNil(t, bundle.Proxies)
	require.NotNil(t, bundle.FFmpeg)
	require.NotNil(t, bundle.RTP)
	require.Nil(t, bundle.Snapshot, "bootstrap must not invent a snapshot URL builder")
	require.Nil(t, bundle.Recording, "GB recording controls stay unavailable until SIP business dependencies are proven")
	require.Nil(t, newZLMManagementBundle(nil, zlmManagementBusinessRuntime{}))
}

func TestZLMManagementT14_RebuildAddsRecordingOnlyWithAuthoritativeBusinessDependencies(t *testing.T) {
	core, db := newT14ManagementCore(t)
	defer core.restart.Close()
	recordingRepo := gbrecording.NewGormRepo(db)

	bundle := newZLMManagementBundle(core, zlmManagementBusinessRuntime{
		recordingSessions:      recordingRepo,
		recordingChannels:      recordingRepo,
		recordingSessionLookup: recordingRepo,
		recordingService:       t14ExistingRecordingService{},
		recordingLocation:      t14StreamLocation{nodeID: 1},
	})
	require.NotNil(t, bundle)
	require.NotNil(t, bundle.Recording)
}

func TestZLMManagementT14_ControllerInstallAndTeardownAreRepeatable(t *testing.T) {
	core, _ := newT14ManagementCore(t)
	// 协调器由**本用例**持有（夹具建的），所以由本用例关 —— 这与生产一致：
	// setupZLMManagementCore 拿到的 restart 是 startControlPlane 建的，
	// 生命周期归 bootstrap_shutdown.go（它用 StopContext 停），不归这里。
	// 前两个用例同样自带这行 defer。
	defer core.restart.Close()
	previousCore := zlmManagementCore
	previousResponse := app.Response
	zlmManagementCore = core
	app.Response = response.NewResponseHandler()
	t.Cleanup(func() {
		clearZLMManagementController()
		zlmManagementCore = previousCore
		app.Response = previousResponse
	})

	installZLMManagementController()
	installZLMManagementController()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	gbroutes.RegisterRoutes(engine.Group("/api"))

	ready := httptest.NewRecorder()
	engine.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/api/gb28181/zlm/overview", nil))
	require.NotEqual(t, http.StatusServiceUnavailable, ready.Code)

	teardownZLMManagementCore()
	teardownZLMManagementCore()
	unavailable := httptest.NewRecorder()
	engine.ServeHTTP(unavailable, httptest.NewRequest(http.MethodGet, "/api/gb28181/zlm/overview", nil))
	require.Equal(t, http.StatusServiceUnavailable, unavailable.Code)

	// 这一句**原先是** `require.ErrorIs(t, err, gbzlmsvc.ErrRestartCoordinatorClosed)`
	// —— 断言 teardown 会关掉协调器。9985cba8（"track bootstrap shutdown
	// generations"）把协调器的停机关进了 bootstrap_shutdown.go，同时从
	// setupZLMManagementCore 和 teardownZLMManagementCore 里删掉了那两处
	// restart.Close()，这句断言就一直是错的（当时被上面的 panic 掩盖着）。
	//
	// 现在反过来钉住**归属边界**：teardown 之后协调器**仍然可用**。
	// 因为 nodeService / heartbeat collector / heartbeat watcher 三家都在共用
	// 同一个协调器，teardown 顺手把它关掉会连带弄死那三家；而这个夹具里的
	// core 只有一个，别处看不出来这条边界。
	_, err := core.restart.Begin(1)
	require.NoError(t, err)
}
