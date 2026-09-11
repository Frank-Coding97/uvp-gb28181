package standalone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaintenanceCompatibilityRequiresTrustedActualBackends(t *testing.T) {
	for _, mode := range []string{"trusted", "empty", "old-untrusted", "candidate-untrusted", "malformed", "candidate-tampered", "retained-untrusted", "retained-trusted", "retained-invalid"} {
		t.Run(mode, func(t *testing.T) {
			old := newTestReleaseFixture(t, "1.2.3-win10")
			candidate := newTestReleaseFixtureAt(t, old.installDir, "2.0.0-win10", false)
			// Distinct bytes make the test discriminate which binary is trusted.
			candidatePath := filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath))
			data := []byte("different candidate backend")
			require.NoError(t, os.WriteFile(candidatePath, data, 0700))
			candidate.manifest.Files[0].Data = data
			candidate.manifest.Files[0].SHA256 = testSHA256(data)
			writeTestReleaseManifest(t, candidate)
			oldSHA := old.manifest.Files[0].SHA256
			candidateSHA := testSHA256(data)
			trust := oldSHA + "," + candidateSHA
			if strings.HasPrefix(mode, "retained-") {
				retained := newTestReleaseFixtureAt(t, old.installDir, "0.9.0-win10", false)
				retainedData := []byte("retained backend")
				require.NoError(t, os.WriteFile(filepath.Join(retained.releaseDir, filepath.FromSlash(releaseBackendPath)), retainedData, 0700))
				retained.manifest.Files[0].SHA256 = testSHA256(retainedData)
				writeTestReleaseManifest(t, retained)
				if mode == "retained-trusted" {
					trust += "," + testSHA256(retainedData)
				}
				if mode == "retained-invalid" {
					require.NoError(t, os.WriteFile(filepath.Join(retained.releaseDir, "manifest.json"), []byte("broken"), 0600))
				}
			}
			switch mode {
			case "empty":
				trust = ""
			case "old-untrusted":
				trust = candidateSHA
			case "candidate-untrusted":
				trust = oldSHA
			case "malformed":
				trust += ",not-a-sha256"
			case "candidate-tampered":
				require.NoError(t, os.WriteFile(candidatePath, []byte("tampered"), 0700))
			}
			before := maintenanceCompatibilitySnapshot(t, old.installDir)
			current, next, err := loadMaintenanceReleasesWithTrust(old.installDir, candidate.manifest.Version, trust)
			if mode == "trusted" || mode == "retained-trusted" {
				require.NoError(t, err)
				require.Equal(t, old.manifest.Version, current.Version)
				require.Equal(t, candidate.manifest.Version, next.Version)
			} else {
				require.Error(t, err)
				require.Empty(t, current)
				require.Empty(t, next)
			}
			require.Equal(t, before, maintenanceCompatibilitySnapshot(t, old.installDir))
		})
	}
}

func maintenanceCompatibilitySnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Windows exclusively holds this coordination file during a
			// transaction. Its bytes are not installation data.
			if path == filepath.Join(root, ".uvp-instance.lock") {
				files[path] = "instance lock"
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = testSHA256(data)
		} else {
			files[path] = "directory"
		}
		return nil
	}))
	return files
}
