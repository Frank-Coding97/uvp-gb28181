package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/firewall"
)

const firewallCommandTimeout = 60 * time.Second

type firewallCommand struct {
	action      firewall.Action
	interfaceID string
	yes         bool
	internal    bool
	planHash    string
}

type firewallCommandOptions struct {
	in       io.Reader
	out      io.Writer
	errOut   io.Writer
	load     firewall.Loader
	identity firewall.Loader
	adapter  func() firewall.Adapter
	elevate  func(context.Context, firewall.Action, string, string) error
	// requireElevated is injected by tests. The production default is set by
	// runFirewallCommand and is checked before any adapter or filesystem I/O.
	requireElevated func() error
}

func runFirewallCommand(args []string) (int, bool) {
	options := firewallCommandOptions{
		in:              os.Stdin,
		out:             os.Stdout,
		errOut:          os.Stderr,
		load:            loadFirewallInputs,
		identity:        loadFirewallIdentity,
		adapter:         firewall.NewSystemAdapter,
		elevate:         elevateFirewall,
		requireElevated: requireFirewallElevation,
	}
	if len(args) == 0 {
		return 0, false
	}
	if (args[0] == "firewall" || args[0] == "--uvp-firewall-internal") && runtime.GOOS != "windows" {
		return writeFirewallFailure(options, "", firewall.ReasonUnsupportedPlatform), true
	}
	if args[0] == "firewall" {
		return runFirewallPublic(args[1:], options), true
	}
	if args[0] == "--uvp-firewall-internal" {
		return runFirewallInternal(args[1:], options), true
	}
	return 0, false
}

func runFirewallPublic(args []string, options firewallCommandOptions) int {
	command, err := parseFirewallPublic(args)
	if err != nil {
		return writeFirewallFailure(options, firewall.Action(""), firewall.ReasonInvalidInput)
	}
	if command.action == firewall.ActionStatus {
		return runFirewallStatus(command, options)
	}
	return runFirewallMutation(command, options)
}

func runFirewallInternal(args []string, options firewallCommandOptions) int {
	command, err := parseFirewallInternal(args)
	if err != nil {
		return writeFirewallFailure(options, firewall.Action(""), firewall.ReasonInvalidInput)
	}
	// The elevated child is the only process allowed to touch firewall state.
	// Keep this check ahead of loader/adapter construction so a non-elevated
	// invocation cannot inspect the installation or enumerate interfaces.
	if options.requireElevated == nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonPermissionDenied)
	}
	if options.requireElevated != nil {
		if err := options.requireElevated(); err != nil {
			return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
		}
	}
	if options.adapter == nil || options.load == nil || (command.action == firewall.ActionRemove && options.identity == nil) {
		return writeFirewallFailure(options, command.action, firewall.ReasonInvalidInput)
	}
	ctx, cancel := context.WithTimeout(context.Background(), firewallCommandTimeout)
	defer cancel()
	adapter := options.adapter()
	if adapter == nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonInvalidInput)
	}
	load := options.load
	if command.action == firewall.ActionRemove {
		load = options.identity
	}
	adapter = &confirmedFirewallAdapter{Adapter: adapter, load: load, command: command}
	service := firewall.NewService(adapter, load)
	var result firewall.Result
	if command.action == firewall.ActionApply {
		result, err = service.Apply(ctx, command.interfaceID)
	} else {
		result, err = service.Remove(ctx, command.interfaceID)
	}
	if err != nil {
		if result.Action == "" {
			result.Action = command.action
		}
		if result.Reason == "" {
			result.Reason = firewall.ReasonOf(err)
		}
		return writeFirewallResult(options, result)
	}
	return writeFirewallResult(options, result)
}

func runFirewallStatus(command firewallCommand, options firewallCommandOptions) int {
	if options.adapter == nil || options.load == nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonInvalidInput)
	}
	ctx, cancel := context.WithTimeout(context.Background(), firewallCommandTimeout)
	defer cancel()
	service := firewall.NewService(options.adapter(), options.load)
	result, err := service.Status(ctx)
	if err != nil {
		if result.Action == "" {
			result.Action = command.action
		}
		if result.Reason == "" {
			result.Reason = firewall.ReasonOf(err)
		}
		return writeFirewallResult(options, result)
	}
	return writeFirewallResult(options, result)
}

