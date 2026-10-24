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
	"time"

	"uvplatform.com/uvp-gb28181/app/utils/ymlconfig"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

const defaultUpdaterState = "/var/lib/uvp/updater-state"
const sharedGateGroupID = 10001

type serveOptions struct {
	SocketPath       string
	StatePath        string
	RegistrationPath string
}

type updaterServer interface {
	Serve(context.Context) error
}

type updaterScheduler interface {
	Run(context.Context) error
}

type serveRuntime struct {
	state      string
	server     updaterServer
	scheduler  updaterScheduler
	background []updaterScheduler
	workflow   *upgrade.WorkflowExecutor
	control    *upgrade.ControlService
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
	// allowing the scheduler to consider a prepared policy task.
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
	scheduler := assembly.scheduler
	if len(assembly.background) > 0 {
		all := append([]updaterScheduler{}, assembly.background...)
		if scheduler != nil {
			all = append(all, scheduler)
		}
		scheduler = &schedulerGroup{items: all}
	}
	return runServeLifecycle(ctx, assembly.server, scheduler)
}

// schedulerGroup keeps online checking/downloading and maintenance-window
// application independent. Both remain helper-owned, so the business process
// may stop or disappear while a release is being fetched or applied.
type schedulerGroup struct {
	items []updaterScheduler
}

func (g *schedulerGroup) Run(ctx context.Context) error {
	if g == nil || len(g.items) == 0 {
		return nil
	}
	groupCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, len(g.items))
	for _, item := range g.items {
		if item == nil {
			results <- nil
			continue
		}
		go func(scheduler updaterScheduler) { results <- scheduler.Run(groupCtx) }(item)
	}
	var firstErr error
	for range g.items {
		err := <-results
		if firstErr == nil && err != nil && !errors.Is(err, context.Canceled) {
			firstErr = err
		}
		cancel()
	}
	return firstErr
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
		policies, err := upgrade.OpenPolicyStore(filepath.Join(state, "policy.json"))
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
			Store:       store,
			Adapter:     adapter,
			Gate:        gate,
			PolicyStore: policies,
			Events:      events,
			Verify:      verify,
			Preflight:   preflight,
			Prepare:     upgrade.RegisteredCapacityPreflight(layout, registration.Deployment, upgrade.OSCommandRunner{}, registration.DatabaseBackup),
			Cleanup: func(ctx context.Context, _ upgrade.TaskRecord) error {
				policy, err := policies.Load()
				if err != nil {
					return fmt.Errorf("load retention policy: %w", err)
				}
				return upgrade.PruneCompletedArtifacts(ctx, store, layout, policy.RetainVersions)
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
		assembly.control = &upgrade.ControlService{
			Store:         store,
			ImportStore:   imports,
			PolicyStore:   policies,
			EventLog:      events,
			InboxRoot:     inbox,
			StagingRoot:   verifiedRoot,
			SequencePath:  filepath.Join(state, "sequences.json"),
			Target:        target,
			TrustedKeys:   keys,
			ReleaseSource: registration.ReleaseSource,
			Preflight:     preflight,
		}
		executor = workflow
		assembly.workflow = workflow
		assembly.scheduler = &upgrade.HelperScheduler{
			Store:       store,
			PolicyStore: policies,
			Target:      target,
			Preflight:   preflight,
			Apply:       workflow.Apply,
			Events:      events,
		}
		assembly.background = []updaterScheduler{&upgrade.AutoScheduler{
			Store:       store,
			PolicyStore: policies,
			Target:      target,
			Check:       assembly.control.CheckManifest,
			CurrentVersionWithContext: func(ctx context.Context) (string, error) {
				snapshot, err := preflight(ctx)
				if err != nil {
					return "", err
				}
				return snapshot.CurrentVersion, nil
			},
			Prepare: func(ctx context.Context, manifest upgrade.Manifest, artifact upgrade.Artifact) (upgrade.StagedArtifact, error) {
				return assembly.control.PrepareOnlineRelease(ctx, manifest, artifact)
			},
			Preflight: preflight,
			Events:    events,
		}}
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

func provisionSharedGate(layout upgrade.RegisteredLayout, groupIDs ...uint32) (*upgrade.GateStore, error) {
	if err := layout.Validate(); err != nil {
		return nil, err
	}
	groupID := uint32(sharedGateGroupID)
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	path := filepath.Join(layout.Root, "updater-gate", "gate.json")
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(parent, 0750); err != nil {
			return nil, fmt.Errorf("create shared updater gate directory: %w", err)
		}
		if err := configureSharedGateOwnership(parent, groupID); err != nil {
			return nil, err
		}
		info, err = os.Lstat(parent)
	}
	if err != nil {
		return nil, fmt.Errorf("stat shared updater gate directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("shared updater gate parent is not a real directory")
	}
	if err := upgrade.VerifySharedGateParent(info); err != nil {
		return nil, err
	}
	gate, err := upgrade.OpenSharedGateStore(path)
	if err != nil {
		return nil, err
	}
	if err := initializeSharedGate(path, groupID); err != nil {
		return nil, err
	}
	return gate, nil
}

// ensurePrivateDirectory provisions helper-owned state and verified roots. A
// pre-existing directory is never made trusted by merely chmod'ing away a
// write bit: its owner and every replaceable parent are checked first.
func ensurePrivateDirectory(path, label string, allowedOwners []uint32) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", label)
	}
	path = filepath.Clean(path)
	if err := ensureTrustedAncestors(filepath.Dir(path), allowedOwners); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("validate %s parent: %w", label, err)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create %s directory: %w", label, err)
	}
	if err := ensureTrustedAncestors(filepath.Dir(path), allowedOwners); err != nil {
		return fmt.Errorf("validate %s parent: %w", label, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat %s directory: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s path is not a real directory", label)
	}
	owner, ok := fileOwnerUID(info)
	if !ok || !containsUID(allowedOwners, owner) {
		return fmt.Errorf("%s directory owner UID %d is not trusted", label, owner)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("%s directory must not be group/world writable", label)
	}
	if info.Mode().Perm() != 0700 {
		if err := os.Chmod(path, 0700); err != nil {
			return fmt.Errorf("protect %s directory: %w", label, err)
		}
	}
	return nil
}

