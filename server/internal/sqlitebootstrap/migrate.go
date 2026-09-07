package sqlitebootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/resource/database/sqlitebaseline"
)

const migrationMarkerTable = "gb_schema_migrations"

const (
	sqliteMigrationSuffix = "-sqlite.sql"
)

type migrationCheck func(context.Context, *gorm.DB, *sql.Conn) error

// migrationSpec is deliberately private. A release adds one explicit
// <version>-sqlite.sql file and one independently reviewed checksum to the
// compile-time list below; there is no directory scan or runtime discovery.
type migrationSpec struct {
	Version string
	SQL     string
	SHA256  string
	Before  migrationCheck
	After   migrationCheck
}

// compiledMigrations is empty for the current release. The baseline is the
// only SQLite schema input today; no synthetic incremental migration is run.
var compiledMigrations = []migrationSpec{}

// Migrate applies the ordered, checksum-pinned SQLite increments after the
// release baseline has already been initialized. It never initializes a
// database and never executes a migration before a matching baseline marker.
func Migrate(ctx context.Context, db *gorm.DB) error {
	return migrateWithSpecs(ctx, db, compiledMigrations)
}

func migrateWithSpecs(ctx context.Context, db *gorm.DB, specs []migrationSpec) error {
	if db == nil || db.Dialector == nil {
		return errors.New("SQLite migration requires a database")
	}
	if db.Dialector.Name() != "sqlite" {
		return errors.New("SQLite migration requires sqlite")
	}
	if err := validateMigrationSpecs(specs); err != nil {
		return err
	}

	if len(specs) == 0 {
		return migrateOne(ctx, db, specs, nil)
	}
	for i := range specs {
		if err := migrateOne(ctx, db, specs, &specs[i]); err != nil {
			return err
		}
	}
	return nil
}

func migrateOne(ctx context.Context, db *gorm.DB, specs []migrationSpec, current *migrationSpec) error {
	return withForeignKeysOffImmediateTransaction(ctx, db, func(tx *gorm.DB, conn *sql.Conn) error {
		markers, err := readMigrationMarkers(ctx, conn)
		if err != nil {
			return err
		}
		if err := validateMarkerState(markers, specs); err != nil {
			return err
		}
		if err := validateAppliedOrder(markers, specs); err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		if _, applied := markers[current.Version]; applied {
			return nil
		}
		return applyMigration(ctx, tx, conn, *current)
	})
}

func validateMigrationSpecs(specs []migrationSpec) error {
	seen := make(map[string]struct{}, len(specs)+1)
	seen[sqlitebaseline.Version] = struct{}{}
	previousVersion := ""
	for _, spec := range specs {
		if spec.Version == "" {
			return errors.New("SQLite migration version is empty")
		}
		if _, duplicate := seen[spec.Version]; duplicate {
			return fmt.Errorf("duplicate SQLite migration version %q", spec.Version)
		}
		seen[spec.Version] = struct{}{}
		if err := validateMigrationVersion(spec.Version); err != nil {
			return err
		}
		if previousVersion != "" && spec.Version <= previousVersion {
			return fmt.Errorf("SQLite migrations must be in strict order: %q before %q", previousVersion, spec.Version)
		}
		previousVersion = spec.Version
		if strings.TrimSpace(spec.SQL) == "" {
			return fmt.Errorf("SQLite migration %q has empty SQL", spec.Version)
		}
		if fmt.Sprintf("%x", sha256.Sum256([]byte(spec.SQL))) != spec.SHA256 {
			return fmt.Errorf("SQLite migration %q checksum mismatch", spec.Version)
		}
		if err := validateMigrationSQL(spec.SQL); err != nil {
			return fmt.Errorf("SQLite migration %q: %w", spec.Version, err)
		}
	}
	return nil
}

