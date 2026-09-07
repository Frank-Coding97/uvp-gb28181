package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	redisProbeDatabase = 9
	probeTimeout       = 8 * time.Second
	startupTimeout     = 15 * time.Second
	persistenceRounds  = 20
)

const luaGetDelScript = `local v = redis.call('GET', KEYS[1]); if v then redis.call('DEL', KEYS[1]); end; return v`

type redisInstance struct {
	binary    string
	workspace string
	role      string
	password  string
	address   string
	port      int
	maxMemory string

	configPath string
	cmd        *exec.Cmd
	done       chan struct{}
	stateMu    sync.Mutex
	exitErr    error
	output     bytes.Buffer
}

func runProbe(opts options) (result report) {
	result = newReport(opts)
	defer func() {
		result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		result.Status = aggregateStatus(result.Checks)
	}()

	if opts.redisServer == "" {
		result.Checks = append(result.Checks,
			failedCheck("t02-a", "-redis-server is required"),
			notExecutedCheck("t02-b", "Redis server path was not supplied"),
			notExecutedCheck("t02-c", "Redis server path was not supplied"),
		)
		return result
	}
	if _, err := os.Stat(opts.redisServer); err != nil {
		result.Checks = append(result.Checks,
			failedCheck("t02-a", fmt.Sprintf("redis-server executable is not readable: %v", err)),
			notExecutedCheck("t02-b", "Redis server could not be opened"),
			notExecutedCheck("t02-c", "Redis server could not be opened"),
		)
		return result
	}
	if opts.sourceSHA != "" && !isSHA1(opts.sourceSHA) {
		result.Checks = append(result.Checks,
			failedCheck("t02-a", "-source-sha must be a 40-character hexadecimal commit SHA"),
			notExecutedCheck("t02-b", "Invalid source SHA prevents candidate verification"),
			notExecutedCheck("t02-c", "Invalid source SHA prevents candidate verification"),
		)
		return result
	}

	workspace, err := makeProbeWorkspace()
	if err != nil {
		result.Checks = append(result.Checks,
			failedCheck("t02-a", fmt.Sprintf("create isolated workspace: %v", err)),
			notExecutedCheck("t02-b", "Isolated workspace could not be created"),
			notExecutedCheck("t02-c", "Isolated workspace could not be created"),
		)
		return result
	}
	result.Workspace = workspace
	if !opts.keepTemp {
		defer os.RemoveAll(workspace)
	}

	binarySHA, err := sha256File(opts.redisServer)
	if err != nil {
		result.Checks = append(result.Checks,
			failedCheck("t02-a", fmt.Sprintf("hash redis-server executable: %v", err)),
			notExecutedCheck("t02-b", "Redis executable hash could not be collected"),
			notExecutedCheck("t02-c", "Redis executable hash could not be collected"),
		)
		return result
	}
	result.BinarySHA256 = binarySHA

	instance, err := newRedisInstance(opts.redisServer, workspace, "primary", "")
	if err != nil {
		result.Checks = append(result.Checks,
			failedCheck("t02-a", fmt.Sprintf("prepare Redis instance: %v", err)),
			notExecutedCheck("t02-b", "Redis instance could not be prepared"),
			notExecutedCheck("t02-c", "Redis instance could not be prepared"),
		)
		return result
	}
	defer instance.stop(true)

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	client, err := instance.start(ctx)
	cancel()
	if err != nil {
		result.Checks = append(result.Checks,
			failedCheck("t02-a", err.Error()),
			notExecutedCheck("t02-b", "Redis did not become ready"),
			notExecutedCheck("t02-c", "Redis did not become ready"),
		)
		return result
	}

	result.Checks = append(result.Checks, checkRedisBaseline(instance, client, opts))
	result.Checks = append(result.Checks, checkRedisCommands(instance, client))
	result.Checks = append(result.Checks, checkRedisPersistence(instance, opts, workspace))
	client.close()
	return result
}

func failedCheck(name, message string) checkResult {
	return checkResult{Name: name, Status: "failed", Error: message}
}

func notExecutedCheck(name, reason string) checkResult {
	return checkResult{Name: name, Status: "not_executed", Details: map[string]any{"reason": reason}}
}

func aggregateStatus(checks []checkResult) string {
	for _, check := range checks {
		if check.Status == "failed" {
			return "failed"
		}
	}
	for _, check := range checks {
		if check.Status != "passed" {
			return "not_executed"
		}
	}
	return "passed"
}