func runFirewallMutation(command firewallCommand, options firewallCommandOptions) int {
	ctx, cancel := context.WithTimeout(context.Background(), firewallCommandTimeout)
	defer cancel()
	if options.load == nil || options.adapter == nil || options.elevate == nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonInvalidInput)
	}
	if command.action == firewall.ActionRemove {
		return runFirewallRemove(command, options, ctx)
	}
	inputs, err := options.load(ctx)
	if err != nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
	}
	if err := firewall.ValidateInputs(inputs); err != nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
	}
	adapter := options.adapter()
	if adapter == nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonInvalidInput)
	}
	interfaces, err := adapter.Interfaces(ctx)
	if err != nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
	}
	selected, err := firewall.SelectInterface(inputs.SIP, interfaces, command.interfaceID)
	if err != nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
	}
	rules, err := firewall.BuildRules(inputs, selected)
	if err != nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
	}
	writeFirewallPreview(options.out, command.action, selected, rules)
	if !command.yes && !readFirewallConfirmation(options.in) {
		return writeFirewallFailure(options, command.action, firewall.ReasonConfirmationRequired)
	}
	if err := options.elevate(ctx, command.action, command.interfaceID, firewallPlanHash(inputs, command.action, rules)); err != nil {
		return reportFirewallChildFailure(ctx, options, command.action, err)
	}
	return writeFirewallResult(options, firewall.Result{
		Action:      command.action,
		Success:     true,
		Converged:   true,
		InterfaceID: command.interfaceID,
	})
}

func runFirewallRemove(command firewallCommand, options firewallCommandOptions, ctx context.Context) int {
	if options.identity == nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonInvalidInput)
	}
	inputs, err := options.identity(ctx)
	if err != nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
	}
	if err := firewall.ValidateIdentity(inputs); err != nil {
		return writeFirewallFailure(options, command.action, firewall.ReasonOf(err))
	}
	names := firewall.RuleNames(inputs.CanonicalRoot)
	if len(names) != 4 {
		return writeFirewallFailure(options, command.action, firewall.ReasonInvalidInput)
	}
	rules := make([]firewall.Rule, 0, len(names))
	for i, name := range names {
		role, protocol := "backend", "TCP"
		if i == 1 {
			protocol = "UDP"
		} else if i >= 2 {
			role = "media"
			if i == 3 {
				protocol = "UDP"
			}
		}
		rules = append(rules, firewall.Rule{Name: name, Role: role, Protocol: protocol, Program: programForRole(inputs, role)})
	}
	writeFirewallPreview(options.out, command.action, firewall.InterfaceInfo{}, rules)
	if !command.yes && !readFirewallConfirmation(options.in) {
		return writeFirewallFailure(options, command.action, firewall.ReasonConfirmationRequired)
	}
	if err := options.elevate(ctx, command.action, "", firewallPlanHash(inputs, command.action, nil)); err != nil {
		return reportFirewallChildFailure(ctx, options, command.action, err)
	}
	return writeFirewallResult(options, firewall.Result{Action: command.action, Success: true, Converged: true})
}

func programForRole(inputs firewall.Inputs, role string) string {
	if role == "media" {
		return inputs.MediaExe
	}
	return inputs.BackendExe
}