func validateMigrationVersion(version string) error {
	if !strings.HasSuffix(version, sqliteMigrationSuffix) {
		return fmt.Errorf("SQLite migration %q must use the %s filename suffix", version, sqliteMigrationSuffix)
	}
	if len(version) < len("2006-01-02-")+len(sqliteMigrationSuffix) || version[10] != '-' {
		return fmt.Errorf("SQLite migration %q must start with YYYY-MM-DD-", version)
	}
	migrationDate, err := time.Parse("2006-01-02", version[:10])
	if err != nil {
		return fmt.Errorf("SQLite migration %q has invalid date: %w", version, err)
	}
	baselineDate, err := baselineReleaseDate()
	if err != nil {
		return fmt.Errorf("read SQLite baseline date: %w", err)
	}
	if migrationDate.Before(baselineDate) {
		return fmt.Errorf("SQLite migration %q predates the release baseline", version)
	}
	return nil
}

func baselineReleaseDate() (time.Time, error) {
	const prefix = "sqlite-baseline-"
	if !strings.HasPrefix(sqlitebaseline.Version, prefix) || len(sqlitebaseline.Version) < len(prefix)+len("20060102") {
		return time.Time{}, errors.New("baseline version has no YYYYMMDD date")
	}
	return time.Parse("20060102", sqlitebaseline.Version[len(prefix):len(prefix)+len("20060102")])
}

func readMigrationMarkers(ctx context.Context, conn *sql.Conn) (map[string]string, error) {
	var tableCount int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", migrationMarkerTable).Scan(&tableCount); err != nil {
		return nil, fmt.Errorf("read SQLite migration marker table: %w", err)
	}
	if tableCount != 1 {
		return nil, errors.New("SQLite migration requires a matching baseline marker")
	}

	rows, err := conn.QueryContext(ctx, "SELECT version, checksum FROM "+migrationMarkerTable)
	if err != nil {
		return nil, fmt.Errorf("read SQLite migration markers: %w", err)
	}
	markers := make(map[string]string)
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rowErr := errors.Join(err, rows.Close())
			return nil, fmt.Errorf("read SQLite migration markers: %w", rowErr)
		}
		if _, duplicate := markers[version]; duplicate {
			_ = rows.Close()
			return nil, fmt.Errorf("duplicate SQLite migration marker %q", version)
		}
		markers[version] = checksum
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, fmt.Errorf("read SQLite migration markers: %w", err)
	}
	return markers, nil
}

func validateMarkerState(markers map[string]string, specs []migrationSpec) error {
	baselineChecksum, ok := markers[sqlitebaseline.Version]
	if !ok {
		return errors.New("SQLite migration requires a matching baseline marker")
	}
	if baselineChecksum != sqlitebaseline.SHA256 {
		return errors.New("stored SQLite baseline checksum mismatch")
	}

	known := make(map[string]string, len(specs)+1)
	known[sqlitebaseline.Version] = sqlitebaseline.SHA256
	for _, spec := range specs {
		known[spec.Version] = spec.SHA256
	}
	for version, checksum := range markers {
		expected, knownVersion := known[version]
		if !knownVersion {
			return fmt.Errorf("unknown SQLite migration marker %q", version)
		}
		if checksum != expected {
			return fmt.Errorf("stored SQLite migration checksum mismatch for %q", version)
		}
	}
	return nil
}

func validateAppliedOrder(markers map[string]string, specs []migrationSpec) error {
	for i, spec := range specs {
		if _, applied := markers[spec.Version]; applied {
			continue
		}
		for _, later := range specs[i+1:] {
			if _, applied := markers[later.Version]; applied {
				return fmt.Errorf("SQLite migration %q is applied before %q", later.Version, spec.Version)
			}
		}
	}
	return nil
}

