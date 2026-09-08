package standalone

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUncleanRecoveryContextCannotCrossOperationKinds(t *testing.T) {
	outer := maintenanceTestJournal(t.TempDir())
	outer.Schema, outer.Kind, outer.CandidateVersion = 2, maintenanceKindUnclean, ""
	outer.RunMarkerSHA256 = strings.Repeat("d", 64)
	outer.ReleaseSetSHA256 = strings.Repeat("e", 64)
	digest := recoveryContextSHA256(outer)
	require.Len(t, digest, 64)
	for _, mode := range []string{"marker", "snapshot", "source-current", "release-set", "operation", "source-config", "source-data"} {
		changed := outer
		switch mode {
		case "marker":
			changed.RunMarkerSHA256 = strings.Repeat("f", 64)
		case "snapshot":
			changed.BackupManifestSHA256 = strings.Repeat("f", 64)
		case "source-current":
			changed.OldCurrentSHA256 = strings.Repeat("f", 64)
		case "release-set":
			changed.ReleaseSetSHA256 = strings.Repeat("f", 64)
		case "operation":
			changed.OperationID = strings.Repeat("f", 64)
		case "source-config":
			changed.SourceConfigSHA256 = strings.Repeat("f", 64)
		case "source-data":
			changed.SourceDataSHA256 = strings.Repeat("f", 64)
		}
		require.NotEqual(t, digest, recoveryContextSHA256(changed), mode)
	}
	j := recoveryJournal{Schema: 2, ContextSHA256: digest, OperationID: outer.OperationID, BackupManifestSHA256: outer.BackupManifestSHA256, OldVersion: outer.OldVersion, OldCurrentSHA256: outer.OldCurrentSHA256, ReleaseSetSHA256: outer.ReleaseSetSHA256, Phase: "staging"}
	require.NoError(t, validateRecoveryJournal(j))
	require.True(t, recoveryContextMatches(j, outer))
	j.Schema = 1
	require.Error(t, validateRecoveryJournal(j))
	require.False(t, recoveryContextMatches(j, outer))
}
