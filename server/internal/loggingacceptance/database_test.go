package loggingacceptance

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

type acceptanceDatabase struct {
	DB       *sql.DB
	Host     string
	Port     int
	Name     string
	User     string
	Password string
}

type acceptanceMySQLEnvironment struct {
	binary  string
	baseDir string
	dataDir string
}

var forbiddenSchemaStatements = regexp.MustCompile(`(?im)(?:^|[;\n])[\t ]*(?:USE|CREATE[\t ]+(?:DATABASE|SCHEMA)|DROP[\t ]+(?:DATABASE|SCHEMA)|ALTER[\t ]+(?:DATABASE|SCHEMA))\b`)

// These tables can contain device, media-node, SIP, cascade, or catalog state
// that would make an acceptance run contact an external system.  The release
// schema currently has no seed rows in them, but clear them defensively after
// importing the schema in case that changes.
var acceptanceRuntimeTables = []string{
	"meta_node",
	"gb_device",
	"gb_channel",
	"gb_channel_mount",
	"gb_catalog_node",
	"gb_custom_group",
	"gb_custom_group_device",
	"gb_device_control_state",
	"gb_device_status_event",
	"gb_device_subscription",
	"gb_mobile_position_history",
	"gb_mobile_position_latest",
	"gb_sip_config",
	"gb_cascade_platform",
	"gb_cascade_device_projection",
	"gb_cascade_channel_projection",
	"gb_cascade_media_session",
	"gb_zlm_managed_resource",
}