func makeProbeWorkspace() (string, error) {
	base := filepath.Join(os.TempDir(), "uvp redis probe 中文")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(base, "case with spaces-")
}

func newRedisInstance(binary, parentWorkspace, role, maxMemory string) (*redisInstance, error) {
	roleWorkspace, err := os.MkdirTemp(parentWorkspace, role+" with spaces 中文-")
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, err
	}
	password, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	return &redisInstance{
		binary:     binary,
		workspace:  roleWorkspace,
		role:       role,
		password:   password,
		address:    net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		port:       port,
		maxMemory:  maxMemory,
		configPath: filepath.Join(roleWorkspace, "redis.conf"),
	}, nil
}

func (instance *redisInstance) writeConfig() error {
	appendOnly := "yes"
	if instance.maxMemory != "" {
		appendOnly = "no"
	}
	config := strings.Join([]string{
		"bind 127.0.0.1",
		fmt.Sprintf("port %d", instance.port),
		"protected-mode yes",
		"daemonize no",
		"supervised no",
		"loglevel warning",
		"logfile \"\"",
		"save \"\"",
		"appendonly " + appendOnly,
		"appendfsync always",
		"aof-use-rdb-preamble yes",
		"appendfilename \"appendonly.aof\"",
		"appenddirname \"appendonlydir\"",
		"dbfilename \"dump.rdb\"",
		"maxmemory-policy noeviction",
		func() string {
			if instance.maxMemory == "" {
				return "maxmemory 0"
			}
			return "maxmemory " + instance.maxMemory
		}(),
		"requirepass " + instance.password,
		"dir " + quoteRedisConfigPath(instance.workspace),
	}, "\n") + "\n"
	return os.WriteFile(instance.configPath, []byte(config), 0o600)
}

func quoteRedisConfigPath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.ReplaceAll(path, `\`, `/`)
	path = strings.ReplaceAll(path, `"`, `\"`)
	return `"` + path + `"`
}

func (instance *redisInstance) start(ctx context.Context) (*redisClient, error) {
	instance.stateMu.Lock()
	if instance.cmd != nil {
		instance.stateMu.Unlock()
		return nil, errors.New("Redis instance is already running")
	}
	instance.stateMu.Unlock()
	if err := instance.writeConfig(); err != nil {
		return nil, fmt.Errorf("write Redis config: %w", err)
	}
	instance.output.Reset()
	command := exec.Command(instance.binary, instance.configPath)
	command.Dir = instance.workspace
	command.Stdout = &instance.output
	command.Stderr = &instance.output
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Redis: %w", err)
	}
	instance.stateMu.Lock()
	instance.cmd = command
	instance.done = make(chan struct{})
	instance.exitErr = nil
	done := instance.done
	instance.stateMu.Unlock()
	go func() {
		err := command.Wait()
		instance.stateMu.Lock()
		instance.exitErr = err
		close(done)
		instance.stateMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for Redis readiness: %w", ctx.Err())
		case <-instance.done:
			return nil, instance.startupError()
		default:
		}
		probeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		client, err := dialRedis(probeCtx, instance.address, instance.password, redisProbeDatabase)
		cancel()
		if err == nil {
			return client, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for Redis readiness: %w", ctx.Err())
		case <-instance.done:
			return nil, instance.startupError()
		case <-time.After(40 * time.Millisecond):
		}
	}
}

func (instance *redisInstance) startupError() error {
	instance.stateMu.Lock()
	defer instance.stateMu.Unlock()
	message := strings.TrimSpace(instance.output.String())
	if len(message) > 2048 {
		message = message[len(message)-2048:]
	}
	if message == "" {
		return fmt.Errorf("Redis exited before readiness: %v", instance.exitErr)
	}
	return fmt.Errorf("Redis exited before readiness: %v; output: %s", instance.exitErr, message)
}

func (instance *redisInstance) stop(force bool) error {
	instance.stateMu.Lock()
	command := instance.cmd
	done := instance.done
	instance.stateMu.Unlock()
	if command == nil || done == nil {
		return nil
	}
	select {
	case <-done:
		instance.clearProcess(command)
		return nil
	default:
	}
	if !force {
		return errors.New("graceful Redis stop is not implemented by this probe")
	}
	if err := command.Process.Kill(); err != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return fmt.Errorf("kill Redis: %w", err)
		}
	}
	select {
	case <-done:
		instance.clearProcess(command)
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("timed out waiting for Redis process to exit")
	}
}

