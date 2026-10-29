// Command uvp-updater is the small privileged helper used by service
// upgrades. It owns the local socket and persistent task state; deployment
// adapters are intentionally registered by the packaged installation.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"uvplatform.com/uvp-gb28181/app/utils/ymlconfig"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

const sharedGateGroupID = 10001

type serveOptions struct {
	SocketPath       string
	StatePath        string
	RegistrationPath string
}

type updaterServer interface {
	Serve(context.Context) error
}

// backgroundRunner is a long-lived loop that must start and stop with the
// helper. Today only the version probe uses it.
type backgroundRunner interface {
	Run(context.Context) error
}

type serveRuntime struct {
	state    string
	server   updaterServer
	workflow *upgrade.WorkflowExecutor
	control  *upgrade.ControlService
	// watcher performs the periodic release check. It only reads the release
	// source: it never downloads, stages, or applies anything.
	watcher backgroundRunner
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "register" {
		runRegistration(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] != "-h" && os.Args[1] != "--help" && os.Args[1] != "serve" {
		runCLI(os.Args[1:])
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runServe(ctx, os.Args[1:], os.Getenv); err != nil && !errors.Is(err, flag.ErrHelp) && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runServe(ctx context.Context, args []string, getenv func(string) string) error {
	options, err := parseServeOptions(args, getenv)
	if err != nil {
		return err
	}
	assembly, err := buildServeRuntime(options)
	if err != nil {
		return err
	}
	// Recover a task that had already crossed the durable stop boundary before
	// before the local control socket starts accepting requests.
	if assembly.workflow != nil {
		if err := assembly.workflow.Recover(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "upgrade recovery:", err)
		}
	}
	if assembly.control != nil {
		if err := assembly.control.Start(ctx); err != nil {
			return fmt.Errorf("start upgrade control: %w", err)
		}
		defer assembly.control.Close()
	}
	return runServeLifecycle(ctx, assembly.server, assembly.watcher)
}

func parseServeOptions(args []string, getenv func(string) string) (serveOptions, error) {
	serveFlags := flag.NewFlagSet("uvp-updater serve", flag.ContinueOnError)
	socket := serveFlags.String("socket", "/run/uvp/updater.sock", "Unix socket path")
	state := serveFlags.String("state", "", "persistent updater state directory")
	registration := serveFlags.String("registration", "", "administrator-owned deployment registration JSON")
	serveArgs := args
	if len(serveArgs) > 0 && serveArgs[0] == "serve" {
		serveArgs = serveArgs[1:]
	}
	if err := serveFlags.Parse(serveArgs); err != nil {
		return serveOptions{}, err
	}
	registrationPath := strings.TrimSpace(*registration)
	if registrationPath == "" && getenv != nil {
		registrationPath = strings.TrimSpace(getenv("UVP_UPDATER_REGISTRATION"))
	}
	return serveOptions{
		SocketPath:       strings.TrimSpace(*socket),
		StatePath:        strings.TrimSpace(*state),
		RegistrationPath: registrationPath,
	}, nil
}

func buildServeRuntime(options serveOptions) (*serveRuntime, error) {
	socketPath := strings.TrimSpace(options.SocketPath)
	if socketPath == "" {
		return nil, errors.New("updater socket path is required")
	}
	var registration *upgrade.Registration
	var adapter upgrade.DeploymentAdapter
	var err error
	socketGroupID := -1
	if options.RegistrationPath != "" {
		loaded, loadErr := loadRegisteredRegistration(filepath.Clean(options.RegistrationPath))
		if loadErr != nil {
			return nil, loadErr
		}
		// Registrations written before the release source became built-in carry
		// no online entry point. Fill it here so an existing installation starts
		// checking for updates after a plain helper restart.
		loaded = loaded.WithOfficialDefaults()
		registration = &loaded
		adapter, err = upgrade.NewRegisteredAdapter(loaded)
		if err != nil {
			return nil, err
		}
	}
	state, err := resolveServeState(options.StatePath, adapter)
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateDirectory(state, "updater state", []uint32{uint32(os.Geteuid()), 0}); err != nil {
		return nil, err
	}
	if err := ensureSocketParent(socketPath); err != nil {
		return nil, err
	}
	store, err := upgrade.OpenTaskStore(state)
	if err != nil {
		return nil, err
	}
	assembly := &serveRuntime{state: state}
	var executor upgrade.ApplyExecutor
	if adapter != nil && registration != nil {
		layout, err := registration.Layout()
		if err != nil {
			return nil, err
		}
		imports, err := upgrade.OpenImportStore(state)
		if err != nil {
			return nil, err
		}
		businessUID, businessGID := registeredBusinessIdentity(*registration)
		if len(registration.AllowedUIDs) > 0 && !containsUID(registration.AllowedUIDs, businessUID) {
			return nil, fmt.Errorf("registered business UID %d is not an authorized Unix peer", businessUID)
		}
		gate, err := provisionSharedGate(layout, businessGID)
		if err != nil {
			return nil, err
		}
		events, err := upgrade.OpenFileEventLog(filepath.Join(state, "events.jsonl"))
		if err != nil {
			return nil, err
		}
		verify, err := upgrade.NewRegisteredVerifier(*registration, state)
		if err != nil {
			return nil, err
		}
		preflight := upgrade.NewRegisteredPreflight(*registration)
		workflow := &upgrade.WorkflowExecutor{
			Store:     store,
			Adapter:   adapter,
			Gate:      gate,
			Events:    events,
			Verify:    verify,
			Preflight: preflight,
			Prepare:   upgrade.RegisteredCapacityPreflight(layout, registration.Deployment, upgrade.OSCommandRunner{}, registration.DatabaseBackup),
			Cleanup: func(ctx context.Context, _ upgrade.TaskRecord) error {
				return upgrade.PruneCompletedArtifacts(ctx, store, layout, upgrade.RetainedReleaseVersions)
			},
		}
		target, err := registeredTarget(adapter)
		if err != nil {
			return nil, err
		}
		keys, err := upgrade.RegisteredReleasePublicKeys(*registration)
		if err != nil {
			return nil, err
		}
		inbox, err := registration.StagingInbox(state)
		if err != nil {
			return nil, err
		}
		if err := ensureBusinessInbox(inbox, businessUID, businessGID); err != nil {
			return nil, err
		}
		socketGroupID = int(businessGID)
		verifiedRoot := filepath.Join(state, "verified")
		if err := ensurePrivateDirectory(verifiedRoot, "verified updater staging", []uint32{uint32(os.Geteuid()), 0}); err != nil {
			return nil, err
		}
		watchStore, err := upgrade.OpenUpdateWatchStore(filepath.Join(state, "update-check.json"))
		if err != nil {
			return nil, err
		}
		assembly.control = &upgrade.ControlService{
			Store:         store,
			ImportStore:   imports,
			EventLog:      events,
			UpdateWatch:   watchStore,
			InboxRoot:     inbox,
			StagingRoot:   verifiedRoot,
			SequencePath:  filepath.Join(state, "sequences.json"),
			Target:        target,
			TrustedKeys:   keys,
			ReleaseSource: registration.ReleaseSource,
			Preflight:     preflight,
		}
		assembly.watcher = &upgrade.UpdateWatcher{
			Service: assembly.control,
			Store:   watchStore,
			// The business process writes this switch when an administrator
			// turns scheduled checks off; the helper only reads it.
			PreferencePath: filepath.Join(state, upgrade.AutoCheckPreferenceFilename),
		}
		executor = workflow
		assembly.workflow = workflow
	}
	server := &upgrade.UnixServer{SocketPath: filepath.Clean(socketPath), Store: store, Executor: executor, Control: assembly.control, SocketGroupID: socketGroupID, SocketGroupSet: socketGroupID >= 0}
	if registration != nil {
		server.PeerAuthorizer = upgrade.NewUnixPeerAuthorizer(registration.AllowedUIDs)
	}
	if assembly.workflow != nil {
		server.Gate, server.Events = assembly.workflow.Gate, assembly.workflow.Events
	}
	assembly.server = server
	return assembly, nil
}

func containsUID(allowed []uint32, owner uint32) bool {
	for _, candidate := range allowed {
		if candidate == owner {
			return true
		}
	}
	return false
}

func resolveServeState(statePath string, adapter upgrade.DeploymentAdapter) (string, error) {
	configured := strings.TrimSpace(statePath)
	if adapter == nil {
		if configured == "" {
			configured = defaultUpdaterState()
		}
		if !filepath.IsAbs(configured) {
			return "", errors.New("updater state path must be absolute")
		}
		return filepath.Clean(configured), nil
	}
	state, err := registeredState(adapter)
	if err != nil {
		return "", err
	}
	if configured == "" {
		return state, nil
	}
	if !filepath.IsAbs(configured) {
		return "", errors.New("updater state path must be absolute")
	}
	configured = filepath.Clean(configured)
	if configured != state {
		return "", fmt.Errorf("explicit updater state %q does not match registered state %q", configured, state)
	}
	return state, nil
}

func loadRegisteredRegistration(registrationPath string) (upgrade.Registration, error) {
	return upgrade.ReadRegistration(registrationPath)
}

func loadRegisteredAdapter(registrationPath string) (upgrade.DeploymentAdapter, error) {
	registration, err := loadRegisteredRegistration(registrationPath)
	if err != nil {
		return nil, err
	}
	return upgrade.NewRegisteredAdapter(registration)
}

func registeredState(adapter upgrade.DeploymentAdapter) (string, error) {
	layout, err := adapterLayout(adapter)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(layout.Root) {
		return "", errors.New("registered deployment root must be absolute")
	}
	state := layout.State
	if state == "" {
		state = filepath.Join(layout.Root, "updater-state")
	}
	if !filepath.IsAbs(state) {
		return "", errors.New("registered state path must be absolute")
	}
	return filepath.Clean(state), nil
}

func registeredTarget(adapter upgrade.DeploymentAdapter) (upgrade.Target, error) {
	deployment, err := adapterDeployment(adapter)
	if err != nil {
		return upgrade.Target{}, err
	}
	return upgrade.Target{Deployment: deployment, OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: upgrade.UpdaterProtocolVersion}, nil
}

func runServeLifecycle(ctx context.Context, server updaterServer, watcher backgroundRunner) error {
	if server == nil {
		return errors.New("updater server is required")
	}
	if watcher == nil {
		return server.Serve(ctx)
	}
	watchCtx, cancelWatch := context.WithCancel(ctx)
	watchDone := make(chan error, 1)
	go func() { watchDone <- watcher.Run(watchCtx) }()
	serveErr := server.Serve(ctx)
	cancelWatch()
	watchErr := <-watchDone
	if serveErr != nil {
		return serveErr
	}
	if watchErr != nil && !errors.Is(watchErr, context.Canceled) {
		return fmt.Errorf("periodic update check stopped: %w", watchErr)
	}
	return nil
}

func runRegistration(args []string) {
	fs := flag.NewFlagSet("uvp-updater register", flag.ExitOnError)
	configDir := fs.String("config-dir", "", "application config directory")
	registrationPath := fs.String("registration", "", "administrator-owned registration JSON")
	deployment := fs.String("deployment", "standalone", "standalone, systemd, or compose")
	root := fs.String("root", "", "registered deployment root")
	state := fs.String("state", "", "updater state directory")
	control := fs.String("control", "", "standalone control script")
	unit := fs.String("unit", "", "systemd unit")
	projectDir := fs.String("project-dir", "", "compose project directory")
	project := fs.String("project", "", "compose project name")
	migrationService := fs.String("migration-service", "backend", "compose service that runs -migrate-up")
	composeFile := fs.String("compose-file", "", "registered compose file")
	sqliteDatabase := fs.String("sqlite-database", "", "administrator-registered SQLite database path for rollback snapshots")
	readinessURL := fs.String("readiness-url", "", "fixed loopback candidate readiness endpoint")
	passwordEnv := fs.String("password-env", "UVP_UPGRADE_DB_PASSWORD", "environment variable containing the external database backup password")
	externalBackupMaxBytes := fs.String("external-backup-max-bytes", "", "maximum external database backup size in bytes (default 8589934592)")
	allowUID := fs.Uint("allow-uid", defaultLocalUID(), "local Unix socket UID allowed to control this helper")
	businessUID := fs.Uint("business-uid", defaultLocalUID(), "business service UID allowed to write the updater inbox")
	businessGID := fs.Uint("business-gid", defaultLocalGID(), "business service GID used for the updater socket and inbox")
	stagingRoot := fs.String("staging-root", "", "business-writable updater inbox directory")
	_ = fs.Parse(args)
	if *configDir == "" || *registrationPath == "" || *root == "" {
		fatalRegistration("config-dir, registration and root are required")
	}
	maxBackupBytes, err := registrationExternalBackupMaxBytes(*externalBackupMaxBytes)
	if err != nil {
		fatalRegistration(err.Error())
	}
	config, err := ymlconfig.LoadYamlFactory(*configDir)
	if err != nil {
		fatalRegistration(fmt.Sprintf("load application config: %v", err))
	}
	if strings.TrimSpace(*stagingRoot) == "" {
		for _, key := range []string{"upgrade.inbox", "upgrade.inbox_root"} {
			if value := strings.TrimSpace(config.GetString(key)); value != "" {
				*stagingRoot = value
				break
			}
		}
	}
	if strings.TrimSpace(*stagingRoot) == "" {
		*stagingRoot = strings.TrimSpace(os.Getenv("UVP_UPGRADE_INBOX"))
	}
	databaseBackup, enabled, err := upgrade.ExternalBackupRegistrationFromConfig(config, *passwordEnv, maxBackupBytes)
	if err != nil {
		fatalRegistration(err.Error())
	}
	if *allowUID > uint(^uint32(0)) {
		fatalRegistration("allow-uid is outside the supported UID range")
	}
	if *businessUID > uint(^uint32(0)) {
		fatalRegistration("business-uid is outside the supported UID range")
	}
	if *businessGID > uint(^uint32(0)) {
		fatalRegistration("business-gid is outside the supported GID range")
	}
	if strings.TrimSpace(*stagingRoot) != "" {
		if !filepath.IsAbs(*stagingRoot) {
			fatalRegistration("staging-root must be an absolute path")
		}
		*stagingRoot = filepath.Clean(*stagingRoot)
	}
	releaseSource := strings.TrimSpace(config.GetString("upgrade.release_source"))
	if releaseSource == "" {
		releaseSource = strings.TrimSpace(config.GetString("upgrade.releaseSource"))
	}
	registration := upgrade.Registration{
		Deployment:     *deployment,
		DatabaseBackup: nil,
		AllowedUIDs:    []uint32{uint32(*allowUID)},
		BusinessUID:    uint32(*businessUID),
		BusinessGID:    uint32(*businessGID),
		StagingRoot:    strings.TrimSpace(*stagingRoot),
		ReleaseSource:  releaseSource,
	}
	registration.TrustedKeys = registrationTrustedKeys(config.Get("upgrade.trusted_keys"))
	// The release source and trust root are built into the binary; configuration
	// is only an override for self-hosted channels, never a requirement.
	registration = registration.WithOfficialDefaults()
	probeURL := *readinessURL
	if probeURL == "" && *deployment != "compose" {
		probeURL, err = registrationReadinessURL(config.GetString("httpserver.port"))
		if err != nil {
			fatalRegistration(err.Error())
		}
	}
	if enabled {
		registration.DatabaseBackup = databaseBackup
	}
	sqlitePath, err := registrationSQLiteDatabase(config, *deployment, *root, *sqliteDatabase)
	if err != nil {
		fatalRegistration(err.Error())
	}
	switch *deployment {
	case "standalone", "systemd":
		registration.Standalone = upgrade.RegisteredLayout{Root: *root, State: *state, Control: *control, ReadinessURL: probeURL, SQLiteDatabase: sqlitePath}
		registration.SystemdUnit = *unit
	case "compose":
		registration.Compose = upgrade.RegisteredLayout{Root: *root, State: *state, ProjectDir: *projectDir, Project: *project, MigrationService: *migrationService, ReadinessURL: probeURL, SQLiteDatabase: sqlitePath}
		registration.ComposeFile = *composeFile
	default:
		fatalRegistration(fmt.Sprintf("unsupported deployment %q", *deployment))
	}
	if err := writeRegistration(*registrationPath, registration); err != nil {
		fatalRegistration(err.Error())
	}
}

func registrationSQLiteDatabase(config upgrade.ConfigLookup, deployment, root, explicit string) (string, error) {
	if path := strings.TrimSpace(explicit); path != "" {
		if !filepath.IsAbs(path) {
			return "", errors.New("registered sqlite database path must be absolute")
		}
		return filepath.Clean(path), nil
	}
	if upgrade.NormalizeDatabaseDialect(config.GetString("gormv2.usedbtype")) != "sqlite" {
		return "", nil
	}
	if deployment == "compose" {
		return "", errors.New("sqlite compose deployment requires --sqlite-database with the host database path")
	}
	path := strings.TrimSpace(config.GetString("gormv2.sqlite.write.database"))
	if path == "" {
		return "", errors.New("SQLite database path is not configured (gormv2.sqlite.write.database)")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("registered deployment root must be absolute")
	}
	return filepath.Clean(path), nil
}

func registrationExternalBackupMaxBytes(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return upgrade.DefaultExternalBackupMaxBytes, nil
	}
	maxBytes, err := strconv.ParseUint(value, 10, 64)
	if err != nil || maxBytes == 0 {
		return 0, fmt.Errorf("external-backup-max-bytes must be a positive integer")
	}
	return maxBytes, nil
}

func writeRegistration(path string, registration upgrade.Registration) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("registration path must be absolute")
	}
	if err := defaultRegistrationState(&registration); err != nil {
		return err
	}
	data, err := json.Marshal(registration)
	if err != nil {
		return fmt.Errorf("encode registration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create registration directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write registration: %w", err)
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("protect registration: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install registration: %w", err)
	}
	return nil
}

