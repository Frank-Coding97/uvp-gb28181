package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultExternalBackupMaxBytes keeps external backup output bounded when an
// installation is registered without a deployment-specific limit.
const DefaultExternalBackupMaxBytes uint64 = 8 << 30

type ExternalBackupSpec struct {
	Dialect  string
	Host     string
	Port     int
	Database string
	User     string
	Password string
	Output   string
	MaxBytes uint64
}

// ConfigLookup is the small part of application configuration needed here.
type ConfigLookup interface {
	GetString(string) string
	GetInt(string) int
}

// ExternalBackupSpecFromConfig builds a backup spec from the selected write
// database. SQLite is handled by SQLiteBackup and therefore returns false.
func ExternalBackupSpecFromConfig(config ConfigLookup, output string, maxBytes uint64) (ExternalBackupSpec, bool, error) {
	if config == nil {
		return ExternalBackupSpec{}, false, errors.New("database configuration is required")
	}
	if err := ValidateConfiguredDatabaseSelection(config); err != nil {
		return ExternalBackupSpec{}, false, err
	}
	dialect, err := configuredExternalDialect(config)
	if err != nil {
		return ExternalBackupSpec{}, false, err
	}
	if dialect == "" || dialect == "sqlite" {
		return ExternalBackupSpec{}, false, nil
	}
	if dialect != "mysql" && dialect != "postgresql" && dialect != "sqlserver" {
		return ExternalBackupSpec{}, false, fmt.Errorf("unsupported configured database dialect %q", dialect)
	}
	base := "gormv2." + dialect + ".write."
	port := config.GetInt(base + "port")
	if port == 0 {
		port = map[string]int{"mysql": 3306, "postgresql": 5432, "sqlserver": 1433}[dialect]
	}
	spec := ExternalBackupSpec{Dialect: dialect, Host: config.GetString(base + "host"), Port: port, Database: config.GetString(base + "database"), User: config.GetString(base + "user"), Password: config.GetString(base + "pass"), Output: output, MaxBytes: maxBytes}
	if err := validateExternalBackupSpec(spec); err != nil {
		return ExternalBackupSpec{}, false, err
	}
	return spec, true, nil
}

// ExternalBackupRegistrationFromConfig converts the selected application
// database into the updater-owned registration shape. The password is never
// copied into the registration JSON; the updater resolves it from passwordEnv
// only when a backup is actually executed.
func ExternalBackupRegistrationFromConfig(config ConfigLookup, passwordEnv string, maxBackupBytes uint64) (*ExternalBackupRegistration, bool, error) {
	if config == nil {
		return nil, false, errors.New("database configuration is required")
	}
	passwordEnv = strings.TrimSpace(passwordEnv)
	if passwordEnv == "" || strings.ContainsAny(passwordEnv, "\x00\r\n=") {
		return nil, false, errors.New("external backup password_env is invalid")
	}
	if err := ValidateConfiguredDatabaseSelection(config); err != nil {
		return nil, false, err
	}
	dialect, err := configuredExternalDialect(config)
	if err != nil {
		return nil, false, err
	}
	if dialect == "" || dialect == "sqlite" {
		return nil, false, nil
	}
	if dialect != "mysql" && dialect != "postgresql" && dialect != "sqlserver" {
		return nil, false, fmt.Errorf("unsupported configured database dialect %q", dialect)
	}
	base := "gormv2." + dialect + ".write."
	port := config.GetInt(base + "port")
	if port == 0 {
		port = map[string]int{"mysql": 3306, "postgresql": 5432, "sqlserver": 1433}[dialect]
	}
	registration := &ExternalBackupRegistration{
		Dialect: dialect, Host: config.GetString(base + "host"), Port: port,
		Database: config.GetString(base + "database"), User: config.GetString(base + "user"), PasswordEnv: passwordEnv, MaxBackupBytes: maxBackupBytes,
	}
	if err := validateExternalBackupRegistration(registration); err != nil {
		return nil, false, err
	}
	return registration, true, nil
}

