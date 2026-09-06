package loggingacceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

const acceptancePrefix = "/api/__logging_acceptance"

// TestLoggingAcceptance starts the actual application, including bootstrap,
// migrations, authentication, audit workers, scheduler and shutdown coordinator.
// It requires an explicitly provisioned, isolated MySQL installation.
func TestLoggingAcceptance(t *testing.T) {
	if os.Getenv("UVP_LOGGING_MYSQLD") == "" {
		t.Skip("set UVP_LOGGING_MYSQLD and isolated MySQL directories")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "logging-acceptance")
	build := exec.Command("go", "build", "-tags", "logging_acceptance", "-o", binary, ".")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, mode := range []string{"development", "release"} {
		t.Run(mode, func(t *testing.T) { runAcceptance(t, root, binary, mode) })
	}
}

type acceptanceResponse struct {
	Status int
	ID     string
	Body   map[string]any
}

func runAcceptance(t *testing.T, root, binary, mode string) {
	db := newAcceptanceDatabase(t)
	password := "Logging-Acceptance-Only-832!"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE sys_users SET username=?,password=?,status=1 WHERE id=1", "logging_acceptance_admin", string(hash)); err != nil {
		t.Fatal(err)
	}
	// This database is private to this test. Explicit permissions make a denial
	// meaningful even if the repository seed contains administrator wildcards.
	if _, err := db.DB.Exec("DELETE FROM sys_casbin_rule"); err != nil {
		t.Fatal(err)
	}
	policy := "INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3,v4,v5) VALUES (?,?,?,?,?,'','')"
	if _, err := db.DB.Exec(policy, "g", "user_1", "role_1", "*", ""); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ path, method string }{{"ok", "GET"}, {"slow-sql", "GET"}, {"panic", "GET"}, {"block", "GET"}, {"business-failure", "POST"}, {"scheduler", "POST"}} {
		if _, err := db.DB.Exec(policy, "p", "role_1", acceptancePrefix+"/"+route.path, route.method, "*"); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	address := acceptanceAddress(t)
	logPath := filepath.Join(dir, "logs", "application.json")
	writeAcceptanceConfig(t, root, dir, address, logPath, mode, db)
	consolePath := filepath.Join(dir, "console.log")
	console, err := os.Create(consolePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = console.Close() })
	cmd := exec.Command(binary)
	cmd.Dir = dir
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "UVP_GB28181_CASCADE_KEY=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Stdout, cmd.Stderr = console, console
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			out, _ := os.ReadFile(consolePath)
			t.Logf("backend console:\n%s", out)
		}
	})
	waitAcceptance(t, 30*time.Second, func() bool {
		select {
		case err := <-done:
			exited = true
			t.Fatalf("backend exited before ready: %v", err)
		default:
		}
		return hasAcceptanceEvent(readAcceptanceLogs(logPath), "http.ready", nil)
	}, "HTTP listener readiness")
	base := "http://" + address
	client := &http.Client{Timeout: 10 * time.Second}
	request := func(method, path, token string, body any) acceptanceResponse {
		return acceptanceRequest(t, client, base, method, path, token, body)
	}
	denied := request("GET", acceptancePrefix+"/ok", "", nil)
	if denied.Status != http.StatusUnauthorized {
		t.Fatalf("missing JWT status=%d, want 401", denied.Status)
	}
	login := request("POST", "/api/login", "", map[string]any{"username": "logging_acceptance_admin", "password": password})
	if login.Status != 200 || login.Body["code"] != float64(0) {
		t.Fatalf("login failed: status=%d code=%v", login.Status, login.Body["code"])
	}
	data, ok := login.Body["data"].(map[string]any)
	if !ok {
		t.Fatal("login data missing")
	}
	token, _ := data["accessToken"].(string)
	if token == "" {
		t.Fatal("login token missing")
	}
	forbidden := request("GET", acceptancePrefix+"/forbidden", token, nil)
	if forbidden.Status != http.StatusForbidden {
		t.Fatalf("Casbin denied status=%d, want 403", forbidden.Status)
	}
	normal := request("GET", acceptancePrefix+"/ok", token, nil)
	if normal.Status != 200 || normal.Body["code"] != float64(0) {
		t.Fatalf("normal response: status=%d code=%v", normal.Status, normal.Body["code"])
	}
	business := request("POST", acceptancePrefix+"/business-failure", token, map[string]any{"input": "fixture"})
	if business.Status != 200 || business.Body["code"] == float64(0) {
		t.Fatal("business failure response contract changed")
	}
	slow := request("GET", acceptancePrefix+"/slow-sql", token, nil)
	if slow.Status != 200 || slow.Body["code"] != float64(0) {
		t.Fatal("slow SQL handler failed")
	}
	panicked := request("GET", acceptancePrefix+"/panic", token, nil)
	if panicked.Status != 500 {
		t.Fatalf("panic status=%d", panicked.Status)
	}
	jobID := "logging-acceptance-" + mode
	scheduled := request("POST", acceptancePrefix+"/scheduler", token, map[string]any{"job_id": jobID, "duration_ms": 5000})
	if scheduled.Status != 200 || scheduled.Body["code"] != float64(0) {
		t.Fatalf("scheduler request failed: code=%v", scheduled.Body["code"])
	}
	// Real in-flight HTTP and scheduler work must exist before SIGTERM.
	blockDone := make(chan acceptanceResponse, 1)
	go func() { blockDone <- request("GET", acceptancePrefix+"/block?ms=1500", token, nil) }()
	waitAcceptance(t, 5*time.Second, func() bool {
		rows := readAcceptanceLogs(logPath)
		return hasAcceptanceEvent(rows, "scheduler.acceptance.started", map[string]any{"job_id": jobID}) && hasAcceptanceEvent(rows, "acceptance.http.started", nil)
	}, "in-flight HTTP and scheduler work")
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		exited = true
		if err != nil {
			t.Fatalf("graceful shutdown: %v", err)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("shutdown deadline exceeded")
	}
	var blocked acceptanceResponse
	select {
	case blocked = <-blockDone:
		if blocked.Status != 200 {
			t.Fatalf("in-flight HTTP status=%d", blocked.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight HTTP did not complete")
	}
	rows := readAcceptanceLogs(logPath)
	for _, item := range []struct {
		event  string
		fields map[string]any
	}{
		{"http.access", map[string]any{"request_id": normal.ID, "http_status": float64(200)}},
		{"http.access", map[string]any{"request_id": blocked.ID, "http_status": float64(200)}},
		{"acceptance.http.completed", map[string]any{"request_id": blocked.ID}},
		{"casbin.permission.denied", map[string]any{"request_id": forbidden.ID, "level": "warn"}},
		{"acceptance.http.completed", map[string]any{"request_id": normal.ID}},
		{"http.access", map[string]any{"request_id": business.ID, "business_success": false, "http_status": float64(200)}},
		{"db.slow_query", map[string]any{"request_id": slow.ID, "level": "warn"}},
		{"http.panic", map[string]any{"request_id": panicked.ID, "level": "error"}},
		{"cascade.credential_key_unavailable", map[string]any{"level": "warn"}}, {"lifecycle.stopped", nil},
	} {
		if !hasAcceptanceEvent(rows, item.event, item.fields) {
			t.Errorf("missing event %s with fields %v", item.event, item.fields)
		}
	}
	if hasAcceptanceEvent(rows, "lifecycle.shutdown_incomplete", nil) {
		t.Error("incomplete shutdown recorded")
	}
	if hasAcceptanceEvent(rows, "http.route_registered", nil) {
		t.Error("route logging enabled despite routes=false")
	}
	finalSeen := false
	for _, row := range rows {
		if finalSeen {
			t.Error("application log appeared after lifecycle.stopped")
			break
		}
		finalSeen = row["event"] == "lifecycle.stopped"
	}
	for _, row := range rows {
		if row["event"] == "http.panic" && row["request_id"] == panicked.ID {
			if stack, ok := row["stack"].(string); !ok || stack == "" {
				t.Error("panic stack missing")
			}
		}
		if row["event"] == "db.slow_query" && row["request_id"] == slow.ID {
			if row["template_fingerprint"] == nil || row["duration_ms"] == nil {
				t.Error("slow SQL metadata missing")
			}
		}
	}
	if !hasAcceptanceEvent(rows, "scheduler.acceptance.canceled", map[string]any{"job_id": jobID}) && !hasAcceptanceEvent(rows, "scheduler.acceptance.completed", map[string]any{"job_id": jobID}) {
		t.Error("actual executor terminal event missing")
	}
	for _, id := range []string{normal.ID, business.ID, slow.ID, panicked.ID, blocked.ID} {
		count := 0
		for _, row := range rows {
			if row["event"] == "http.access" && row["request_id"] == id {
				count++
				if row["client_request_id"] != "acceptance-client-correlation" {
					t.Error("client correlation missing")
				}
			}
		}
		if count != 1 {
			t.Errorf("access records for %s=%d, want one", id, count)
		}
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
		if !json.Valid(line) {
			t.Error("persisted log contains invalid JSON")
		}
	}
	for _, secret := range []string{password, token, "logging-acceptance-secret-panic"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Error("sensitive fixture content escaped into application log")
		}
	}
	consoleBytes, _ := os.ReadFile(consolePath)
	if mode == "release" && bytes.Contains(consoleBytes, []byte(`"event":"http.access"`)) {
		t.Error("console=false still emitted access records")
	}
	if mode == "development" && !bytes.Contains(consoleBytes, []byte(`"event":"http.access"`)) {
		t.Error("explicit stdout output missing")
	}
	var jobStatus string
	if err := db.DB.QueryRow("SELECT status FROM sys_job_results WHERE job_id=?", jobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "SUCCESS" {
		t.Errorf("drained scheduler terminal status=%s, want SUCCESS", jobStatus)
	}
	var results int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM sys_job_results WHERE job_id=?", jobID).Scan(&results); err != nil {
		t.Fatal(err)
	}
	if results != 1 {
		t.Errorf("scheduler persisted %d results, want exactly one", results)
	}
	var logins, operations int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM sys_login_logs WHERE username=?", "logging_acceptance_admin").Scan(&logins); err != nil {
		t.Fatal(err)
	}
	if logins != 1 {
		t.Errorf("login audit count=%d, want 1", logins)
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM sys_operation_logs WHERE path IN (?,?)", acceptancePrefix+"/business-failure", acceptancePrefix+"/scheduler").Scan(&operations); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{acceptancePrefix + "/business-failure", acceptancePrefix + "/scheduler"} {
		var count int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM sys_operation_logs WHERE path=?", path).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("operation audit count for %s=%d, want 1", path, count)
		}
	}
	if operations != 2 {
		t.Errorf("operation audit count=%d, want 2", operations)
	}
	t.Logf("%s: actual TCP requests correlated, login audits=%d operation audits=%d scheduler results=%d, graceful shutdown and final file record verified", mode, logins, operations, results)
}

