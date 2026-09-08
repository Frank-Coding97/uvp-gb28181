package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

var admittedMaintenance *standalone.MaintenancePermitClaims

// MaintenanceOperation exposes only the identity of an already consumed
// startup permit. It cannot authorize a second action or disclose its secret.
func MaintenanceOperation() (standalone.MaintenancePermitClaims, bool) {
	if admittedMaintenance == nil {
		return standalone.MaintenancePermitClaims{}, false
	}
	return *admittedMaintenance, true
}

func authorizeMaintenanceStartup(paths standalone.Paths) error {
	err := standalone.CheckMaintenanceGate(paths.InstallDir)
	if err == nil {
		return nil
	}
	if !errors.Is(err, standalone.ErrMaintenanceRequired) {
		return err
	}
	denied := func() error {
		return fmt.Errorf("%w: maintenance admission rejected", standalone.ErrMaintenanceRequired)
	}
	journal, err := standalone.ReadMaintenanceJournal(paths.InstallDir)
	if err != nil {
		return denied()
	}
	purpose := ""
	args := os.Args[1:]
	if len(args) == 0 {
		if journal.Phase == standalone.MaintenanceRestoring {
			purpose = "revoke_sessions"
		} else {
			purpose = "candidate_health"
		}
	} else if len(args) == 1 {
		switch args[0] {
		case "-bootstrap-db":
			purpose = "bootstrap_db"
		case "-migrate-up":
			purpose = "migrate_up"
		case "-db-check":
			purpose = "db_check"
		}
	}
	if purpose == "" {
		return denied()
	}
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return denied()
	}
	type result struct {
		claims standalone.MaintenancePermitClaims
		err    error
	}
	completed := make(chan result, 1)
	defer os.Stdin.Close()
	go func() {
		claims, err := standalone.ConsumeMaintenancePermit(paths.InstallDir, purpose, os.Stdin)
		completed <- result{claims, err}
	}()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case outcome := <-completed:
		if outcome.err != nil {
			return denied()
		}
		admittedMaintenance = &outcome.claims
		return nil
	case <-timer.C:
		return denied()
	}
}
