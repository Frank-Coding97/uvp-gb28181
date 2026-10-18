package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type upgradePreflightConfig map[string]interface{}

func (c upgradePreflightConfig) GetString(key string) string {
	value, _ := c[key].(string)
	return value
}

func (c upgradePreflightConfig) GetInt(key string) int {
	switch value := c[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	default:
		return 0
	}
}

func TestPreflightDatabaseDialectRequiresOneMatchingEnabledDatabase(t *testing.T) {
	const (
		mysqlEnabled    = "gormv2.mysql.isinitglobalgormmysql"
		postgresEnabled = "gormv2.postgresql.isinitglobalgormpostgresql"
		sqliteEnabled   = "gormv2.sqlite.isinitglobalgormsqlite"
	)
	tests := []struct {
		name   string
		config upgradePreflightConfig
		want   string
		failed string
	}{
		{
			name:   "sqlite",
			config: upgradePreflightConfig{"gormv2.usedbtype": "sqlite", sqliteEnabled: 1},
			want:   "sqlite",
		},
		{
			name:   "postgres alias",
			config: upgradePreflightConfig{"gormv2.usedbtype": "postgres", postgresEnabled: 1},
			want:   "postgresql",
		},
		{
			name:   "unsupported configured dialect",
			config: upgradePreflightConfig{"gormv2.usedbtype": "oracle", mysqlEnabled: 1},
			failed: "unsupported",
		},
		{
			name:   "no enabled database",
			config: upgradePreflightConfig{"gormv2.usedbtype": "mysql"},
			failed: "exactly one",
		},
		{
			name: "multiple enabled databases",
			config: upgradePreflightConfig{
				"gormv2.usedbtype": "mysql", mysqlEnabled: 1, sqliteEnabled: 1,
			},
			failed: "exactly one",
		},
		{
			name:   "enabled database differs from configured dialect",
			config: upgradePreflightConfig{"gormv2.usedbtype": "sqlserver", mysqlEnabled: 1},
			failed: "does not match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := PreflightDatabaseDialect(test.config)
			if test.failed != "" {
				if err == nil || !strings.Contains(err.Error(), test.failed) {
					t.Fatalf("PreflightDatabaseDialect() error = %v, want %q", err, test.failed)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("PreflightDatabaseDialect() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestWritePreflightSnapshotWritesOneJSONValue(t *testing.T) {
	want := PreflightSnapshot{
		CurrentVersion:  "1.2.3",
		CurrentBuildID:  "build-a",
		ConfigDigest:    strings.Repeat("a", 64),
		DatabaseDialect: "sqlite",
		MigrationDigest: strings.Repeat("b", 64),
	}
	var output bytes.Buffer
	if err := WritePreflightSnapshot(context.Background(), &output, func(context.Context) (PreflightSnapshot, error) {
		return want, nil
	}); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var got PreflightSnapshot
	if err := decoder.Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("snapshot = %#v, want %#v", got, want)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("preflight output contains more than one JSON value: %v", err)
	}
}

func TestWritePreflightSnapshotDoesNotWriteOnFailure(t *testing.T) {
	var output bytes.Buffer
	failed := errors.New("database unavailable")
	err := WritePreflightSnapshot(context.Background(), &output, func(context.Context) (PreflightSnapshot, error) {
		return PreflightSnapshot{}, failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("error = %v, want %v", err, failed)
	}
	if output.Len() != 0 {
		t.Fatalf("failed preflight wrote %q", output.String())
	}
}
