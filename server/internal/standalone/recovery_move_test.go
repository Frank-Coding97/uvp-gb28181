package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryMoveResolvesActualDirectoryState(t *testing.T) {
	for _, mode := range []string{"complete", "already-moved", "both-present", "both-missing", "wrong-identity", "rename-failed", "moved-with-error", "rename-noop"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
			require.NoError(t, os.Mkdir(source, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(source, "state"), []byte("preserve"), 0600))
			identity, err := recoveryTreeIdentity(context.Background(), source)
			require.NoError(t, err)
			calls := 0
			rename := func(from, to string) error {
				calls++
				if mode == "rename-failed" {
					return errors.New("injected failure")
				}
				if mode == "rename-noop" {
					return nil
				}
				err := backupPublish(from, to)
				if err == nil && mode == "moved-with-error" {
					return errors.New("uncertain result")
				}
				return err
			}
			switch mode {
			case "already-moved":
				require.NoError(t, backupPublish(source, target))
			case "both-present":
				require.NoError(t, os.Mkdir(target, 0700))
			case "both-missing":
				require.NoError(t, os.RemoveAll(source))
			case "wrong-identity":
				identity = strings.Repeat("a", 64)
			}
			err = recoveryMoveTree(context.Background(), source, target, identity, rename)
			if mode == "complete" || mode == "already-moved" || mode == "moved-with-error" {
				require.NoError(t, err)
				actual, err := recoveryTreeIdentity(context.Background(), target)
				require.NoError(t, err)
				require.Equal(t, identity, actual)
				_, err = os.Lstat(source)
				require.ErrorIs(t, err, os.ErrNotExist)
				previousCalls := calls
				require.NoError(t, recoveryMoveTree(context.Background(), source, target, identity, rename))
				require.Equal(t, previousCalls, calls)
			} else {
				require.Error(t, err)
				if mode != "both-missing" {
					raw, err := os.ReadFile(filepath.Join(source, "state"))
					require.NoError(t, err)
					require.Equal(t, "preserve", string(raw))
				}
			}
			if mode == "already-moved" || mode == "both-present" || mode == "both-missing" || mode == "wrong-identity" {
				require.Zero(t, calls)
			}
		})
	}
}