func parseFirewallPublic(args []string) (firewallCommand, error) {
	if len(args) == 0 {
		return firewallCommand{}, errors.New("missing firewall action")
	}
	command := firewallCommand{action: firewall.Action(strings.ToLower(args[0]))}
	if command.action != firewall.ActionStatus && command.action != firewall.ActionApply && command.action != firewall.ActionRemove {
		return firewallCommand{}, errors.New("unsupported firewall action")
	}
	if command.action == firewall.ActionStatus {
		if len(args) != 1 {
			return firewallCommand{}, errors.New("status does not accept flags")
		}
		return command, nil
	}
	if command.action == firewall.ActionRemove {
		for index := 1; index < len(args); index++ {
			if args[index] != "--yes" || command.yes {
				return firewallCommand{}, errors.New("remove accepts only one optional confirmation")
			}
			command.yes = true
		}
		return command, nil
	}
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--interface-id":
			if command.interfaceID != "" || index+1 >= len(args) {
				return firewallCommand{}, errors.New("interface id is required once")
			}
			index++
			command.interfaceID = strings.TrimSpace(args[index])
			if command.interfaceID == "" {
				return firewallCommand{}, errors.New("interface id is empty")
			}
		case "--yes":
			if command.yes {
				return firewallCommand{}, errors.New("confirmation repeated")
			}
			command.yes = true
		default:
			return firewallCommand{}, errors.New("unsupported firewall option")
		}
	}
	if command.interfaceID == "" {
		return firewallCommand{}, errors.New("interface id is required")
	}
	return command, nil
}

func parseFirewallInternal(args []string) (firewallCommand, error) {
	if len(args) < 3 || args[len(args)-2] != "--plan-sha256" || !validFirewallPlanHash(args[len(args)-1]) {
		return firewallCommand{}, errors.New("invalid internal plan hash")
	}
	command, err := parseFirewallPublic(args[:len(args)-2])
	if err != nil || command.yes || command.action == firewall.ActionStatus {
		return firewallCommand{}, errors.New("invalid internal firewall action")
	}
	command.internal = true
	command.planHash = args[len(args)-1]
	return command, nil
}

func writeFirewallPreview(out io.Writer, action firewall.Action, selected firewall.InterfaceInfo, rules []firewall.Rule) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "UVP firewall %s preview (interface %s: %s)\n", action, selected.ID, selected.Alias)
	for _, rule := range rules {
		fmt.Fprintf(out, "  %s role=%s protocol=%s program=%s local-port=%s profile=%s remote=%s direction=%s action=%s edge=%s\n", rule.Name, rule.Role, rule.Protocol, rule.Program, rule.LocalPort, rule.Profile, rule.RemoteAddress, rule.Direction, rule.Action, rule.EdgeTraversalPolicy)
	}
	fmt.Fprintln(out, "Only these four instance rules are addressed; enter yes to continue.")
}

func readFirewallConfirmation(in io.Reader) bool {
	if in == nil {
		return false
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	return (err == nil || errors.Is(err, io.EOF)) && strings.EqualFold(strings.TrimSpace(line), "yes")
}

func writeFirewallFailure(options firewallCommandOptions, action firewall.Action, reason firewall.Reason) int {
	return writeFirewallResult(options, firewall.Result{Action: action, Reason: reason})
}

func writeFirewallResult(options firewallCommandOptions, result firewall.Result) int {
	if options.out == nil {
		options.out = io.Discard
	}
	if err := json.NewEncoder(options.out).Encode(result); err != nil {
		return 1
	}
	if result.Success {
		return 0
	}
	if options.errOut != nil && result.Reason != "" {
		fmt.Fprintln(options.errOut, "UVP firewall:", result.Reason)
	}
	return firewallExitCode(result.Reason)
}

func loadFirewallInputs(ctx context.Context) (firewall.Inputs, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	identity, release, err := loadVerifiedFirewallIdentity()
	if err != nil {
		return firewall.Inputs{}, err
	}
	installDir := identity.CanonicalRoot
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir:    installDir,
		ConfigDir:     filepath.Join(installDir, "config"),
		ResourceDir:   release.ResourceDir,
		WebDir:        release.WebDir,
		DataDir:       filepath.Join(installDir, "data"),
		RecordingsDir: filepath.Join(installDir, "recordings"),
	})
	if err != nil {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonConfigurationUnavailable)
	}
	config, err := standalone.LoadConfig(paths)
	if err != nil {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonConfigurationUnavailable)
	}
	db, closeDB, err := openFirewallReadOnlyDatabase(paths.DatabasePath)
	if err != nil {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonDatabaseUnavailable)
	}
	defer closeDB()
	var state firewallInstallation
	stateResult := db.WithContext(ctx).Table("standalone_installation").
		Select("phase,admin_user_id,completed_at").Where("id = ?", 1).Take(&state)
	if stateResult.Error != nil {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonDatabaseUnavailable)
	}
	if stateResult.RowsAffected != 1 {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonInstallationIncomplete)
	}
	if state.Phase != "complete" || state.CompletedAt == nil {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonInstallationIncomplete)
	}
	var row firewallSIPConfig
	result := db.WithContext(ctx).Table("gb_sip_config").
		Select("id,listen_ip,port").
		Where("id = ?", setup.SingletonID).
		Take(&row)
	if result.Error != nil {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonDatabaseUnavailable)
	}
	if result.RowsAffected != 1 {
		return firewall.Inputs{}, firewall.NewError(firewall.ReasonSIPConfigMissing)
	}
	return firewall.Inputs{
		CanonicalRoot:   installDir,
		BackendExe:      release.BackendExe,
		MediaExe:        release.MediaExe,
		ReleaseVerified: true,
		ReleaseID:       identity.ReleaseID,
		SIP: firewall.SIPConfig{
			ListenIP: row.ListenIP,
			Port:     row.Port,
		},
		MediaListeners: config.MediaListeners(),
	}, nil
}