func configuredExternalDialect(config ConfigLookup) (string, error) {
	dialect := strings.ToLower(strings.TrimSpace(config.GetString("gormv2.usedbtype")))
	if dialect != "" {
		return dialect, nil
	}
	var selected string
	for _, candidate := range []struct {
		dialect string
		key     string
	}{
		{"mysql", "gormv2.mysql.isinitglobalgormmysql"},
		{"postgresql", "gormv2.postgresql.isinitglobalgormpostgresql"},
		{"sqlserver", "gormv2.sqlserver.isinitglobalgormsqlserver"},
	} {
		if config.GetInt(candidate.key) == 0 {
			continue
		}
		if selected != "" {
			return "", errors.New("multiple external database dialects are enabled")
		}
		selected = candidate.dialect
	}
	return selected, nil
}

// ValidateConfiguredDatabaseSelection prevents an ambiguous upgrade backup
// target when more than one global GORM database is enabled.
func ValidateConfiguredDatabaseSelection(config ConfigLookup) error {
	if config == nil {
		return errors.New("database configuration is required")
	}
	enabled := 0
	for _, key := range []string{
		"gormv2.mysql.isinitglobalgormmysql",
		"gormv2.postgresql.isinitglobalgormpostgresql",
		"gormv2.sqlserver.isinitglobalgormsqlserver",
	} {
		if config.GetInt(key) != 0 {
			enabled++
		}
	}
	if enabled > 1 {
		return errors.New("multiple external database dialects are enabled")
	}
	return nil
}