// ensureBusinessInbox is the only relaxed permission root in the helper. The
// business process may create one opaque upload token here, while verified
// artifacts, task state, rollback data, and event logs remain helper-private.
// The parent must still be administrator-owned and non-writable so the
// business process cannot replace the inbox directory itself.
func ensureBusinessInbox(path string, businessUID, businessGID uint32) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("updater inbox path must be absolute")
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(path, 0770); err != nil {
		return fmt.Errorf("create updater inbox directory: %w", err)
	}
	if err := ensureTrustedAncestors(filepath.Dir(path), []uint32{uint32(os.Geteuid()), 0}); err != nil {
		return fmt.Errorf("validate updater inbox parent: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat updater inbox directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("updater inbox path is not a real directory")
	}
	owner, ownerOK := fileOwnerUID(info)
	if !ownerOK || (owner != 0 && owner != uint32(os.Geteuid())) {
		return fmt.Errorf("updater inbox owner UID %d is not trusted", owner)
	}
	group, groupOK := fileGroupID(info)
	if !groupOK {
		return errors.New("updater inbox group is unavailable")
	}
	if os.Geteuid() == 0 && group != businessGID {
		if err := os.Chown(path, 0, int(businessGID)); err != nil {
			return fmt.Errorf("set updater inbox group: %w", err)
		}
		info, err = os.Lstat(path)
		if err != nil {
			return fmt.Errorf("restat updater inbox directory: %w", err)
		}
		group, groupOK = fileGroupID(info)
	}
	if !groupOK || group != businessGID {
		return fmt.Errorf("updater inbox group %d does not match registered business GID %d", group, businessGID)
	}
	if info.Mode().Perm()&0007 != 0 || info.Mode().Perm() != 0770 {
		if err := os.Chmod(path, 0770); err != nil {
			return fmt.Errorf("protect updater inbox directory: %w", err)
		}
	}
	if businessUID == ^uint32(0) || businessGID == ^uint32(0) {
		return errors.New("registered business identity is outside the supported range")
	}
	return nil
}

