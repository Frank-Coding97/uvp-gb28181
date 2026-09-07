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
	serve := strings.Index(source, "_ = ginhelper.StartServer(engine)")
	stop := strings.LastIndex(source, "stopRevocation()")
	gbStop := strings.Index(source, "gb28181.Stop()")
	require.Greater(t, setup, start)
	require.Greater(t, serve, setup)
	require.Greater(t, stop, serve)
	require.Greater(t, gbStop, stop)
	require.Contains(t, source[setup:serve], "defer stopRevocation()")
	require.Contains(t, source[serve:stop], "cancelMaintenance()")
	require.Contains(t, source[setup:serve], `GetString("openapi.revocation_bindings_file")`)
}
