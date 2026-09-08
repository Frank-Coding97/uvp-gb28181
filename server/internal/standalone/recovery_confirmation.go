package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
)

type RecoveryConfirmationInfo struct {
	OperationID string
	BackupTime  time.Time
	Impacts     []string
}

type RecoveryConfirmationInput struct {
	Username        string
	Password        string
	Acknowledgement string
}

// ConfirmRecovery requires the launcher to collect credentials and explicit
// acknowledgement from its local console. No normal component is started.
func ConfirmRecovery(ctx context.Context, root, operation string, prompt func(RecoveryConfirmationInfo) (RecoveryConfirmationInput, error)) error {
	return confirmRecoveryWithTrust(ctx, root, operation, maintenanceBackendSHA256Allowlist, prompt)
}

func confirmRecoveryWithTrust(ctx context.Context, root, operation, trust string, prompt func(RecoveryConfirmationInfo) (RecoveryConfirmationInput, error)) (failure error) {
	if ctx == nil || prompt == nil {
		return errors.New("local recovery confirmation requires a context and prompt")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := AcquireInstanceLock(root)
	if err != nil {
		return err
	}
	defer func() { failure = errors.Join(failure, lock.Close()) }()
	_, manifest, err := verifyRecoveryConfirmation(ctx, root, operation, trust)
	if err != nil {
		return err
	}
	restoredDB := filepath.Join(root, "data", "uvp.db")
	failedDB := filepath.Join(root, maintenanceDirName, "failed", operation, "data", "uvp.db")
	impacts, err := recoveryAuthorizationImpacts(ctx, restoredDB, failedDB)
	if err != nil {
		return err
	}
	info := RecoveryConfirmationInfo{OperationID: operation, BackupTime: manifest.CreatedAt, Impacts: impacts}
	// Bind the receipt to the exact information displayed, including backup time.
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	impactSHA := hex.EncodeToString(digest[:])
	input, err := prompt(info)
	if err != nil {
		return errors.New("local recovery confirmation was cancelled")
	}
	defer func() { input.Password = "" }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if input.Acknowledgement != "CONFIRM "+operation {
		return errors.New("recovery acknowledgement did not match this operation")
	}
	adminID, err := authenticateRecoveryAdministrator(ctx, restoredDB, input.Username, input.Password)
	if err != nil {
		return errors.New("recovery administrator authentication failed")
	}
	return archiveRecoveryConfirmation(ctx, root, operation, trust, adminID, impactSHA, func() error {
		// Read-only authentication is repeated under ConfigLock immediately before
		// publishing the receipt. Directory proof also covers both compared DBs.
		verified, err := authenticateRecoveryAdministrator(ctx, restoredDB, input.Username, input.Password)
		if err != nil || verified != adminID {
			return errors.New("recovery administrator authentication failed")
		}
		return ctx.Err()
	}, backupPublish)
}