func defaultRegistrationState(registration *upgrade.Registration) error {
	if registration == nil {
		return errors.New("registration is required")
	}
	var layout *upgrade.RegisteredLayout
	switch registration.Deployment {
	case "standalone", "systemd":
		layout = &registration.Standalone
	case "compose":
		layout = &registration.Compose
	default:
		return fmt.Errorf("unsupported deployment %q", registration.Deployment)
	}
	if !filepath.IsAbs(layout.Root) {
		return errors.New("registered deployment root must be absolute")
	}
	if layout.State == "" {
		layout.State = filepath.Join(layout.Root, "updater-state")
	}
	if !filepath.IsAbs(layout.State) {
		return errors.New("registered state path must be absolute")
	}
	layout.State = filepath.Clean(layout.State)
	return nil
}

func fatalRegistration(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func runCLI(args []string) {
	fs := flag.NewFlagSet("uvp-updater", flag.ExitOnError)
	socket := fs.String("socket", "/run/uvp/updater.sock", "Unix socket path")
	taskID := fs.String("task", "", "task ID")
	digest := fs.String("digest", "", "target SHA-256 digest")
	_ = fs.Parse(args[1:])
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: uvp-updater status|apply|cancel|rollback [--socket path --task id]")
		os.Exit(2)
	}
	action := map[string]upgrade.Action{"status": upgrade.ActionStatus, "apply": upgrade.ActionApply, "cancel": upgrade.ActionCancel, "rollback": upgrade.ActionRollback}[args[0]]
	if action == "" {
		fmt.Fprintln(os.Stderr, "不支持的助手命令")
		os.Exit(2)
	}
	request := upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: action, TaskID: *taskID, TargetDigest: *digest, Confirmation: action == upgrade.ActionApply}
	response, err := upgrade.CallUnix(context.Background(), *socket, request)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(data))
	if !response.OK {
		os.Exit(1)
	}
}

