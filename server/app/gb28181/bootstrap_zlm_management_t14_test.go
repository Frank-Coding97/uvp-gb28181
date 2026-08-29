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
	return &zlmManagementCoreRuntime{
		registry: reg,
		executor: executor,
		runtime:  gbzlmmanagement.NewRuntimeReader(executor),
		ledger:   gbzlmrepo.NewManagedResourceRepo(db),
		restart:  gbzlmsvc.NewRestartCoordinator(reg),
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

	_, err := core.restart.Begin(1)
	require.ErrorIs(t, err, gbzlmsvc.ErrRestartCoordinatorClosed)
}
