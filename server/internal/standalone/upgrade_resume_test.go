package standalone

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpgradeCompletionResumeRequiresDurableProof(t *testing.T) {
	for _, mode := range []string{"complete", "committing", "wrong-operation", "busy", "untrusted"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			if mode != "committing" {
				require.NoError(t, owner.markCompletionReady(context.Background()))
			}
			if mode != "busy" {
				require.NoError(t, owner.Close())
			}
			operation, trust := owner.journal.OperationID, owner.trust
			if mode == "wrong-operation" {
				operation = strings.Repeat("b", 64)
			}
			if mode == "untrusted" {
				trust = strings.Repeat("c", 64)
			}
			err := resumeUpgradeCompletionWithTrust(context.Background(), owner.paths, operation, trust)
			if mode == "complete" {
				require.NoError(t, err)
				require.NoError(t, CheckMaintenanceGate(owner.paths.InstallDir))
			} else {
				require.Error(t, err)
				require.ErrorIs(t, CheckMaintenanceGate(owner.paths.InstallDir), ErrMaintenanceRequired)
			}
		})
	}
}