// ExternalBackupRegistration keeps the password out of the registration file;
// the updater reads it from the named environment variable at execution time.
type ExternalBackupRegistration struct {
	Dialect        string `json:"dialect"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Database       string `json:"database"`
	User           string `json:"user"`
	PasswordEnv    string `json:"password_env"`
	MaxBackupBytes uint64 `json:"max_backup_bytes"`
}

func (r *ExternalBackupRegistration) spec(output string) (ExternalBackupSpec, error) {
	if r == nil {
		return ExternalBackupSpec{}, nil
	}
	if strings.TrimSpace(r.PasswordEnv) == "" || strings.ContainsAny(r.PasswordEnv, "\x00\r\n=") {
		return ExternalBackupSpec{}, errors.New("external backup password_env is invalid")
	}
	spec := ExternalBackupSpec{Dialect: r.Dialect, Host: r.Host, Port: r.Port, Database: r.Database, User: r.User, Password: os.Getenv(r.PasswordEnv), Output: output, MaxBytes: r.MaxBackupBytes}
	if err := validateExternalBackupSpec(spec); err != nil {
		return ExternalBackupSpec{}, err
	}
	return spec, nil
}

func validateExternalBackupRegistration(r *ExternalBackupRegistration) error {
	if r == nil {
		return errors.New("external backup registration is required")
	}
	if r.Dialect != "mysql" && r.Dialect != "postgresql" && r.Dialect != "sqlserver" {
		return fmt.Errorf("unsupported external database dialect %q", r.Dialect)
	}
	if r.Host == "" || r.Database == "" || r.User == "" || r.Port <= 0 || r.Port > 65535 {
		return errors.New("external backup registration connection is incomplete")
	}
	if strings.ContainsAny(r.Host, "\x00\r\n") || strings.ContainsAny(r.Database, "\x00\r\n") || strings.ContainsAny(r.User, "\x00\r\n") {
		return errors.New("external backup registration contains control characters")
	}
	if r.MaxBackupBytes == 0 {
		return errors.New("external backup max_backup_bytes must be positive")
	}
	return nil
}

type ExternalBackupRunner interface {
	RunToFile(context.Context, string, []string, map[string]string, string, uint64) error
}

type OSExternalBackupRunner struct{}

func (OSExternalBackupRunner) Run(ctx context.Context, name string, args []string, env map[string]string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = os.Environ()
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdout, command.Stderr = nil, nil
	return command.Run()
}

var errExternalBackupTooLarge = errors.New("external backup exceeds registered maximum size")

func (OSExternalBackupRunner) RunToFile(ctx context.Context, name string, args []string, env map[string]string, output string, maxBytes uint64) error {
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	writer := &limitedBackupWriter{writer: file, remaining: maxBytes}
	command := exec.CommandContext(ctx, name, args...)
	command.Env = os.Environ()
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stdout, command.Stderr = writer, io.Discard
	runErr := command.Run()
	if err := file.Sync(); err != nil && runErr == nil && !writer.exceeded {
		runErr = err
	}
	closeErr := file.Close()
	if writer.exceeded {
		return errExternalBackupTooLarge
	}
	if runErr != nil {
		return runErr
	}
	return closeErr
}

type limitedBackupWriter struct {
	writer    io.Writer
	remaining uint64
	exceeded  bool
}

func (w *limitedBackupWriter) Write(data []byte) (int, error) {
	if uint64(len(data)) <= w.remaining {
		n, err := w.writer.Write(data)
		w.remaining -= uint64(n)
		return n, err
	}
	if w.remaining == 0 {
		w.exceeded = true
		return 0, errExternalBackupTooLarge
	}
	allowed := int(w.remaining)
	n, err := w.writer.Write(data[:allowed])
	w.remaining -= uint64(n)
	w.exceeded = true
	if err != nil {
		return n, err
	}
	return n, errExternalBackupTooLarge
}

func ExternalBackup(ctx context.Context, spec ExternalBackupSpec, runner ExternalBackupRunner) error {
	if runner == nil {
		runner = OSExternalBackupRunner{}
	}
	if err := validateExternalBackupSpec(spec); err != nil {
		return err
	}
	if _, err := os.Lstat(spec.Output); err == nil {
		return errors.New("backup output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(spec.Output), 0700); err != nil {
		return err
	}
	var command string
	var args []string
	env := map[string]string{}
	switch strings.ToLower(spec.Dialect) {
	case "mysql":
		command = "mysqldump"
		args = []string{"--host", spec.Host, "--port", strconv.Itoa(spec.Port), "--user", spec.User, "--single-transaction", "--routines", "--events", spec.Database}
		env["MYSQL_PWD"] = spec.Password
	case "postgres", "postgresql":
		command = "pg_dump"
		args = []string{"--host", spec.Host, "--port", strconv.Itoa(spec.Port), "--username", spec.User, "--format=custom", spec.Database}
		env["PGPASSWORD"] = spec.Password
	case "sqlserver":
		return errors.New("automatic SQL Server backup is unsupported because its target file cannot be size-enforced")
	default:
		return fmt.Errorf("unsupported external database dialect %q", spec.Dialect)
	}
	if err := runner.RunToFile(ctx, command, args, env, spec.Output, spec.MaxBytes); err != nil {
		_ = os.Remove(spec.Output)
		return fmt.Errorf("%s backup failed: %w", spec.Dialect, err)
	}
	info, err := os.Stat(spec.Output)
	if err != nil {
		return fmt.Errorf("%s backup did not produce output: %w", spec.Dialect, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || uint64(info.Size()) > spec.MaxBytes {
		_ = os.Remove(spec.Output)
		return fmt.Errorf("%s backup output is empty or not a regular file", spec.Dialect)
	}
	return nil
}

func validateExternalBackupSpec(spec ExternalBackupSpec) error {
	if spec.Host == "" || spec.Database == "" || spec.User == "" || spec.Password == "" {
		return errors.New("external backup connection identity and password are required")
	}
	if spec.Port <= 0 || spec.Port > 65535 {
		return errors.New("external backup port is invalid")
	}
	if spec.Output == "" || !filepath.IsAbs(spec.Output) {
		return errors.New("external backup output must be absolute")
	}
	if spec.MaxBytes == 0 {
		return errors.New("external backup maximum size must be positive")
	}
	if strings.ContainsAny(spec.Database, "\x00\r\n") || strings.ContainsAny(spec.Host, "\x00\r\n") || strings.ContainsAny(spec.User, "\x00\r\n") {
		return errors.New("external backup connection fields contain control characters")
	}
	return nil
}