func (instance *redisInstance) clearProcess(command *exec.Cmd) {
	instance.stateMu.Lock()
	if instance.cmd == command {
		instance.cmd = nil
		instance.done = nil
	}
	instance.stateMu.Unlock()
}

func (instance *redisInstance) restart(ctx context.Context) (*redisClient, error) {
	if err := instance.stop(true); err != nil {
		return nil, err
	}
	return instance.start(ctx)
}

func checkRedisBaseline(instance *redisInstance, client *redisClient, opts options) checkResult {
	details := map[string]any{
		"working_directory":    instance.workspace,
		"path_has_spaces":      strings.ContainsAny(instance.workspace, " \t"),
		"path_has_chinese":     strings.Contains(instance.workspace, "中文"),
		"password_transport":   "redis.conf (not command line)",
		"address":              instance.address,
		"loopback_only_target": true,
		"binary_sha256":        mustHashOrEmpty(opts.redisServer),
	}
	failures := make([]string, 0)
	info, err := redisInfo(client, "server")
	if err != nil {
		failures = append(failures, "INFO server: "+err.Error())
	} else {
		details["redis_version"] = info["redis_version"]
		details["redis_mode"] = info["redis_mode"]
		if opts.expectedVersion != "" && info["redis_version"] != opts.expectedVersion {
			failures = append(failures, fmt.Sprintf("Redis version %q does not match expected %q", info["redis_version"], opts.expectedVersion))
		}
	}

	if value, err := client.do(context.Background(), "PING"); err != nil {
		failures = append(failures, "authenticated PING: "+err.Error())
	} else if text, err := value.stringValue(); err != nil || text != "PONG" {
		failures = append(failures, fmt.Sprintf("authenticated PING response %q (%v)", text, err))
	} else {
		details["authenticated_ping"] = "PONG"
	}
	if value, err := client.do(context.Background(), "SELECT", strconv.Itoa(redisProbeDatabase)); err != nil {
		failures = append(failures, "SELECT: "+err.Error())
	} else if text, err := value.stringValue(); err != nil || text != "OK" {
		failures = append(failures, fmt.Sprintf("SELECT response %q (%v)", text, err))
	} else {
		details["selected_database"] = redisProbeDatabase
	}

	if unauthenticatedPing(instance.address) {
		details["unauthenticated_ping"] = "rejected"
	} else {
		failures = append(failures, "unauthenticated PING was not rejected")
	}
	wrongPassword := instance.password + "-wrong"
	wrongCtx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	_, wrongErr := dialRedis(wrongCtx, instance.address, wrongPassword, redisProbeDatabase)
	cancel()
	if wrongErr == nil {
		failures = append(failures, "wrong password was accepted")
	} else {
		details["wrong_password"] = "rejected"
	}

	bind, bindErr := redisConfigValue(client, "bind")
	protected, protectedErr := redisConfigValue(client, "protected-mode")
	if bindErr != nil {
		failures = append(failures, "CONFIG GET bind: "+bindErr.Error())
	} else {
		details["bind"] = bind
		if bind != "127.0.0.1" {
			failures = append(failures, fmt.Sprintf("Redis bind is %q, expected 127.0.0.1", bind))
		}
	}
	if protectedErr != nil {
		failures = append(failures, "CONFIG GET protected-mode: "+protectedErr.Error())
	} else {
		details["protected_mode"] = protected
		if protected != "yes" {
			failures = append(failures, fmt.Sprintf("Redis protected-mode is %q", protected))
		}
	}
	details["external_network"] = "not reachable through loopback-only bind; no external network mutation attempted"
	if len(failures) > 0 {
		return checkResult{Name: "t02-a", Status: "failed", Details: details, Error: strings.Join(failures, "; ")}
	}
	return checkResult{Name: "t02-a", Status: "passed", Details: details}
}

func unauthenticatedPing(address string) bool {
	connection, err := net.DialTimeout("tcp", address, probeTimeout)
	if err != nil {
		return false
	}
	defer connection.Close()
	value, err := doConn(connection, "PING")
	return err == nil && value.kind == respError && strings.Contains(strings.ToUpper(value.errText), "NOAUTH")
}

