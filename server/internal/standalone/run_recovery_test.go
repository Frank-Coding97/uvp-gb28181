package standalone

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInspectRunMarkerPreservesUncleanEvidence(t *testing.T) {
	paths := runMarkerTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer lock.Close()
	state, err := InspectRunMarker(paths)
	require.NoError(t, err)
	require.False(t, state.PreviousUnclean)
	marker, err := BeginRun(paths)
	require.NoError(t, err)
	before, err := os.ReadFile(testRunMarkerPath(paths))
	require.NoError(t, err)
	state, err = InspectRunMarker(paths)
	require.ErrorIs(t, err, ErrUncleanRecoveryRequired)
	require.True(t, state.PreviousUnclean)
	digest := sha256.Sum256(before)
	require.Equal(t, hex.EncodeToString(digest[:]), state.SHA256)
	after, err := os.ReadFile(testRunMarkerPath(paths))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoError(t, marker.Finish())
	_, err = InspectRunMarker(paths)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(testRunMarkerPath(paths), []byte("{"), false, nil))
	_, err = InspectRunMarker(paths)
	require.ErrorIs(t, err, ErrUncleanRecoveryRequired)
	after, err = os.ReadFile(testRunMarkerPath(paths))
	require.NoError(t, err)
	require.Equal(t, []byte("{"), after)
	require.NoError(t, lock.Close())
	_, err = InspectRunMarker(paths)
	require.Error(t, err)
}