func applyMigration(ctx context.Context, tx *gorm.DB, conn *sql.Conn, spec migrationSpec) error {
	if spec.Before != nil {
		if err := spec.Before(ctx, tx, conn); err != nil {
			return fmt.Errorf("SQLite migration %q before-check: %w", spec.Version, err)
		}
	}
	// Execute the complete script as one driver batch. Statement splitting
	// would corrupt literals/comments and would change SQLite transaction
	// semantics.
	if _, err := conn.ExecContext(ctx, spec.SQL); err != nil {
		return fmt.Errorf("SQLite migration %q SQL: %w", spec.Version, err)
	}
	if spec.After != nil {
		if err := spec.After(ctx, tx, conn); err != nil {
			return fmt.Errorf("SQLite migration %q after-check: %w", spec.Version, err)
		}
	}
	if err := checkForeignKeys(ctx, conn); err != nil {
		return fmt.Errorf("SQLite migration %q foreign keys: %w", spec.Version, err)
	}
	if err := checkSQLiteIntegrity(ctx, conn); err != nil {
		return fmt.Errorf("SQLite migration %q integrity: %w", spec.Version, err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO "+migrationMarkerTable+"(version, applied_at, checksum) VALUES(?, ?, ?)", spec.Version, time.Now().UTC(), spec.SHA256); err != nil {
		return fmt.Errorf("SQLite migration %q marker: %w", spec.Version, err)
	}
	return nil
}

func checkForeignKeys(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	rowErr := rows.Err()
	closeErr := rows.Close()
	if err := errors.Join(rowErr, closeErr); err != nil {
		return err
	}
	if invalid {
		return errors.New("foreign key check failed")
	}
	return nil
}

func checkSQLiteIntegrity(ctx context.Context, conn *sql.Conn) error {
	var integrity string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("quick_check returned %q", integrity)
	}
	return nil
}

func validateMigrationSQL(script string) error {
	tokens, err := scanSQLTokens(script)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return errors.New("SQL contains no statements")
	}
	for _, token := range tokens {
		switch token {
		case "BEGIN", "COMMIT", "END", "ROLLBACK", "SAVEPOINT", "RELEASE":
			return fmt.Errorf("transaction control %q is not allowed", token)
		case "PRAGMA":
			return errors.New("PRAGMA is not allowed")
		case "ATTACH", "DETACH":
			return fmt.Errorf("database attachment %q is not allowed", token)
		case "VACUUM":
			return errors.New("VACUUM is not allowed")
		case "TRIGGER":
			return errors.New("triggers are not supported by SQLite migrations")
		}
	}
	return nil
}

func scanSQLTokens(script string) ([]string, error) {
	tokens := make([]string, 0)
	for i := 0; i < len(script); {
		c := script[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ';':
			i++
		case c == '-' && i+1 < len(script) && script[i+1] == '-':
			i += 2
			for i < len(script) && script[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(script) && script[i+1] == '*':
			end := strings.Index(script[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("unterminated SQL comment")
			}
			i += end + 4
		case c == '\'' || c == '"' || c == '`':
			if err := skipSQLQuote(script, &i, c); err != nil {
				return nil, err
			}
		case c == '[':
			if err := skipBracketIdentifier(script, &i); err != nil {
				return nil, err
			}
		case isSQLIdentifierStart(c):
			start := i
			i++
			for i < len(script) && isSQLIdentifierPart(script[i]) {
				i++
			}
			tokens = append(tokens, strings.ToUpper(script[start:i]))
		default:
			i++
		}
	}
	return tokens, nil
}

func skipSQLQuote(script string, index *int, quote byte) error {
	(*index)++
	for *index < len(script) {
		if script[*index] != quote {
			(*index)++
			continue
		}
		if *index+1 < len(script) && script[*index+1] == quote {
			*index += 2
			continue
		}
		(*index)++
		return nil
	}
	return errors.New("unterminated SQL quoted literal")
}

func skipBracketIdentifier(script string, index *int) error {
	(*index)++
	for *index < len(script) {
		if script[*index] != ']' {
			(*index)++
			continue
		}
		if *index+1 < len(script) && script[*index+1] == ']' {
			*index += 2
			continue
		}
		(*index)++
		return nil
	}
	return errors.New("unterminated SQL bracket identifier")
}

func isSQLIdentifierStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isSQLIdentifierPart(c byte) bool {
	return isSQLIdentifierStart(c) || c >= '0' && c <= '9' || c == '$'
}
