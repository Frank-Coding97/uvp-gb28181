package gb28181

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbsip "uvplatform.cn/uvp-gb28181/app/gb28181/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// Actual SIP dependency assembly runs in a fresh process so legacy global
// background producers cannot contaminate other bootstrap tests. No application
// bootstrap init, default DB, real device, or ZLM endpoint is loaded.
func TestSIPRootStartsAndRetainsPlaybackRecovery(t *testing.T) {
	if os.Getenv("UVP_ROOT_RECOVERY_CHILD") != "1" {
		for _, mode := range []string{"normal", "conflicting-barrier"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSIPRootStartsAndRetainsPlaybackRecovery$")
				cmd.Env = append(os.Environ(), "UVP_ROOT_RECOVERY_CHILD=1", "UVP_ROOT_RECOVERY_MODE="+mode, "UVP_ROOT_RECOVERY_DB="+filepath.Join(t.TempDir(), "root.sqlite"))
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", out)
			})
		}
		return
	}
	t.Setenv("UVP_GB28181_CASCADE_KEY", "")
	app.ZapLog = zap.NewNop()
	app.ConfigYml = shutdownRootConfig{}
	db, err := gorm.Open(sqlite.Open(os.Getenv("UVP_ROOT_RECOVERY_DB")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	app.GormDbMysql = db
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceFirmwareUpgrade{}))
	require.NoError(t, db.Exec("CREATE TABLE gb_device (id BIGINT PRIMARY KEY, device_id TEXT, access_epoch BIGINT, cleanup_completed_epoch BIGINT, deleted_at DATETIME)").Error)
	entered, release := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() { unblock(); _ = stopSIPDependencies(context.Background()) }()
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("fixture:root-recovery-discovery", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device" {
			enteredOnce.Do(func() { close(entered) })
			<-release
		}
	}))
	cfg, err := gbconfig.LoadValidatedFrom(app.ConfigYml)
	require.NoError(t, err)
	require.False(t, gbconfig.CurrentPlayAuthSettings().Enabled, "compensation must not depend on play-auth being enabled")
	require.Nil(t, zlmRegistry, "compensation registration must not depend on media readiness")
	cfg.SIP = gbconfig.SIPConfig{ListenIP: "127.0.0.1", AdvertiseIP: "127.0.0.1", Domain: "3402000000", ServerID: "34020000002000000001"}
	// No SIP peer is needed to discover an empty DB. Actual listener drain is
	// covered by Server's dual-transport test, not inferred from this fixture.
	var server *gbsip.Server
	conflict := os.Getenv("UVP_ROOT_RECOVERY_MODE") == "conflicting-barrier"
	err = startSIPDependenciesWithFactory(cfg, func(cfg gbconfig.Config) (sipRuntimeServer, error) {
		var err error
		server, err = gbsip.NewServer(cfg)
		if err == nil && conflict {
			_, err = server.UAC().StartPlaybackRecovery(context.Background(), playauth.NewDeviceCleanupStore(db), playauth.NewDeviceOperationIntentStore(db), playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)), nil)
			require.NoError(t, err)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("fixture recovery never started")
			}
		}
		return server, err
	})
	if conflict {
		require.ErrorIs(t, err, uac.ErrPlaybackUnavailable, "root must reject a different pre-bound barrier")
		require.ErrorIs(t, err, context.DeadlineExceeded, "failed rollback must retain the still-running worker")
		require.Same(t, server, sipServer)
		require.NotNil(t, securityRuntime)
		unblock()
		require.NoError(t, stopSIPDependencies(context.Background()))
		require.Nil(t, sipServer)
		return
	}
	require.NoError(t, err)
	require.Same(t, server, sipServer)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual SIP root did not start persistent recovery discovery")
	}
	_, err = server.UAC().StartPlaybackRecovery(context.Background(), playauth.NewDeviceCleanupStore(db), playauth.NewDeviceOperationIntentStore(db), playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db)), nil)
	require.Error(t, err, "root already owns the only recovery worker/barrier")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = stopSIPDependencies(ctx)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "unexpected stop: %v", err)
	require.Same(t, server, sipServer, "timed-out worker must remain owned by the same root")
	require.NotNil(t, securityRuntime)
	unblock()
	require.NoError(t, stopSIPDependencies(context.Background()))
	require.Nil(t, sipServer)
	require.Nil(t, securityRuntime)
}

func TestSIPRootRejectsMissingRecoveryUAC(t *testing.T) {
	isolateSIPShutdownRoot(t)
	err := startSIPDependenciesWithFactory(gbconfig.Config{}, func(gbconfig.Config) (sipRuntimeServer, error) {
		return &fakeSIPRuntimeServer{}, nil
	})
	require.ErrorIs(t, err, uac.ErrPlaybackUnavailable)
	require.Nil(t, sipServer)
	require.Nil(t, securityRuntime)
}

// Source ordering complements actual assembly and assignment's executable
// nil-barrier rejection tests; it is not evidence of complete HTTP transfer.
func TestSIPRootRecoverySharesBarrierBeforeFacadePublication(t *testing.T) {
	data, err := os.ReadFile("bootstrap.go")
	require.NoError(t, err)
	source := string(data)
	begin, end := strings.Index(source, "func startSIPDependenciesWithFactory("), strings.Index(source, "func buildPlaySigner(")
	require.Greater(t, end, begin)
	start := source[begin:end]
	register, publish := strings.Index(start, "srv.UAC().StartPlaybackRecovery("), strings.Index(start, "gbroutes.SetDeviceTransferBarrier(deviceOperations)")
	require.GreaterOrEqual(t, register, 0)
	require.Greater(t, publish, register)
	require.Equal(t, 1, strings.Count(start, "playauth.NewDeviceOperationBarrier("))
	require.Contains(t, start, "deviceDB := app.DB()")
	require.Contains(t, start, "playauth.NewDeviceSecurityStore(deviceDB)")
	require.Contains(t, start[register:publish], "playauth.NewDeviceCleanupStore(deviceDB), playauth.NewDeviceOperationIntentStore(deviceDB), deviceOperations")
	stopBegin, stopEnd := strings.Index(source, "func stopSIPDependencies("), strings.Index(source, "func stopPlaybackRuntime(")
	require.Greater(t, stopEnd, stopBegin)
	stop := source[stopBegin:stopEnd]
	detach := strings.Index(stop, "gbroutes.SetDeviceTransferBarrier(nil)")
	require.GreaterOrEqual(t, detach, 0)
	require.Less(t, detach, strings.Index(stop, "stopPlaybackRuntime(ctx)"))
	require.Less(t, detach, strings.Index(stop, "sipServer.Shutdown(ctx)"))
}
