package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type PreflightDatabaseConfig interface {
	GetString(string) string
	GetInt(string) int
}

// PreflightDatabaseDialect verifies that the read-only preflight has exactly
// one configured database and that it is the database selected by the
// installation configuration.
func PreflightDatabaseDialect(config PreflightDatabaseConfig) (string, error) {
	if config == nil {
		return "", errors.New("database configuration is unavailable")
	}
	dialect := NormalizeDatabaseDialect(config.GetString("gormv2.usedbtype"))
	if dialect == "" {
		return "", fmt.Errorf("unsupported database dialect %q", config.GetString("gormv2.usedbtype"))
	}
	configured := []struct {
		dialect string
		key     string
	}{
		{dialect: "mysql", key: "gormv2.mysql.isinitglobalgormmysql"},
		{dialect: "sqlserver", key: "gormv2.sqlserver.isinitglobalgormsqlserver"},
		{dialect: "postgresql", key: "gormv2.postgresql.isinitglobalgormpostgresql"},
		{dialect: "sqlite", key: "gormv2.sqlite.isinitglobalgormsqlite"},
	}
	enabled := ""
	for _, candidate := range configured {
		if config.GetInt(candidate.key) != 1 {
			continue
		}
		if enabled != "" {
			return "", errors.New("upgrade preflight requires exactly one enabled database")
		}
		enabled = candidate.dialect
	}
	if enabled == "" {
		return "", errors.New("upgrade preflight requires exactly one enabled database")
	}
	if enabled != dialect {
		return "", fmt.Errorf("enabled database %q does not match gormv2.usedbtype %q", enabled, dialect)
	}
	return dialect, nil
}

type PreflightSnapshotCollector func(context.Context) (PreflightSnapshot, error)

// WritePreflightSnapshot emits exactly one JSON value after the collector has
// successfully gathered all evidence. Callers keep errors on stderr.
func WritePreflightSnapshot(ctx context.Context, output io.Writer, collect PreflightSnapshotCollector) error {
	if ctx == nil {
		return errors.New("preflight context is required")
	}
	if output == nil {
		return errors.New("preflight output is required")
	}
	if collect == nil {
		return errors.New("preflight collector is required")
	}
	snapshot, err := collect(ctx)
	if err != nil {
		return fmt.Errorf("collect upgrade preflight: %w", err)
	}
	return json.NewEncoder(output).Encode(snapshot)
}