func loadFirewallIdentity(ctx context.Context) (firewall.Inputs, error) {
	identity, _, err := loadVerifiedFirewallIdentity()
	return identity, err
}

func loadVerifiedFirewallIdentity() (firewall.Inputs, standalone.Release, error) {
	executable, err := os.Executable()
	if err != nil {
		return firewall.Inputs{}, standalone.Release{}, firewall.NewError(firewall.ReasonConfigurationUnavailable)
	}
	installDir := filepath.Clean(filepath.Dir(executable))
	if !filepath.IsAbs(installDir) {
		return firewall.Inputs{}, standalone.Release{}, firewall.NewError(firewall.ReasonReleaseUnverified)
	}
	info, err := os.Lstat(installDir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return firewall.Inputs{}, standalone.Release{}, firewall.NewError(firewall.ReasonReleaseUnverified)
	}
	resolved, err := filepath.EvalSymlinks(installDir)
	if err != nil || !sameFirewallPath(installDir, resolved) {
		return firewall.Inputs{}, standalone.Release{}, firewall.NewError(firewall.ReasonReleaseUnverified)
	}
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		return firewall.Inputs{}, standalone.Release{}, firewall.NewError(firewall.ReasonReleaseUnverified)
	}
	manifest, err := os.ReadFile(filepath.Join(release.ReleaseDir, "manifest.json"))
	if err != nil {
		return firewall.Inputs{}, standalone.Release{}, firewall.NewError(firewall.ReasonReleaseUnverified)
	}
	return firewall.Inputs{CanonicalRoot: installDir, BackendExe: release.BackendExe, MediaExe: release.MediaExe, ReleaseVerified: true, ReleaseID: firewallDigest(manifest)}, release, nil
}

func sameFirewallPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func openFirewallReadOnlyDatabase(path string) (*gorm.DB, func(), error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, func() {}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, func() {}, errors.New("invalid database file")
	}
	if err := standalone.ValidateDatabaseFile(path); err != nil {
		return nil, func() {}, err
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	query := url.Values{}
	query.Set("mode", "ro")
	dsn := (&url.URL{Scheme: "file", Path: uriPath, RawQuery: query.Encode()}).String()
	db, err := gorm.Open(sqlitedialect.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	if err != nil {
		return nil, func() {}, err
	}
	raw, err := db.DB()
	if err != nil {
		return nil, func() {}, err
	}
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	return db, func() { _ = raw.Close() }, nil
}

type firewallInstallation struct {
	Phase       string     `gorm:"column:phase"`
	AdminUserID *uint      `gorm:"column:admin_user_id"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

type firewallSIPConfig struct {
	ID       uint8  `gorm:"column:id"`
	ListenIP string `gorm:"column:listen_ip"`
	Port     int    `gorm:"column:port"`
}