func checkRedisCommands(instance *redisInstance, client *redisClient) checkResult {
	details := map[string]any{"concurrent_consumers": 20}
	failures := make([]string, 0)
	token, err := randomToken(10)
	if err != nil {
		return failedCheck("t02-b", "create test key suffix: "+err.Error())
	}
	prefix := "__uvp_probe_" + token + ":"

	commandChecks := map[string]string{}
	setKey := prefix + "set"
	if _, err := client.do(context.Background(), "SET", setKey, "value"); err != nil {
		commandChecks["set_get_del_exists"] = "failed: " + err.Error()
		failures = append(failures, commandChecks["set_get_del_exists"])
	} else {
		value, getErr := client.do(context.Background(), "GET", setKey)
		text, textErr := value.stringValue()
		if getErr != nil || textErr != nil || text != "value" {
			message := fmt.Sprintf("GET returned %q (%v, %v)", text, getErr, textErr)
			commandChecks["set_get_del_exists"] = "failed: " + message
			failures = append(failures, message)
		} else if _, err := client.do(context.Background(), "DEL", setKey); err != nil {
			commandChecks["set_get_del_exists"] = "failed: DEL: " + err.Error()
			failures = append(failures, commandChecks["set_get_del_exists"])
		} else if exists, err := client.do(context.Background(), "EXISTS", setKey); err != nil {
			commandChecks["set_get_del_exists"] = "failed: EXISTS: " + err.Error()
			failures = append(failures, commandChecks["set_get_del_exists"])
		} else if count, err := exists.int64Value(); err != nil || count != 0 {
			message := fmt.Sprintf("EXISTS after DEL = %d (%v)", count, err)
			commandChecks["set_get_del_exists"] = "failed: " + message
			failures = append(failures, message)
		} else {
			commandChecks["set_get_del_exists"] = "passed"
		}
	}

	expireKey := prefix + "expire"
	if _, err := client.do(context.Background(), "SET", expireKey, "expire-value"); err != nil {
		commandChecks["expire_ttl"] = "failed: SET: " + err.Error()
		failures = append(failures, commandChecks["expire_ttl"])
	} else if _, err := client.do(context.Background(), "EXPIRE", expireKey, "5"); err != nil {
		commandChecks["expire_ttl"] = "failed: EXPIRE: " + err.Error()
		failures = append(failures, commandChecks["expire_ttl"])
	} else if ttl, err := client.do(context.Background(), "TTL", expireKey); err != nil {
		commandChecks["expire_ttl"] = "failed: TTL: " + err.Error()
		failures = append(failures, commandChecks["expire_ttl"])
	} else if seconds, err := ttl.int64Value(); err != nil || seconds < 1 || seconds > 5 {
		commandChecks["expire_ttl"] = fmt.Sprintf("failed: TTL=%d (%v)", seconds, err)
		failures = append(failures, commandChecks["expire_ttl"])
	} else {
		commandChecks["expire_ttl"] = "passed"
	}

	scanKey := prefix + "scan"
	if _, err := client.do(context.Background(), "SET", scanKey, "scan-value"); err != nil {
		commandChecks["scan"] = "failed: SET: " + err.Error()
		failures = append(failures, commandChecks["scan"])
	} else if scan, err := client.do(context.Background(), "SCAN", "0", "MATCH", prefix+"scan*", "COUNT", "100"); err != nil {
		commandChecks["scan"] = "failed: " + err.Error()
		failures = append(failures, commandChecks["scan"])
	} else if len(scan.items) != 2 || scan.items[0].kind == respNil || scan.items[1].kind == respNil {
		commandChecks["scan"] = "failed: malformed SCAN reply"
		failures = append(failures, commandChecks["scan"])
	} else if keys, err := scan.items[1].stringSlice(); err != nil || !containsString(keys, scanKey) {
		commandChecks["scan"] = fmt.Sprintf("failed: keys=%v err=%v", keys, err)
		failures = append(failures, commandChecks["scan"])
	} else {
		commandChecks["scan"] = "passed"
	}

	counterKey := prefix + "counter"
	_, _ = client.do(context.Background(), "DEL", counterKey)
	increment, incrementErr := client.do(context.Background(), "INCR", counterKey)
	decrement, decrementErr := client.do(context.Background(), "DECR", counterKey)
	incrementValue, incrementValueErr := increment.int64Value()
	decrementValue, decrementValueErr := decrement.int64Value()
	if incrementErr != nil || decrementErr != nil || incrementValueErr != nil || decrementValueErr != nil || incrementValue != 1 || decrementValue != 0 {
		commandChecks["incr_decr"] = fmt.Sprintf("failed: INCR=%d/%v DECR=%d/%v", incrementValue, incrementErr, decrementValue, decrementErr)
		failures = append(failures, commandChecks["incr_decr"])
	} else {
		commandChecks["incr_decr"] = "passed"
	}

	getDelKey := prefix + "getdel"
	if _, err := client.do(context.Background(), "SET", getDelKey, "one-time"); err != nil {
		commandChecks["getdel"] = "failed: SET: " + err.Error()
		failures = append(failures, commandChecks["getdel"])
	} else if value, err := client.do(context.Background(), "GETDEL", getDelKey); err != nil {
		commandChecks["getdel"] = "failed: " + err.Error()
		failures = append(failures, commandChecks["getdel"])
	} else if text, err := value.stringValue(); err != nil || text != "one-time" {
		commandChecks["getdel"] = fmt.Sprintf("failed: value=%q err=%v", text, err)
		failures = append(failures, commandChecks["getdel"])
	} else if value, err := client.do(context.Background(), "GETDEL", getDelKey); err != nil {
		commandChecks["getdel"] = "failed on missing key: " + err.Error()
		failures = append(failures, commandChecks["getdel"])
	} else if value.kind != respNil {
		commandChecks["getdel"] = "failed: second GETDEL did not return nil"
		failures = append(failures, commandChecks["getdel"])
	} else {
		commandChecks["getdel"] = "passed"
	}

	details["commands"] = commandChecks
	nativeKey := prefix + "native-once"
	native, nativeErr := concurrentConsume(instance.address, instance.password, nativeKey, false)
	details["native_getdel"] = native
	if nativeErr != nil {
		failures = append(failures, "native GETDEL: "+nativeErr.Error())
	}
	fallbackKey := prefix + "lua-once"
	fallback, fallbackErr := concurrentConsume(instance.address, instance.password, fallbackKey, true)
	details["lua_fallback"] = fallback
	if fallbackErr != nil {
		failures = append(failures, "Lua fallback: "+fallbackErr.Error())
	}

	cleanup := []string{setKey, expireKey, scanKey, counterKey, getDelKey, nativeKey, fallbackKey}
	_, _ = client.do(context.Background(), append([]string{"DEL"}, cleanup...)...)
	if len(failures) > 0 {
		return checkResult{Name: "t02-b", Status: "failed", Details: details, Error: strings.Join(failures, "; ")}
	}
	return checkResult{Name: "t02-b", Status: "passed", Details: details}
}

