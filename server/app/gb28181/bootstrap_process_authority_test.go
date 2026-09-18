package gb28181

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestSIPRootRejectsAuthorityBeforeStartingProducer(t *testing.T) {
	for _, mode := range []string{"nil", "sealed", "wrong-database"} {
		t.Run(mode, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			isolateSIPShutdownRoot(t)
			authority := authoritytest.Register(t, app.DB(), "")
			switch mode {
			case "nil":
				authority = nil
			case "sealed":
				authority.Seal()
			case "wrong-database":
				app.GormDbMysql = authoritytest.OpenSQLite(t, filepath.Join(t.TempDir(), "other.sqlite"))
			}
			calls := 0
			err := startSIPDependenciesWithFactory(gbconfig.Config{}, authority, func(gbconfig.Config) (sipRuntimeServer, error) {
				calls++
				return &fakeSIPRuntimeServer{}, nil
			})
			require.ErrorIs(t, err, uac.ErrPlaybackUnavailable)
			require.Zero(t, calls, "invalid authority must not construct or start a listener")
			require.Nil(t, sipServer)
			require.Nil(t, securityRuntime)
		})
	}
}
