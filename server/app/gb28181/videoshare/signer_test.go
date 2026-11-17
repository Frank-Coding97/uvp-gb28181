package videoshare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignerBindsEveryClaim(t *testing.T) {
	signer, err := NewSigner(make([]byte, keyBytes))
	require.NoError(t, err)
	claims := Claims{PublicID: "public-a", Generation: "generation-a", ExpiresAt: 1800000000, KeyID: KeyID}
	signature, err := signer.Sign(claims)
	require.NoError(t, err)
	require.True(t, signer.Verify(claims, signature))

	for _, changed := range []Claims{
		{PublicID: "public-b", Generation: claims.Generation, ExpiresAt: claims.ExpiresAt, KeyID: claims.KeyID},
		{PublicID: claims.PublicID, Generation: "generation-b", ExpiresAt: claims.ExpiresAt, KeyID: claims.KeyID},
		{PublicID: claims.PublicID, Generation: claims.Generation, ExpiresAt: claims.ExpiresAt + 1, KeyID: claims.KeyID},
		{PublicID: claims.PublicID, Generation: claims.Generation, ExpiresAt: claims.ExpiresAt, KeyID: "old"},
	} {
		require.False(t, signer.Verify(changed, signature))
	}
	require.False(t, signer.Verify(claims, signature+"x"))
}

func TestLoadOrCreateSignerPersistsKeyAndRejectsCorruption(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "secrets")
	first, err := LoadOrCreateSigner(directory)
	require.NoError(t, err)
	claims := Claims{PublicID: "public-a", Generation: "generation-a", ExpiresAt: 1800000000, KeyID: KeyID}
	signature, err := first.Sign(claims)
	require.NoError(t, err)

	second, err := LoadOrCreateSigner(directory)
	require.NoError(t, err)
	require.True(t, second.Verify(claims, signature), "restarting must preserve existing links")

	path := filepath.Join(directory, keyFile)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.NoError(t, os.WriteFile(path, []byte("damaged"), 0600))
	_, err = LoadOrCreateSigner(directory)
	require.Error(t, err, "corrupt key must disable sharing rather than rotate silently")
}

func TestLoadOrCreateSignerRejectsDirectoryAsKeyPath(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(directory, keyFile), 0700))
	_, err := LoadOrCreateSigner(directory)
	require.Error(t, err)
}
