package securestore

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestKeyFilePersistsAndDoesNotReplaceInvalidKey(t *testing.T) {
	t.Setenv("TEST_CASCADE_FILE_KEY", "")
	p := filepath.Join(t.TempDir(), "secrets", "cascade.key")
	c, e := LoadOrCreateCipher("TEST_CASCADE_FILE_KEY", p, "v1")
	require.NoError(t, e)
	envelope, e := c.Encrypt("test", []byte("password"))
	require.NoError(t, e)
	c, e = LoadOrCreateCipher("TEST_CASCADE_FILE_KEY", p, "v1")
	require.NoError(t, e)
	plain, e := c.Decrypt("test", envelope)
	require.NoError(t, e)
	require.Equal(t, "password", string(plain))
	require.NoError(t, os.WriteFile(p, []byte("broken"), 0600))
	_, e = LoadOrCreateCipher("TEST_CASCADE_FILE_KEY", p, "v1")
	require.Error(t, e)
	raw, _ := os.ReadFile(p)
	require.Equal(t, "broken", string(raw))
}
func TestConcurrentKeyCreation(t *testing.T) {
	t.Setenv("TEST_CASCADE_FILE_KEY", "")
	p := filepath.Join(t.TempDir(), "secrets", "cascade.key")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := LoadOrCreateCipher("TEST_CASCADE_FILE_KEY", p, "v1")
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
}
