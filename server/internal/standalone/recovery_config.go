package standalone

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"gopkg.in/yaml.v3"
)

// The offline coordinator holds InstanceLock throughout restoration. This
// replaces both short-term credential roots atomically, while the persistent
// gate continues blocking business startup until all recovery steps succeed.
func rotateRecoveryCredentials(paths Paths, operationID, expectedConfigSHA256 string, hook func(string) error) error {
	return withConfigLock(paths.InstallDir, func() error {
		if err := requireMaintenanceInstanceLock(paths.InstallDir); err != nil {
			return err
		}
		journal, err := ReadMaintenanceJournal(paths.InstallDir)
		if err != nil {
			return err
		}
		if journal.OperationID != operationID || journal.Phase != MaintenanceRestoring {
			return errors.New("credential rotation requires the owning restore operation")
		}
		cfg, err := LoadConfig(paths)
		if err != nil {
			return err
		}
		if cfg.ConfigSHA256 != expectedConfigSHA256 {
			return errors.New("restore configuration changed")
		}
		token := cfg.values["token"].(map[string]any)
		for _, key := range []string{"jwttokensignkey", "instancegeneration"} {
			var raw [32]byte
			if _, err := rand.Read(raw[:]); err != nil {
				return errors.New("cannot generate recovery credentials")
			}
			token[key] = base64.RawURLEncoding.EncodeToString(raw[:])
		}
		raw, err := yaml.Marshal(cfg.values)
		if err != nil {
			return errors.New("cannot encode recovery configuration")
		}
		if _, err := decodeInstanceConfig(raw); err != nil {
			return err
		}
		return writeSecureConfigFile(paths.ConfigFile, raw, true, hook)
	})
}