// ensureTrustedAncestors rejects a writable or user-owned ancestor before a
// privileged helper relies on a path. A root-owned sticky directory such as
// /tmp is safe for an already-created root-owned child and is allowed; the
// immediate parent of the state/inbox is still required to be non-writable.
func ensureTrustedAncestors(path string, allowedOwners []uint32) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	for current := filepath.Clean(path); current != string(filepath.Separator); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// macOS exposes /var as a root-owned symlink to /private/var. It is
			// safe when the link and its resolved directory ancestry are trusted;
			// rejecting every system symlink would make the registered helper
			// unusable on the supported desktop platform.
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return fmt.Errorf("resolve %s: %w", current, resolveErr)
			}
			resolvedInfo, statErr := os.Stat(resolved)
			if statErr != nil {
				return fmt.Errorf("stat resolved ancestor %s: %w", resolved, statErr)
			}
			if !resolvedInfo.IsDir() {
				return fmt.Errorf("%s is not a real directory", current)
			}
			if err := validateTrustedDirectory(resolved, resolvedInfo, allowedOwners); err != nil {
				return err
			}
			if err := ensureTrustedAncestors(filepath.Dir(resolved), allowedOwners); err != nil {
				return err
			}
			continue
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a real directory", current)
		}
		if err := validateTrustedDirectory(current, info, allowedOwners); err != nil {
			return err
		}
	}
	return nil
}

func validateTrustedDirectory(path string, info os.FileInfo, allowedOwners []uint32) error {
	if info == nil || !info.IsDir() {
		return fmt.Errorf("%s is not a real directory", path)
	}
	owner, ok := fileOwnerUID(info)
	if !ok || !containsUID(allowedOwners, owner) {
		return fmt.Errorf("%s owner UID %d is not trusted", path, owner)
	}
	mode := info.Mode()
	if mode.Perm()&0022 != 0 && !(owner == 0 && mode&os.ModeSticky != 0) {
		return fmt.Errorf("%s must not be group/world writable", path)
	}
	return nil
}

