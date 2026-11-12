package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeExternalBackupRunner struct {
	calls    int
	command  string
	args     []string
	env      map[string]string
	output   string
	maxBytes uint64
	body     []byte
}

type externalBackupConfig map[string]any

func (c externalBackupConfig) GetString(key string) string {
	if value, ok := c[key].(string); ok {
		return value
	}
	return ""
}

func (c externalBackupConfig) GetInt(key string) int {
	if value, ok := c[key].(int); ok {
		return value
	}
	return 0
}

func (f *fakeExternalBackupRunner) RunToFile(_ context.Context, command string, args []string, env map[string]string, output string, maxBytes uint64) error {
	f.calls++
	f.command, f.args, f.env = command, append([]string(nil), args...), env
	f.output, f.maxBytes = output, maxBytes
	body := f.body
	if body == nil {
		body = []byte("backup")
	}
	if uint64(len(body)) > maxBytes {
		return errExternalBackupTooLarge
	}
	return os.WriteFile(output, body, 0600)
}

func TestExternalBackupKeepsPasswordOutOfArguments(t *testing.T) {
	runner := &fakeExternalBackupRunner{}
	output := filepath.Join(t.TempDir(), "mysql.sql")
	if err := ExternalBackup(context.Background(), ExternalBackupSpec{Dialect: "mysql", Host: "db.internal", Port: 3306, Database: "uvp", User: "backup", Password: "secret", Output: output, MaxBytes: 1024}, runner); err != nil {
		t.Fatal(err)
	}
	if runner.command != "mysqldump" || runner.env["MYSQL_PWD"] != "secret" {
		t.Fatalf("runner = %#v", runner)
	}
	if strings.Contains(strings.Join(runner.args, " "), "secret") {
		t.Fatalf("password leaked into argv: %v", runner.args)
	}
	if strings.Contains(strings.Join(runner.args, " "), "--result-file") || runner.output != output || runner.maxBytes != 1024 {
		t.Fatalf("backup did not use bounded output: %#v", runner)
	}
}

func TestExternalBackupRejectsUnsupportedDialectAndEmptyOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "backup")
	err := ExternalBackup(context.Background(), ExternalBackupSpec{Dialect: "oracle", Host: "db", Port: 1, Database: "db", User: "u", Password: "p", Output: output, MaxBytes: 1}, &fakeExternalBackupRunner{})
	if err == nil {
		t.Fatal("unsupported dialect accepted")
	}
	if err := os.WriteFile(output, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	err = ExternalBackup(context.Background(), ExternalBackupSpec{Dialect: "postgresql", Host: "db", Port: 5432, Database: "db", User: "u", Password: "p", Output: output, MaxBytes: 1}, &fakeExternalBackupRunner{})
	if err == nil {
		t.Fatal("existing output accepted")
	}
}

func TestExternalBackupSpecFromConfigSelectsWriteDatabaseAndRejectsMultipleDialects(t *testing.T) {
	config := externalBackupConfig{
		"gormv2.usedbtype": "postgresql",
		"gormv2.postgresql.isinitglobalgormpostgresql": 1,
		"gormv2.postgresql.write.host":                 "db.internal",
		"gormv2.postgresql.write.port":                 5432,
		"gormv2.postgresql.write.database":             "uvp",
		"gormv2.postgresql.write.user":                 "backup",
		"gormv2.postgresql.write.pass":                 "secret",
	}
	spec, enabled, err := ExternalBackupSpecFromConfig(config, filepath.Join(t.TempDir(), "backup.dump"), 8<<20)
	if err != nil || !enabled || spec.Dialect != "postgresql" || spec.Password != "secret" {
		t.Fatalf("spec=%#v enabled=%v err=%v", spec, enabled, err)
	}
	config["gormv2.mysql.isinitglobalgormmysql"] = 1
	if _, _, err := ExternalBackupSpecFromConfig(config, spec.Output, spec.MaxBytes); err == nil {
		t.Fatal("multiple database dialects were accepted")
	}
}

