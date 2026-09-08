package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Source ordering complements the executable runner/startup tests. It does not
// claim to boot the complete application or exercise production dependencies.
func TestOpenAPIRevocationMainLifecycleWiring(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "main.go"))
	require.NoError(t, err)
	source := string(data)
	start := strings.Index(source, "gb28181.Start(authority)")
	prime := strings.Index(source, "gb28181.LoadStartupOpenAPIControlBindingsOnce()")
	setup := strings.Index(source, "openapimedia.StartRevocationWithBindings(")
	serve := strings.Index(source, "serveErr := ginhelper.StartServer(engine)")
	stop := strings.LastIndex(source, "stopRevocation()")
	gbStop := strings.Index(source, "waitForSIPShutdown(gb28181.Stop,")
	require.GreaterOrEqual(t, prime, 0)
	require.Greater(t, start, prime)
	require.Greater(t, setup, start)
	require.Greater(t, serve, setup)
	require.Greater(t, stop, serve)
	require.Greater(t, gbStop, stop)
	require.Greater(t, strings.Index(source, "os.Exit(1)"), gbStop)
	require.NotContains(t, source, "gb28181.Stop()", "main must not discard the shutdown error")
	require.Contains(t, source[setup:serve], "defer stopRevocation()")
	require.Contains(t, source[serve:stop], "cancelMaintenance()")
	require.Contains(t, source[setup:serve], "controlBindings")
	require.NotContains(t, source, "openapimedia.StartConfiguredRevocation(")
	require.NotContains(t, source, `GetString("openapi.revocation_bindings_file")`)
	acquire := strings.Index(source, "processauthority.AcquireLocalLock(")
	register := strings.Index(source, "processauthority.Register(")
	seal := strings.Index(source, "authority.Seal()")
	require.GreaterOrEqual(t, acquire, 0)
	require.Greater(t, acquire, strings.Index(source, "if downFile := parseArgs"))
	require.Greater(t, register, acquire)
	schedulerStart := strings.Index(source, "app.JobScheduler.Start()")
	require.Greater(t, schedulerStart, register)
	require.Less(t, schedulerStart, strings.Index(source, "routes.InitRoutes(engine)"))
	require.Greater(t, strings.Index(source, "routes.InitRoutes(engine)"), register)
	require.Greater(t, seal, gbStop)
	require.Contains(t, source[seal:], "authorityLock.Close()")
	require.Less(t, seal, strings.Index(source, "os.Exit(1)"))
	require.Contains(t, source, `GetString("processauthority.state_dir")`)
	bootstrap, err := os.ReadFile(filepath.Join("..", "..", "..", "bootstrap", "init.go"))
	require.NoError(t, err)
	require.NotContains(t, string(bootstrap), "scheduler.Start()", "init may build/load jobs but must not execute them before registration")
	helper, err := os.ReadFile(filepath.Join("..", "..", "utils", "ginhelper", "ginhelper.go"))
	require.NoError(t, err)
	httpRoot := string(helper[strings.Index(string(helper), "func StartServer("):])
	require.NotContains(t, httpRoot, ".Fatal(")
	require.Contains(t, httpRoot, "defer signal.Stop(quit)")
	require.Less(t, strings.Index(httpRoot, "serveHTTPUntilShutdown("), strings.Index(httpRoot, "scheduler.StopResultHandler()"))
}
