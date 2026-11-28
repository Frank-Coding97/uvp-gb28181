//go:build !windows

package processauthority

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func privateAuthorityTestPermissions(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Chmod(path, 0700))
}