func newAcceptanceDatabase(t *testing.T) acceptanceDatabase {
	t.Helper()

	env := acceptanceMySQLTestEnvironment(t)
	schemaPath := acceptanceSchemaPath(t)
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("读取 MySQL release schema 失败: %v", err)
	}
	if err := validateAcceptanceSchema(string(schema)); err != nil {
		t.Fatalf("拒绝导入不受限的 MySQL schema: %v", err)
	}
	schemaText := normalizeAcceptanceSchema(string(schema))

	runDir, err := os.MkdirTemp("/tmp", "uvp-logging-mysql-")
	if err != nil {
		t.Fatalf("创建 MySQL 临时目录失败: %v", err)
	}
	if !isTemporaryPath(runDir) {
		_ = os.RemoveAll(runDir)
		t.Fatalf("MySQL 临时目录不在 /tmp 下")
	}

	socketPath := filepath.Join(runDir, "mysql.sock")
	pidPath := filepath.Join(runDir, "mysql.pid")
	logPath := filepath.Join(runDir, "mysqld.err")
	secureDir := filepath.Join(runDir, "secure")
	if err := os.Mkdir(secureDir, 0700); err != nil {
		_ = os.RemoveAll(runDir)
		t.Fatalf("创建 secure-file-priv 目录失败: %v", err)
	}

	port, err := freeLoopbackPort()
	if err != nil {
		_ = os.RemoveAll(runDir)
		t.Fatalf("分配 MySQL 临时端口失败: %v", err)
	}
	if err := assertDataDirAvailable(env.dataDir); err != nil {
		_ = os.RemoveAll(runDir)
		t.Fatalf("拒绝使用已被 mysqld 占用的 datadir: %v", err)
	}

	var (
		server        *exec.Cmd
		serverWait    <-chan error
		serverStarted bool
		serverWaited  bool
		serverErr     error
		adminDB       *sql.DB
		db            *sql.DB
		databaseName  string
		userName      string
	)

	pollServer := func() (error, bool) {
		if serverWait == nil || serverWaited {
			return serverErr, serverWaited
		}
		select {
		case serverErr = <-serverWait:
			serverWaited = true
			return serverErr, true
		default:
			return nil, false
		}
	}
	waitServer := func(timeout time.Duration) bool {
		if serverWait == nil || serverWaited {
			return serverWaited
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case serverErr = <-serverWait:
			serverWaited = true
			return true
		case <-timer.C:
			return false
		}
	}

	t.Cleanup(func() {
		if db != nil {
			if err := db.Close(); err != nil {
				t.Errorf("关闭受限 MySQL 连接失败: %v", err)
			}
			db = nil
		}
		if adminDB != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if databaseName != "" {
				if _, err := adminDB.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteMySQLIdentifier(databaseName)); err != nil {
					t.Errorf("清理临时 MySQL database 失败: %v", err)
				}
			}
			if userName != "" {
				if _, err := adminDB.ExecContext(ctx, "DROP USER IF EXISTS "+quoteMySQLAccount(userName)); err != nil {
					t.Errorf("清理临时 MySQL user 失败: %v", err)
				}
			}
			cancel()
			if err := adminDB.Close(); err != nil {
				t.Errorf("关闭 MySQL 管理连接失败: %v", err)
			}
			adminDB = nil
		}
		if serverStarted && !serverWaited {
			if server != nil && server.Process != nil {
				if err := server.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
					t.Errorf("停止自启 mysqld 失败: %v", err)
				}
			}
			if !waitServer(8 * time.Second) {
				if server != nil && server.Process != nil {
					if err := server.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
						t.Errorf("超时后终止自启 mysqld 失败: %v", err)
					}
				}
				if !waitServer(2 * time.Second) {
					t.Errorf("自启 mysqld 在 SIGTERM/Kill 后仍未退出")
				}
			}
		}
		if err := os.RemoveAll(runDir); err != nil {
			t.Errorf("清理 MySQL 临时目录失败: %v", err)
		}
	})

	server = exec.Command(env.binary,
		"--no-defaults",
		"--basedir", env.baseDir,
		"--datadir", env.dataDir,
		"--bind-address=127.0.0.1",
		"--port="+strconv.Itoa(port),
		"--mysqlx=0",
		"--skip-name-resolve",
		"--socket="+socketPath,
		"--pid-file="+pidPath,
		"--log-error="+logPath,
		"--secure-file-priv="+secureDir,
	)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatalf("创建 mysqld 日志文件失败: %v", err)
	}
	server.Stdout = logFile
	server.Stderr = logFile
	if err := server.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("启动自启 mysqld 失败: %v\n日志尾部:\n%s", err, acceptanceLogTail(logPath))
	}
	serverStarted = true
	_ = logFile.Close()
	waitDone := make(chan error, 1)
	serverWait = waitDone
	go func() { waitDone <- server.Wait() }()

	adminDB, err = sql.Open("mysql", acceptanceMySQLDSN(mysql.Config{
		User:            "root",
		Net:             "unix",
		Addr:            socketPath,
		ParseTime:       true,
		MultiStatements: true,
		Timeout:         2 * time.Second,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
	}))
	if err != nil {
		t.Fatalf("打开 MySQL Unix 管理连接失败: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	adminDB.SetMaxIdleConns(1)

	deadline := time.Now().Add(30 * time.Second)
	for {
		if processErr, done := pollServer(); done {
			t.Fatalf("自启 mysqld 在 ready 前退出: %v\n日志尾部:\n%s", processErr, acceptanceLogTail(logPath))
		}
		if err := pingAcceptanceDB(adminDB, 750*time.Millisecond); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待自启 mysqld ready 超时\n日志尾部:\n%s", acceptanceLogTail(logPath))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := verifyAcceptanceServer(adminDB, env.dataDir, socketPath, port); err != nil {
		t.Fatalf("ready 的 MySQL 不是 helper 自启实例: %v", err)
	}

	databaseName = "uvp_t16_db_" + acceptanceRandomHex(t, 8)
	userName = "uvp_t16_u_" + acceptanceRandomHex(t, 8)
	password := "T16" + acceptanceRandomHex(t, 24)
	if err := createAcceptanceDatabase(context.Background(), adminDB, databaseName, userName, password); err != nil {
		t.Fatalf("创建临时 MySQL database/user 失败: %v", err)
	}

	db, err = sql.Open("mysql", acceptanceMySQLDSN(mysql.Config{
		User:            userName,
		Passwd:          password,
		Net:             "tcp",
		Addr:            net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		DBName:          databaseName,
		ParseTime:       true,
		MultiStatements: true,
		Timeout:         2 * time.Second,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
	}))
	if err != nil {
		t.Fatalf("打开临时 MySQL TCP 连接失败: %v", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := pingAcceptanceDB(db, 5*time.Second); err != nil {
		t.Fatalf("临时 MySQL TCP 连接不可用: %v", err)
	}
	if err := importAcceptanceSchema(db, schemaText); err != nil {
		t.Fatalf("导入 MySQL release schema 失败: %v", err)
	}
	if err := clearAcceptanceRuntimeRows(db); err != nil {
		t.Fatalf("清理临时库运行时入口数据失败: %v", err)
	}
	if err := verifyAcceptanceSchema(db, databaseName); err != nil {
		t.Fatalf("临时 MySQL schema 验证失败: %v", err)
	}

	return acceptanceDatabase{DB: db, Host: "127.0.0.1", Port: port, Name: databaseName, User: userName, Password: password}
}

func TestLoggingAcceptanceDatabaseIsolation(t *testing.T) {
	database := newAcceptanceDatabase(t)
	if database.Host != "127.0.0.1" || database.Port == 0 || database.Name == "" || database.User == "" || database.Password == "" {
		t.Fatalf("helper 返回的 TCP database 连接信息不完整")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got int
	if err := database.DB.QueryRowContext(ctx, "SELECT 40 + 2").Scan(&got); err != nil {
		t.Fatalf("TCP 读取失败: %v", err)
	}
	if got != 42 {
		t.Fatalf("TCP 读取结果错误: got=%d", got)
	}
	probeTable := "t16_tcp_probe_" + acceptanceRandomHex(t, 4)
	if _, err := database.DB.ExecContext(ctx, "CREATE TEMPORARY TABLE "+quoteMySQLIdentifier(probeTable)+" (value VARCHAR(32) NOT NULL)"); err != nil {
		t.Fatalf("TCP 写入准备失败: %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, "INSERT INTO "+quoteMySQLIdentifier(probeTable)+" (value) VALUES (?)", "isolated"); err != nil {
		t.Fatalf("TCP 写入失败: %v", err)
	}
	var value string
	if err := database.DB.QueryRowContext(ctx, "SELECT value FROM "+quoteMySQLIdentifier(probeTable)).Scan(&value); err != nil {
		t.Fatalf("TCP 回读失败: %v", err)
	}
	if value != "isolated" {
		t.Fatalf("TCP 回读结果错误: got=%q", value)
	}
	for _, table := range acceptanceRuntimeTables {
		var rows int
		if err := database.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteMySQLIdentifier(table)).Scan(&rows); err != nil {
			t.Fatalf("检查 %s 入口数据失败: %v", table, err)
		}
		if rows != 0 {
			t.Fatalf("入口表 %s 存在预置运行时数据: %d", table, rows)
		}
	}
}

func acceptanceMySQLTestEnvironment(t *testing.T) acceptanceMySQLEnvironment {
	t.Helper()
	values := map[string]string{}
	for _, key := range []string{"UVP_LOGGING_MYSQLD", "UVP_LOGGING_MYSQL_BASEDIR", "UVP_LOGGING_MYSQL_DATADIR"} {
		value, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			t.Skip("设置 UVP_LOGGING_MYSQLD、UVP_LOGGING_MYSQL_BASEDIR、UVP_LOGGING_MYSQL_DATADIR 后才运行隔离 MySQL acceptance")
		}
		values[key] = filepath.Clean(value)
	}
	if err := validateTemporaryPath(values["UVP_LOGGING_MYSQLD"], false, true); err != nil {
		t.Fatalf("UVP_LOGGING_MYSQLD 无效: %v", err)
	}
	if err := validateTemporaryPath(values["UVP_LOGGING_MYSQL_BASEDIR"], true, false); err != nil {
		t.Fatalf("UVP_LOGGING_MYSQL_BASEDIR 无效: %v", err)
	}
	if err := validateTemporaryPath(values["UVP_LOGGING_MYSQL_DATADIR"], true, false); err != nil {
		t.Fatalf("UVP_LOGGING_MYSQL_DATADIR 无效: %v", err)
	}
	return acceptanceMySQLEnvironment{
		binary:  values["UVP_LOGGING_MYSQLD"],
		baseDir: values["UVP_LOGGING_MYSQL_BASEDIR"],
		dataDir: values["UVP_LOGGING_MYSQL_DATADIR"],
	}
}

func validateTemporaryPath(path string, wantDir, wantExecutable bool) error {
	if !filepath.IsAbs(path) || !isTemporaryPath(path) {
		return fmt.Errorf("路径必须是 /tmp 下的绝对路径")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if wantDir && !info.IsDir() {
		return fmt.Errorf("路径不是目录")
	}
	if !wantDir && info.IsDir() {
		return fmt.Errorf("路径不能是目录")
	}
	if wantExecutable && info.Mode()&0111 == 0 {
		return fmt.Errorf("文件不可执行")
	}
	return nil
}

func isTemporaryPath(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	resolved = filepath.Clean(resolved)
	for _, root := range []string{"/tmp", "/private/tmp"} {
		if resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func assertDataDirAvailable(dataDir string) error {
	output, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return fmt.Errorf("检查 mysqld 进程失败: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if !looksLikeMySQLServer(line) || !commandUsesDataDir(line, dataDir) {
			continue
		}
		return fmt.Errorf("发现 mysqld 使用目标 datadir")
	}
	return nil
}

func looksLikeMySQLServer(commandLine string) bool {
	for _, field := range strings.Fields(commandLine) {
		field = strings.Trim(field, "'\"")
		base := filepath.Base(field)
		if base == "mysqld" || base == "mysqld_safe" || strings.HasPrefix(base, "mysqld-") {
			return true
		}
	}
	return false
}

func commandUsesDataDir(commandLine, dataDir string) bool {
	want := filepath.Clean(dataDir)
	fields := strings.Fields(commandLine)
	for i, field := range fields {
		field = strings.Trim(field, "'\"")
		if strings.HasPrefix(field, "--datadir=") && filepath.Clean(strings.TrimPrefix(field, "--datadir=")) == want {
			return true
		}
		if field == "--datadir" && i+1 < len(fields) && filepath.Clean(strings.Trim(fields[i+1], "'\"")) == want {
			return true
		}
	}
	return false
}

func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func acceptanceSchemaPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 loggingacceptance 测试文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "resource", "database", "uvp-gb28181.sql"))
}

func validateAcceptanceSchema(schema string) error {
	masked := maskSQL(schema)
	if match := forbiddenSchemaStatements.FindStringIndex(masked); match != nil {
		return fmt.Errorf("包含 USE/CREATE DATABASE/DROP DATABASE/ALTER DATABASE 语句(offset %d)", match[0])
	}
	return nil
}

func normalizeAcceptanceSchema(schema string) string {
	// The current release dump has one duplicate statement terminator.  Remove
	// the empty multi-statement element in memory; the checked-in schema stays
	// untouched and all statements still run through the restricted user.
	return strings.ReplaceAll(schema, ";;", ";")
}

func maskSQL(input string) string {
	const (
		normal byte = iota
		singleQuote
		doubleQuote
		backtick
		lineComment
		blockComment
	)
	masked := []byte(input)
	state := normal
	for i := 0; i < len(input); i++ {
		c := input[i]
		switch state {
		case normal:
			switch c {
			case '\'', '"', '`':
				masked[i] = ' '
				if c == '\'' {
					state = singleQuote
				} else if c == '"' {
					state = doubleQuote
				} else {
					state = backtick
				}
			case '#':
				masked[i] = ' '
				state = lineComment
			case '-':
				if i+2 < len(input) && input[i+1] == '-' && (input[i+2] == ' ' || input[i+2] == '\t' || input[i+2] == '\r' || input[i+2] == '\n') {
					masked[i], masked[i+1] = ' ', ' '
					i++
					state = lineComment
				}
			case '/':
				if i+1 < len(input) && input[i+1] == '*' {
					masked[i], masked[i+1] = ' ', ' '
					i++
					state = blockComment
				}
			}
		case lineComment:
			if c != '\n' {
				masked[i] = ' '
			} else {
				state = normal
			}
		case blockComment:
			if c == '*' && i+1 < len(input) && input[i+1] == '/' {
				masked[i], masked[i+1] = ' ', ' '
				i++
				state = normal
			} else if c != '\n' {
				masked[i] = ' '
			}
		case singleQuote, doubleQuote, backtick:
			quote := byte('`')
			if state == singleQuote {
				quote = '\''
			} else if state == doubleQuote {
				quote = '"'
			}
			if c == '\\' && state != backtick && i+1 < len(input) {
				masked[i] = ' '
				i++
				if input[i] != '\n' {
					masked[i] = ' '
				}
				continue
			}
			if c == quote {
				masked[i] = ' '
				if i+1 < len(input) && input[i+1] == quote {
					masked[i+1] = ' '
					i++
					continue
				}
				state = normal
			} else if c != '\n' {
				masked[i] = ' '
			}
		}
	}
	return string(masked)
}

func acceptanceRandomHex(t *testing.T, byteCount int) string {
	t.Helper()
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("生成临时 MySQL 随机标识失败: %v", err)
	}
	return hex.EncodeToString(buf)
}

func acceptanceMySQLConfig(config mysql.Config) mysql.Config {
	if config.Params == nil {
		config.Params = map[string]string{}
	}
	config.Params["charset"] = "utf8mb4"
	config.Params["loc"] = "Local"
	return config
}

func acceptanceMySQLDSN(config mysql.Config) string {
	config = acceptanceMySQLConfig(config)
	return config.FormatDSN()
}

func pingAcceptanceDB(db *sql.DB, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return db.PingContext(ctx)
}

func verifyAcceptanceServer(adminDB *sql.DB, wantDataDir, wantSocket string, wantPort int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var gotPort int
	var gotSocket, gotDataDir string
	if err := adminDB.QueryRowContext(ctx, "SELECT @@port, @@socket, @@datadir").Scan(&gotPort, &gotSocket, &gotDataDir); err != nil {
		return err
	}
	if gotPort != wantPort {
		return fmt.Errorf("port=%d, want %d", gotPort, wantPort)
	}
	if filepath.Clean(gotSocket) != filepath.Clean(wantSocket) {
		return fmt.Errorf("socket 与 helper 目标不符")
	}
	if filepath.Clean(gotDataDir) != filepath.Clean(wantDataDir) {
		return fmt.Errorf("datadir 与 helper 目标不符")
	}
	return nil
}

func createAcceptanceDatabase(ctx context.Context, adminDB *sql.DB, databaseName, userName, password string) error {
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+quoteMySQLIdentifier(databaseName)+" CHARACTER SET utf8mb4"); err != nil {
		return err
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE USER "+quoteMySQLAccount(userName)+" IDENTIFIED BY "+quoteMySQLString(password)); err != nil {
		return err
	}
	_, err := adminDB.ExecContext(ctx, "GRANT ALL PRIVILEGES ON "+quoteMySQLIdentifier(databaseName)+".* TO "+quoteMySQLAccount(userName))
	return err
}