func registrationTrustedKeys(value interface{}) map[string]string {
	keys := map[string]string{}
	add := func(id, encoded string) {
		var raw []byte
		var err error
		if len(encoded) == 64 {
			raw, err = hex.DecodeString(encoded)
		} else {
			raw, err = base64.StdEncoding.DecodeString(encoded)
		}
		if err == nil && len(raw) == ed25519.PublicKeySize && strings.TrimSpace(id) != "" {
			keys[id] = base64.StdEncoding.EncodeToString(raw)
		}
	}
	switch values := value.(type) {
	case map[string]interface{}:
		for id, encoded := range values {
			if text, ok := encoded.(string); ok {
				add(id, text)
			}
		}
	case map[string]string:
		for id, text := range values {
			add(id, text)
		}
	case []interface{}:
		for _, value := range values {
			if text, ok := value.(string); ok {
				parts := strings.SplitN(text, "=", 2)
				if len(parts) == 2 {
					add(parts[0], parts[1])
				}
			}
		}
	case []string:
		for _, text := range values {
			parts := strings.SplitN(text, "=", 2)
			if len(parts) == 2 {
				add(parts[0], parts[1])
			}
		}
	}
	return keys
}

func registrationReadinessURL(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid backend HTTP listen address: %w", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("invalid backend HTTP listen port")
	}
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsUnspecified() && !ip.IsLoopback()) {
			return "", fmt.Errorf("backend listen address requires an explicitly registered loopback readiness endpoint")
		}
	}
	return "http://" + net.JoinHostPort("127.0.0.1", port) + upgrade.RuntimeIdentityPath, nil
}
