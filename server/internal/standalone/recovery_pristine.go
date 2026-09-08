package standalone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var errRecoveryPristine = errors.New("recovery pristine check failed")

// recoveryPristinePendingAdmin proves that a stopped SQLite database still
// represents an untouched standalone installation. It reads a protected copy
// of the complete SQLite family, including an uncheckpointed WAL, and never
// opens or changes the source database.
//
// The installation package cannot be imported here: it depends on
// app/utils/gormhelper, which imports this package. The installation.Store
// State/readState/validateStateRow rules are therefore kept equivalent below.
func recoveryPristinePendingAdmin(ctx context.Context, dbPath string) (bool, error) {
	if ctx == nil {
		return false, errRecoveryPristine
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	db, cleanup, err := openRecoveryAuthorizationDatabase(ctx, dbPath)
	if err != nil {
		return false, errRecoveryPristine
	}
	defer cleanup()
	if err := rejectKnownRecoveryAuthorizationTables(ctx, db); err != nil {
		return false, err
	}
	if err := requireRecoveryPristineTables(ctx, db); err != nil {
		return false, err
	}

	row, err := readRecoveryPristineInstallationRow(ctx, db)
	if err != nil {
		return false, err
	}
	pristineState, err := validateRecoveryPristineInstallationRow(row)
	if err != nil {
		return false, err
	}
	if !pristineState {
		return false, nil
	}

	for _, check := range []struct {
		query  string
		reason string
	}{
		{query: `SELECT COUNT(*) FROM sys_users`, reason: "sys_users is not empty"},
		{query: `SELECT COUNT(*) FROM sys_user_role`, reason: "sys_user_role is not empty"},
		{query: `SELECT COUNT(*) FROM sys_user_sessions`, reason: "sys_user_sessions is not empty"},
		{query: `SELECT COUNT(*) FROM sys_casbin_rule WHERE ptype = 'g' AND v0 LIKE 'user_%'`, reason: "user Casbin relations are not empty"},
	} {
		count, err := recoveryPristineCount(ctx, db, check.query)
		if err != nil {
			return false, err
		}
		if count != 0 {
			return false, recoveryPristineFailure(check.reason)
		}
	}
	return true, nil
}

type recoveryPristineInstallationRow struct {
	id          int64
	phase       string
	adminUserID sql.NullInt64
	completedAt sql.NullTime
	idType      string
	adminType   string
}

func readRecoveryPristineInstallationRow(ctx context.Context, db *sql.DB) (recoveryPristineInstallationRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, phase, admin_user_id, completed_at, typeof(id), typeof(admin_user_id)
		FROM standalone_installation`)
	if err != nil {
		return recoveryPristineInstallationRow{}, recoveryPristineFailure("read standalone installation state")
	}
	var (
		row     recoveryPristineInstallationRow
		matches int
	)
	for rows.Next() {
		matches++
		if matches > 1 {
			_ = rows.Close()
			return recoveryPristineInstallationRow{}, recoveryPristineFailure("standalone installation state has multiple rows")
		}
		if err := rows.Scan(&row.id, &row.phase, &row.adminUserID, &row.completedAt, &row.idType, &row.adminType); err != nil {
			_ = rows.Close()
			return recoveryPristineInstallationRow{}, recoveryPristineFailure("scan standalone installation state")
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryPristineInstallationRow{}, recoveryPristineFailure("read standalone installation state")
	}
	if err := rows.Close(); err != nil {
		return recoveryPristineInstallationRow{}, recoveryPristineFailure("close standalone installation state")
	}
	if matches != 1 {
		return recoveryPristineInstallationRow{}, recoveryPristineFailure("standalone installation state row is missing")
	}
	if row.id != 1 || row.idType != "integer" {
		return recoveryPristineInstallationRow{}, recoveryPristineFailure("standalone installation state id is invalid")
	}
	if row.adminUserID.Valid && row.adminType != "integer" {
		return recoveryPristineInstallationRow{}, recoveryPristineFailure("standalone installation administrator id is invalid")
	}
	return row, nil
}

// validateRecoveryPristineInstallationRow mirrors installation.Store's
// validateStateRow rules and returns whether the state is eligible for the
// stricter pristine empty-data checks.
func validateRecoveryPristineInstallationRow(row recoveryPristineInstallationRow) (bool, error) {
	switch row.phase {
	case "pending_admin":
		if row.adminUserID.Valid || row.completedAt.Valid {
			return false, recoveryPristineFailure("pending_admin row contains completion metadata")
		}
		return true, nil
	case "pending_sip":
		if !row.adminUserID.Valid || row.adminUserID.Int64 <= 0 || row.completedAt.Valid {
			return false, recoveryPristineFailure("pending_sip row has invalid administrator or completion metadata")
		}
		return false, nil
	case "complete":
		if row.completedAt.Valid && row.adminUserID.Valid && row.adminUserID.Int64 <= 0 {
			return false, recoveryPristineFailure("complete row has an invalid administrator")
		}
		if !row.completedAt.Valid {
			return false, recoveryPristineFailure("complete row has no completion timestamp")
		}
		return false, nil
	default:
		return false, recoveryPristineFailure("unknown standalone installation phase")
	}
}

func requireRecoveryPristineTables(ctx context.Context, db *sql.DB) error {
	for _, table := range []struct {
		name    string
		columns []string
	}{
		{name: "standalone_installation", columns: []string{"id", "phase", "admin_user_id", "completed_at", "created_at", "updated_at"}},
		{name: "sys_users", columns: []string{"id", "username", "password", "status", "deleted_at"}},
		{name: "sys_user_role", columns: []string{"user_id", "role_id"}},
		{name: "sys_user_sessions", columns: []string{"sid", "user_id"}},
		{name: "sys_casbin_rule", columns: []string{"ptype", "v0"}},
	} {
		if err := requireRecoveryPristineTable(ctx, db, table.name, table.columns); err != nil {
			return err
		}
	}
	return nil
}

func requireRecoveryPristineTable(ctx context.Context, db *sql.DB, table string, columns []string) error {
	var tableType string
	if err := db.QueryRowContext(ctx, "SELECT type FROM sqlite_master WHERE name = ?", table).Scan(&tableType); err != nil {
		return recoveryPristineFailure("required table is missing")
	}
	if tableType != "table" {
		return recoveryPristineFailure("required object is not a table")
	}

	rows, err := db.QueryContext(ctx, `PRAGMA table_info("`+table+`")`)
	if err != nil {
		return recoveryPristineFailure("read required table schema")
	}
	found := make(map[string]struct{}, len(columns))
	for rows.Next() {
		var (
			cid        int
			name       string
			declType   string
			notNull    int
			defaultV   any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &declType, &notNull, &defaultV, &primaryKey); err != nil {
			_ = rows.Close()
			return recoveryPristineFailure("scan required table schema")
		}
		found[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return recoveryPristineFailure("read required table schema")
	}
	if err := rows.Close(); err != nil {
		return recoveryPristineFailure("close required table schema")
	}
	for _, column := range columns {
		if _, ok := found[column]; !ok {
			return recoveryPristineFailure("required table column is missing")
		}
	}
	return nil
}

func recoveryPristineCount(ctx context.Context, db *sql.DB, query string) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, recoveryPristineFailure("read pristine data count")
	}
	return count, nil
}

func recoveryPristineFailure(reason string) error {
	return fmt.Errorf("%w: %s", errRecoveryPristine, reason)
}