type consumeResult struct {
	Consumers int      `json:"consumers"`
	Successes int32    `json:"successes"`
	Missing   int32    `json:"missing"`
	Errors    []string `json:"errors,omitempty"`
	Atomic    bool     `json:"atomic"`
}

func concurrentConsume(address, password, key string, luaFallback bool) (consumeResult, error) {
	setupCtx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	setup, err := dialRedis(setupCtx, address, password, redisProbeDatabase)
	cancel()
	if err != nil {
		return consumeResult{Consumers: 20, Atomic: false}, err
	}
	defer setup.close()
	if _, err := setup.do(context.Background(), "SET", key, "one-time"); err != nil {
		return consumeResult{Consumers: 20, Atomic: false}, err
	}
	var successes int32
	var missing int32
	var errorMu sync.Mutex
	errorsList := make([]string, 0)
	var waitGroup sync.WaitGroup
	waitGroup.Add(20)
	for index := 0; index < 20; index++ {
		go func(worker int) {
			defer waitGroup.Done()
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			client, err := dialRedis(ctx, address, password, redisProbeDatabase)
			cancel()
			if err != nil {
				errorMu.Lock()
				if len(errorsList) < 5 {
					errorsList = append(errorsList, fmt.Sprintf("worker %d dial: %v", worker, err))
				}
				errorMu.Unlock()
				return
			}
			defer client.close()
			ctx, cancel = context.WithTimeout(context.Background(), probeTimeout)
			args := []string{"GETDEL", key}
			if luaFallback {
				args = []string{"EVAL", luaGetDelScript, "1", key}
			}
			value, err := client.do(ctx, args...)
			cancel()
			if err != nil {
				errorMu.Lock()
				if len(errorsList) < 5 {
					errorsList = append(errorsList, fmt.Sprintf("worker %d command: %v", worker, err))
				}
				errorMu.Unlock()
				return
			}
			if value.kind == respNil {
				atomic.AddInt32(&missing, 1)
				return
			}
			text, err := value.stringValue()
			if err != nil || text != "one-time" {
				errorMu.Lock()
				if len(errorsList) < 5 {
					errorsList = append(errorsList, fmt.Sprintf("worker %d value=%q err=%v", worker, text, err))
				}
				errorMu.Unlock()
				return
			}
			atomic.AddInt32(&successes, 1)
		}(index)
	}
	waitGroup.Wait()
	result := consumeResult{
		Consumers: 20,
		Successes: successes,
		Missing:   missing,
		Errors:    errorsList,
		Atomic:    successes == 1 && missing == 19 && len(errorsList) == 0,
	}
	if !result.Atomic {
		return result, fmt.Errorf("expected one success and nineteen missing, got successes=%d missing=%d errors=%v", successes, missing, errorsList)
	}
	return result, nil
}