func importAcceptanceSchema(db *sql.DB, schema string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := db.ExecContext(ctx, schema)
	return err
}

func clearAcceptanceRuntimeRows(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, table := range acceptanceRuntimeTables {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&count); err != nil {
			return fmt.Errorf("检查表 %s: %w", table, err)
		}
		if count != 1 {
			return fmt.Errorf("schema 缺少入口表 %s", table)
		}
	}
	if _, err := db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
		return err
	}
	for _, table := range acceptanceRuntimeTables {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+quoteMySQLIdentifier(table)); err != nil {
			return fmt.Errorf("清理表 %s: %w", table, err)
		}
	}
	_, err := db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=1")
	return err
}

func verifyAcceptanceSchema(db *sql.DB, databaseName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var currentDatabase string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&currentDatabase); err != nil {
		return err
	}
	if currentDatabase != databaseName {
		return fmt.Errorf("当前 database=%s, want 临时 database", currentDatabase)
	}
	for _, table := range []string{"sys_jobs", "sys_users", "sys_casbin_rule", "sys_operation_logs"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?", table).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("schema 缺少 %s", table)
		}
	}
	return nil
}

func quoteMySQLIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func quoteMySQLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quoteMySQLAccount(userName string) string {
	return quoteMySQLString(userName) + "@'127.0.0.1'"
}

func acceptanceLogTail(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(日志不可读)"
	}
	if len(data) > 4096 {
		data = data[len(data)-4096:]
	}
	return strings.TrimSpace(string(data))
}