func TestExternalBackupRegistrationFromConfigOmitsPassword(t *testing.T) {
	config := externalBackupConfig{
		"gormv2.usedbtype":                   "mysql",
		"gormv2.mysql.isinitglobalgormmysql": 1,
		"gormv2.mysql.write.host":            "db.internal",
		"gormv2.mysql.write.port":            3306,
		"gormv2.mysql.write.database":        "uvp",
		"gormv2.mysql.write.user":            "backup",
		"gormv2.mysql.write.pass":            "must-not-be-copied",
	}
	registration, enabled, err := ExternalBackupRegistrationFromConfig(config, "UVP_UPGRADE_DB_PASSWORD", 8<<20)
	if err != nil || !enabled {
		t.Fatalf("registration=%#v enabled=%v err=%v", registration, enabled, err)
	}
	if registration.PasswordEnv != "UVP_UPGRADE_DB_PASSWORD" || registration.Database != "uvp" || registration.MaxBackupBytes != 8<<20 {
		t.Fatalf("registration=%#v", registration)
	}
	if registration.PasswordEnv == config.GetString("gormv2.mysql.write.pass") {
		t.Fatal("registration copied the database password")
	}
}

func TestExternalBackupRegistrationFromConfigDisablesSQLite(t *testing.T) {
	registration, enabled, err := ExternalBackupRegistrationFromConfig(externalBackupConfig{"gormv2.usedbtype": "sqlite"}, "UVP_UPGRADE_DB_PASSWORD", 8<<20)
	if err != nil || enabled || registration != nil {
		t.Fatalf("registration=%#v enabled=%v err=%v", registration, enabled, err)
	}
}

func TestExternalBackupRegistrationKeepsSQLServerUpgradeBlockedBeforeStop(t *testing.T) {
	config := externalBackupConfig{
		"gormv2.usedbtype":                           "sqlserver",
		"gormv2.sqlserver.isinitglobalgormsqlserver": 1,
		"gormv2.sqlserver.write.host":                "db.internal",
		"gormv2.sqlserver.write.port":                1433,
		"gormv2.sqlserver.write.database":            "uvp",
		"gormv2.sqlserver.write.user":                "backup",
	}
	registration, enabled, err := ExternalBackupRegistrationFromConfig(config, "UVP_UPGRADE_DB_PASSWORD", 8<<20)
	if err != nil || !enabled || registration == nil || registration.Dialect != "sqlserver" {
		t.Fatalf("registration=%#v enabled=%v err=%v", registration, enabled, err)
	}
	t.Setenv("UVP_UPGRADE_DB_PASSWORD", "secret")
	spec, err := registration.spec(filepath.Join(t.TempDir(), "database.backup"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ExternalBackup(context.Background(), spec, &fakeExternalBackupRunner{}); err == nil {
		t.Fatal("SQL Server automatic backup was accepted")
	}
}

func TestExternalBackupSpecInfersTheOnlyEnabledDialectWhenUsedbtypeIsOmitted(t *testing.T) {
	config := externalBackupConfig{
		"gormv2.mysql.isinitglobalgormmysql": 1,
		"gormv2.mysql.write.host":            "db.internal",
		"gormv2.mysql.write.database":        "uvp",
		"gormv2.mysql.write.user":            "backup",
		"gormv2.mysql.write.pass":            "secret",
	}
	spec, enabled, err := ExternalBackupSpecFromConfig(config, filepath.Join(t.TempDir(), "backup.sql"), 8<<20)
	if err != nil || !enabled || spec.Dialect != "mysql" {
		t.Fatalf("spec=%#v enabled=%v err=%v", spec, enabled, err)
	}
}

func TestExternalBackupRejectsUnboundedAndSQLServerBackups(t *testing.T) {
	output := filepath.Join(t.TempDir(), "backup")
	for _, spec := range []ExternalBackupSpec{
		{Dialect: "mysql", Host: "db", Port: 3306, Database: "uvp", User: "backup", Password: "secret", Output: output},
		{Dialect: "sqlserver", Host: "db", Port: 1433, Database: "uvp", User: "backup", Password: "secret", Output: output, MaxBytes: 8 << 20},
	} {
		if err := ExternalBackup(context.Background(), spec, &fakeExternalBackupRunner{}); err == nil {
			t.Fatalf("unsafe external backup was accepted: %#v", spec)
		}
	}
}
