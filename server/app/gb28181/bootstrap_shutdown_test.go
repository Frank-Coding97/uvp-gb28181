package gb28181

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emiago/sipgo"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type shutdownRootConfig struct{ app.YmlConfigInterf }

func (shutdownRootConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	if key == gbconfig.PlayAuthActiveKeyConfigKey {
		return strings.Repeat("fixture-key-", 4)
	}
	return ""
}
func (shutdownRootConfig) Get(string) interface{}         { return nil }
func (shutdownRootConfig) GetInt(string) int              { return 0 }
func (shutdownRootConfig) GetBool(key string) bool        { return key == "gb28181.enabled" }
func (shutdownRootConfig) GetStringSlice(string) []string { return nil }

func isolateSIPShutdownRoot(t *testing.T) {
	t.Helper()
	previousServer, previousSecurity, previousStatus := sipServer, securityRuntime, sipRuntimeStatus
	previousLog := app.ZapLog
	previousConfig := app.ConfigYml
	previousMySQL, previousPG, previousMSSQL := app.GormDbMysql, app.GormDbPostgreSql, app.GormDbSqlserver
	sipServer, securityRuntime, sipRuntimeStatus = nil, nil, gbsetup.NewRuntimeStatus()
	app.GormDbMysql, app.GormDbPostgreSql, app.GormDbSqlserver = nil, nil, nil
	app.ZapLog = zap.NewNop()
	app.ConfigYml = shutdownRootConfig{}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "root.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	app.GormDbMysql = db // Only this isolated empty fixture; never default configuration.
	t.Cleanup(func() {
		sipServer, securityRuntime, sipRuntimeStatus = previousServer, previousSecurity, previousStatus
		app.GormDbMysql, app.GormDbPostgreSql, app.GormDbSqlserver = previousMySQL, previousPG, previousMSSQL
		app.ZapLog = previousLog
		app.ConfigYml = previousConfig
		_ = raw.Close()
	})
}

func TestStopSIPDependenciesRetainsFailedServerAndSecurity(t *testing.T) {
	isolateSIPShutdownRoot(t)
	want := errors.New("fixture SIP drain unavailable")
	server := &fakeSIPRuntimeServer{shutdownErr: want}
	runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, nil)
	sipServer, securityRuntime = server, runtime
	require.ErrorIs(t, stopSIPDependencies(context.Background()), want)
	require.Same(t, server, sipServer)
	require.Same(t, runtime, securityRuntime)
	called := 0
	err := startSIPDependenciesWithFactory(gbconfig.Config{}, func(gbconfig.Config) (sipRuntimeServer, error) {
		called++
		return &fakeSIPRuntimeServer{}, nil
	})
	require.Error(t, err)
	require.Zero(t, called, "failed stop must not permit a replacement instance")
	server.shutdownErr = nil
	require.NoError(t, stopSIPDependencies(context.Background()))
	require.Nil(t, sipServer)
	require.Nil(t, securityRuntime)
}

func TestStartSIPDependenciesRetainsFailedRollback(t *testing.T) {
	for _, stage := range []string{"factory", "start", "assembly"} {
		t.Run(stage, func(t *testing.T) {
			isolateSIPShutdownRoot(t)
			failure, shutdownFailure := errors.New("fixture start failed"), errors.New("fixture rollback failed")
			server := &fakeSIPRuntimeServer{shutdownErr: shutdownFailure}
			if stage == "start" {
				server.startErr = failure
			}
			if stage == "assembly" {
				ua, err := sipgo.NewUA()
				require.NoError(t, err)
				defer ua.Close()
				server.uac, err = uac.New(ua, "34020000002000000001", "3402000000", "127.0.0.1", 5061, false)
				require.NoError(t, err)
			}
			err := startSIPDependenciesWithFactory(gbconfig.Config{}, func(gbconfig.Config) (sipRuntimeServer, error) {
				if stage == "factory" {
					return server, failure
				}
				return server, nil
			})
			require.ErrorIs(t, err, shutdownFailure)
			if stage != "assembly" {
				require.ErrorIs(t, err, failure)
			}
			require.Same(t, server, sipServer)
			require.NotNil(t, securityRuntime)
			require.Equal(t, gbsetup.RuntimeFailed, sipRuntimeStatus.Snapshot().State)
			server.shutdownErr = nil
			require.NoError(t, stopSIPDependencies(context.Background()))
			require.Nil(t, sipServer)
			require.Nil(t, securityRuntime)
		})
	}
}

func TestStopFailurePreservesControlPlaneDependencies(t *testing.T) {
	isolateSIPShutdownRoot(t)
	want := errors.New("fixture persistent drain failed")
	sipServer = &fakeSIPRuntimeServer{shutdownErr: want}
	previousHeartbeat := heartbeatCancel
	defer func() { heartbeatCancel = previousHeartbeat }()
	called := false
	heartbeatCancel = func() { called = true }
	require.ErrorIs(t, Stop(), want)
	require.False(t, called, "control plane/DB dependencies must not be torn down after SIP failure")
}

func TestReloadSIPDoesNotReplaceServerAfterStopFailure(t *testing.T) {
	isolateSIPShutdownRoot(t)
	db := app.DB()
	require.NoError(t, db.AutoMigrate(&gbsetup.SIPConfig{}))
	require.NoError(t, db.Create(&gbsetup.SIPConfig{ID: 1, DeploymentMode: gbsetup.DeploymentLAN,
		ListenIP: "127.0.0.1", AdvertiseIP: "127.0.0.1", Port: 5061, Domain: "3402000000", ServerID: "34020000002000000001"}).Error)
	want := errors.New("fixture reload drain failed")
	server := &fakeSIPRuntimeServer{shutdownErr: want}
	runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, nil)
	sipServer, securityRuntime = server, runtime
	require.ErrorIs(t, ReloadSIP(), want)
	require.Same(t, server, sipServer)
	require.Same(t, runtime, securityRuntime)
	require.Equal(t, gbsetup.RuntimeFailed, sipRuntimeStatus.Snapshot().State)
}
