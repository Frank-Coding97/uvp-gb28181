//go:build !windows

package authoritytest

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func privateDirectory(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Chmod(path, 0700))
}
