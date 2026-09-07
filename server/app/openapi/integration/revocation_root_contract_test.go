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
	start := strings.Index(source, "gb28181.Start()")
	setup := strings.Index(source, "openapimedia.StartConfiguredRevocation(")
	serve := strings.Index(source, "serveErr := ginhelper.StartServer(engine)")
	stop := strings.LastIndex(source, "stopRevocation()")
	gbStop := strings.Index(source, "waitForSIPShutdown(gb28181.Stop,")
	require.Greater(t, setup, start)
	require.Greater(t, serve, setup)
	require.Greater(t, stop, serve)
	require.Greater(t, gbStop, stop)
	require.Greater(t, strings.Index(source, "os.Exit(1)"), gbStop)
	require.NotContains(t, source, "gb28181.Stop()", "main must not discard the shutdown error")
	require.Contains(t, source[setup:serve], "defer stopRevocation()")
	require.Contains(t, source[serve:stop], "cancelMaintenance()")
	require.Contains(t, source[setup:serve], `GetString("openapi.revocation_bindings_file")`)
	helper, err := os.ReadFile(filepath.Join("..", "..", "utils", "ginhelper", "ginhelper.go"))
	require.NoError(t, err)
	httpRoot := string(helper[strings.Index(string(helper), "func StartServer("):])
	require.NotContains(t, httpRoot, ".Fatal(")
	require.Contains(t, httpRoot, "defer signal.Stop(quit)")
	require.Less(t, strings.Index(httpRoot, "serveHTTPUntilShutdown("), strings.Index(httpRoot, "scheduler.StopResultHandler()"))
}