func checkRedisPersistence(instance *redisInstance, opts options, workspace string) checkResult {
	details := map[string]any{
		"appendfsync":         "always",
		"maxmemory_policy":    "noeviction",
		"forced_kill_rounds":  persistenceRounds,
		"disk_full_injection": "not_executed",
		"disk_full_reason":    "OS disk-full injection is intentionally excluded because it can damage unrelated host data",
	}
	failures := make([]string, 0)
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	client, err := dialRedis(ctx, instance.address, instance.password, redisProbeDatabase)
	cancel()
	if err != nil {
		return failedCheck("t02-c", "dial persistence instance: "+err.Error())
	}
	defer func() { _ = client.close() }()

	for key, expected := range map[string]string{
		"appendonly":       "yes",
		"appendfsync":      "always",
		"maxmemory-policy": "noeviction",
	} {
		value, err := redisConfigValue(client, key)
		if err != nil {
			failures = append(failures, "CONFIG GET "+key+": "+err.Error())
			continue
		}
		details[key] = value
		if value != expected {
			failures = append(failures, fmt.Sprintf("CONFIG GET %s=%q, expected %q", key, value, expected))
		}
	}

	persistToken, err := randomToken(10)
	if err != nil {
		failures = append(failures, "create persistence key suffix: "+err.Error())
	} else {
		key := "__uvp_probe_persist_" + persistToken
		value := "confirmed-before-rewrite"
		recoveredRounds := 0
		if _, err := client.do(context.Background(), "SET", key, value); err != nil {
			failures = append(failures, "confirmed SET: "+err.Error())
		} else if err := triggerAOFRewrite(client); err != nil {
			failures = append(failures, "BGREWRITEAOF: "+err.Error())
		} else {
			details["aof_rewrite"] = "passed"
			for round := 1; round <= persistenceRounds; round++ {
				roundValue := fmt.Sprintf("confirmed-round-%02d", round)
				if _, err := client.do(context.Background(), "SET", key, roundValue); err != nil {
					failures = append(failures, fmt.Sprintf("round %d SET: %v", round, err))
					break
				}
				client.close()
				restartCtx, restartCancel := context.WithTimeout(context.Background(), startupTimeout)
				client, err = instance.restart(restartCtx)
				restartCancel()
				if err != nil {
					failures = append(failures, fmt.Sprintf("round %d restart: %v", round, err))
					break
				}
				readCtx, readCancel := context.WithTimeout(context.Background(), probeTimeout)
				read, readErr := client.do(readCtx, "GET", key)
				readCancel()
				got, textErr := read.stringValue()
				if readErr != nil || textErr != nil || got != roundValue {
					failures = append(failures, fmt.Sprintf("round %d recovered value=%q command_err=%v value_err=%v", round, got, readErr, textErr))
					break
				}
				recoveredRounds++
			}
			details["recovered_rounds"] = recoveredRounds
			_, _ = client.do(context.Background(), "DEL", key)
		}
	}

	oom, oomResult, oomErr := runOOMCheck(opts.redisServer, workspace)
	if oom != nil {
		_ = oom.stop(true)
	}
	details["noeviction_oom"] = oomResult
	if oomErr != nil {
		failures = append(failures, "noeviction OOM: "+oomErr.Error())
	}

	licenseResult := verifyLicense(opts.licenseFile)
	details["license_material"] = licenseResult
	if licenseResult["status"] == "failed" {
		failures = append(failures, "license material: "+licenseResult["reason"].(string))
	}

	if len(failures) > 0 {
		return checkResult{Name: "t02-c", Status: "failed", Details: details, Error: strings.Join(failures, "; ")}
	}
	if details["disk_full_injection"] == "not_executed" || licenseResult["status"] != "passed" {
		return checkResult{Name: "t02-c", Status: "not_executed", Details: details}
	}
	return checkResult{Name: "t02-c", Status: "passed", Details: details}
}