func writeAcceptanceConfig(t *testing.T, root, dir, address, logPath, mode string, db acceptanceDatabase) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(root, "config", "config.example.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(source, &config); err != nil {
		t.Fatal(err)
	}
	set := func(path string, value any) {
		parts := strings.Split(path, ".")
		current := config
		for _, part := range parts[:len(parts)-1] {
			next, ok := current[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				current[part] = next
			}
			current = next
		}
		current[parts[len(parts)-1]] = value
	}
	for path, value := range map[string]any{
		"server.appdebug": mode == "development", "server.cachetype": "memory", "server.syslog": true,
		"server.notcheckuser": []any{}, "server.demoaccount.enabled": false,
		"safe.loginlockthreshold": 0, "captcha.open": false, "httpserver.port": address,
		"token.jwttokensignkey": "isolated-logging-acceptance-signing-key-832562", "token.isCache": false,
		"logs.filepath": logPath, "logs.textformat": "json", "logs.stdoutformat": "json", "logs.level": "warn",
		"logs.modules": map[string]any{"access": "info", "http": "info", "scheduler": "info", "lifecycle": "info", "acceptance": "info"},
		"logs.routes":  false, "logs.console": false, "logs.outputs": []any{"file", "stdout"},
		"gormv2.mysql.slowthreshold": 1, "gormv2.mysql.isopenreaddb": 0,
		"gb28181.zlm.host": "", "gb28181.zlm.secret": "", "gb28181.trace.enabled": false,
		"gb28181.recording.reconcile_interval_sec": 0, "gb28181.recording.catalog_reconcile_interval_sec": 0,
	} {
		set(path, value)
	}
	for _, target := range []string{"write", "read"} {
		set("gormv2.mysql."+target, map[string]any{"host": db.Host, "port": db.Port, "database": db.Name, "user": db.User, "pass": db.Password, "prefix": "", "charset": "utf8mb4", "setmaxidleconns": 2, "setmaxopenconns": 8, "setconnmaxlifetime": 60, "setconnmaxidletime": 30})
	}
	if mode == "release" {
		delete(config["logs"].(map[string]any), "outputs")
	}
	encoded, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "config.yml"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	version, err := os.ReadFile(filepath.Join(root, "version.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "version.json"), version, 0600); err != nil {
		t.Fatal(err)
	}
}

func acceptanceAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func acceptanceRequest(t *testing.T, client *http.Client, base, method, path, token string, body any) acceptanceResponse {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "acceptance-client-correlation")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	result, err := client.Do(req)
	if err != nil {
		t.Errorf("HTTP %s %s failed: %v", method, path, err)
		return acceptanceResponse{}
	}
	defer result.Body.Close()
	response := acceptanceResponse{Status: result.StatusCode, ID: result.Header.Get("X-Request-ID")}
	if response.ID == "" || response.ID == "acceptance-client-correlation" {
		t.Errorf("server request ID missing or reused client ID: %s", response.ID)
	}
	if err := json.NewDecoder(result.Body).Decode(&response.Body); err != nil {
		t.Errorf("HTTP %s JSON response: %v", path, err)
	}
	return response
}

func readAcceptanceLogs(path string) []map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rows []map[string]any
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row map[string]any
		if json.Unmarshal(line, &row) == nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func hasAcceptanceEvent(rows []map[string]any, event string, fields map[string]any) bool {
	for _, row := range rows {
		if row["event"] != event {
			continue
		}
		matched := true
		for key, value := range fields {
			if row[key] != value {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func waitAcceptance(t *testing.T, timeout time.Duration, ready func() bool, label string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("timed out waiting for %s", label))
}