func ensureSocketParent(socketPath string) error {
	if socketPath == "" || !filepath.IsAbs(socketPath) {
		return errors.New("updater socket path must be absolute")
	}
	parent := filepath.Dir(filepath.Clean(socketPath))
	if err := os.MkdirAll(parent, 0750); err != nil {
		return fmt.Errorf("create updater socket directory: %w", err)
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("stat updater socket directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("updater socket parent is not a real directory")
	}
	owner, ok := fileOwnerUID(info)
	if !ok || (owner != 0 && owner != uint32(os.Geteuid())) {
		return fmt.Errorf("updater socket parent owner UID %d is not trusted", owner)
	}
	if info.Mode().Perm()&0022 != 0 {
		return errors.New("updater socket parent must not be group/world writable")
	}
	return nil
}

func fileOwnerUID(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint32(stat.Uid), true
}

func fileGroupID(info os.FileInfo) (uint32, bool) {
	if info == nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint32(stat.Gid), true
}

func containsUID(allowed []uint32, owner uint32) bool {
	for _, candidate := range allowed {
		if candidate == owner {
			return true
		}
	}
	return false
}

func registeredBusinessIdentity(registration upgrade.Registration) (uint32, uint32) {
	uid, gid := registration.BusinessUID, registration.BusinessGID
	if uid == 0 && os.Geteuid() != 0 {
		uid = uint32(os.Geteuid())
	}
	if gid == 0 && os.Geteuid() != 0 {
		gid = uint32(os.Getegid())
	}
	return uid, gid
}

func configureSharedGateOwnership(path string, groupIDs ...uint32) error {
	groupID := uint32(sharedGateGroupID)
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	if err := os.Chmod(path, 0750); err != nil {
		return fmt.Errorf("protect shared updater gate directory: %w", err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(path, 0, int(groupID)); err != nil {
			return fmt.Errorf("set shared updater gate directory ownership: %w", err)
		}
	}
	return nil
}

// initializeSharedGate creates normal state only with O_EXCL. An existing
// recovery or candidate gate is always preserved across helper restarts.
func initializeSharedGate(path string, groupIDs ...uint32) error {
	groupID := uint32(sharedGateGroupID)
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("shared updater gate is not a regular file")
		}
		if err := os.Chmod(path, 0640); err != nil {
			return fmt.Errorf("protect existing shared updater gate: %w", err)
		}
		if os.Geteuid() == 0 {
			if err := os.Chown(path, 0, int(groupID)); err != nil {
				return fmt.Errorf("set existing shared updater gate ownership: %w", err)
			}
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat shared updater gate: %w", err)
	}
	state := upgrade.GateState{Mode: upgrade.GateNormal, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if errors.Is(err, os.ErrExist) {
		return initializeSharedGate(path, groupID)
	}
	if err != nil {
		return fmt.Errorf("create shared updater gate: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0640); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("protect shared updater gate: %w", err)
	}
	if os.Geteuid() == 0 {
		if err := file.Chown(0, int(groupID)); err != nil {
			_ = os.Remove(path)
			return fmt.Errorf("set shared updater gate ownership: %w", err)
		}
	}
	if _, err := file.Write(data); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write shared updater gate: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("sync shared updater gate: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close shared updater gate: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open shared updater gate directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync shared updater gate directory: %w", err)
	}
	return nil
}

func resolveServeState(statePath string, adapter upgrade.DeploymentAdapter) (string, error) {
	configured := strings.TrimSpace(statePath)
	if adapter == nil {
		if configured == "" {
			configured = defaultUpdaterState
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
	var layout upgrade.RegisteredLayout
	switch adapter := adapter.(type) {
	case *upgrade.StandaloneAdapter:
		layout = adapter.Layout
	case *upgrade.SystemdAdapter:
		layout = adapter.Layout
	case *upgrade.ComposeAdapter:
		layout = adapter.Layout
	default:
		return "", errors.New("registered deployment adapter is unsupported")
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
	deployment := ""
	switch adapter.(type) {
	case *upgrade.StandaloneAdapter:
		deployment = "standalone"
	case *upgrade.SystemdAdapter:
		deployment = "systemd"
	case *upgrade.ComposeAdapter:
		deployment = "compose"
	default:
		return upgrade.Target{}, errors.New("registered deployment adapter is unsupported")
	}
	return upgrade.Target{Deployment: deployment, OS: runtime.GOOS, Arch: runtime.GOARCH, UpdaterProtocol: upgrade.UpdaterProtocolVersion}, nil
}

func runServeLifecycle(ctx context.Context, server updaterServer, scheduler updaterScheduler) error {
	if server == nil {
		return errors.New("updater server is required")
	}
	if scheduler == nil {
		return server.Serve(ctx)
	}
	schedulerCtx, cancelScheduler := context.WithCancel(ctx)
	schedulerDone := make(chan error, 1)
	go func() { schedulerDone <- scheduler.Run(schedulerCtx) }()
	serveErr := server.Serve(ctx)
	cancelScheduler()
	schedulerErr := <-schedulerDone
	if serveErr != nil {
		return serveErr
	}
	if schedulerErr != nil && !errors.Is(schedulerErr, context.Canceled) {
		return fmt.Errorf("automatic upgrade scheduler stopped: %w", schedulerErr)
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
	allowUID := fs.Uint("allow-uid", uint(os.Getuid()), "local Unix socket UID allowed to control this helper")
	businessUID := fs.Uint("business-uid", uint(os.Getuid()), "business service UID allowed to write the updater inbox")
	businessGID := fs.Uint("business-gid", uint(os.Getgid()), "business service GID used for the updater socket and inbox")
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