func triggerAOFRewrite(client *redisClient) error {
	before, err := redisInfo(client, "persistence")
	if err != nil {
		return err
	}
	beforeCount := parseInfoInt(before, "aof_rewrites")
	if _, err := client.do(context.Background(), "BGREWRITEAOF"); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		info, err := redisInfo(client, "persistence")
		if err != nil {
			return err
		}
		if info["aof_last_bgrewrite_status"] == "err" {
			return errors.New("aof_last_bgrewrite_status=err")
		}
		if parseInfoInt(info, "aof_rewrite_in_progress") == 0 &&
			info["aof_last_bgrewrite_status"] == "ok" &&
			parseInfoInt(info, "aof_rewrites") > beforeCount {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timed out waiting for BGREWRITEAOF")
}

func runOOMCheck(binary, workspace string) (*redisInstance, map[string]any, error) {
	instance, err := newRedisInstance(binary, workspace, "oom", "4mb")
	if err != nil {
		return nil, map[string]any{"status": "failed", "reason": err.Error()}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	client, err := instance.start(ctx)
	cancel()
	if err != nil {
		return instance, map[string]any{"status": "failed", "reason": err.Error()}, err
	}
	defer client.close()
	policy, policyErr := redisConfigValue(client, "maxmemory-policy")
	result := map[string]any{
		"maxmemory": "4mb",
		"policy":    policy,
		"attempts":  0,
		"status":    "failed",
	}
	if policyErr != nil {
		result["reason"] = policyErr.Error()
		return instance, result, policyErr
	}
	if policy != "noeviction" {
		result["reason"] = "policy is " + policy
		return instance, result, errors.New("OOM check policy is not noeviction")
	}
	payload := strings.Repeat("x", 128*1024)
	for attempt := 1; attempt <= 100; attempt++ {
		result["attempts"] = attempt
		_, err := client.do(context.Background(), "SET", fmt.Sprintf("__uvp_probe_oom_%03d", attempt), payload)
		if err == nil {
			continue
		}
		result["observed_error"] = err.Error()
		if strings.Contains(strings.ToUpper(err.Error()), "OOM") {
			result["status"] = "passed"
			return instance, result, nil
		}
		result["reason"] = err.Error()
		return instance, result, err
	}
	result["reason"] = "100 writes did not observe an OOM rejection"
	return instance, result, errors.New("noeviction OOM was not observed")
}

func verifyLicense(path string) map[string]any {
	if path == "" {
		return map[string]any{"status": "not_executed", "reason": "-license-file was not supplied"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{"status": "failed", "reason": err.Error()}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"status": "failed", "reason": "license file is empty"}
	}
	hash := sha256.Sum256(data)
	return map[string]any{
		"status": "passed",
		"path":   path,
		"sha256": hex.EncodeToString(hash[:]),
		"bytes":  len(data),
	}
}

func redisInfo(client *redisClient, section string) (map[string]string, error) {
	value, err := client.do(context.Background(), "INFO", section)
	if err != nil {
		return nil, err
	}
	text, err := value.stringValue()
	if err != nil {
		return nil, err
	}
	info := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		info[key] = value
	}
	return info, nil
}

func parseInfoInt(info map[string]string, key string) int64 {
	value, _ := strconv.ParseInt(info[key], 10, 64)
	return value
}

func redisConfigValue(client *redisClient, key string) (string, error) {
	value, err := client.do(context.Background(), "CONFIG", "GET", key)
	if err != nil {
		return "", err
	}
	if value.kind != respArray || len(value.items) != 2 {
		return "", fmt.Errorf("CONFIG GET %s returned malformed response", key)
	}
	return value.items[1].stringValue()
}

func mustHashOrEmpty(path string) string {
	hash, err := sha256File(path)
	if err != nil {
		return ""
	}
	return hash
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func randomToken(bytesLength int) (string, error) {
	buffer := make([]byte, bytesLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func isSHA1(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
