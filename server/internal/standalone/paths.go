// Package standalone contains the filesystem contract used by the Windows
// single machine package.  It deliberately has no dependency on the process
// working directory: callers must provide absolute roots for every package
// directory.
package standalone

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	EnvInstallDir    = "UVP_INSTALL_DIR"
	EnvConfigDir     = "UVP_CONFIG_DIR"
	EnvResourceDir   = "UVP_RESOURCE_DIR"
	EnvWebDir        = "UVP_WEB_DIR"
	EnvDataDir       = "UVP_DATA_DIR"
	EnvRecordingsDir = "UVP_RECORDINGS_DIR"

	ArgInstallDir    = "-uvp-install-dir"
	ArgConfigDir     = "-uvp-config-dir"
	ArgResourceDir   = "-uvp-resource-dir"
	ArgWebDir        = "-uvp-web-dir"
	ArgDataDir       = "-uvp-data-dir"
	ArgRecordingsDir = "-uvp-recordings-dir"
)

var (
	ErrExplicitPathRequired = errors.New("standalone: explicit path is required")
	ErrPathNotAbsolute      = errors.New("standalone: path must be absolute")
	ErrUNCPath              = errors.New("standalone: UNC paths are not supported")
	ErrPathOutsideInstall   = errors.New("standalone: path escapes install directory")
	ErrPathMissing          = errors.New("standalone: path does not exist")
	ErrPathNotDirectory     = errors.New("standalone: path is not a directory")
	ErrPathNotWritable      = errors.New("standalone: path is not writable")
	ErrPathSymlink          = errors.New("standalone: symbolic links are not supported")
)

// PathOptions are the roots supplied by the launcher. Every root is explicit;
// the server executable may live below a versioned release directory, so its
// own location cannot be used to guess InstallDir.
type PathOptions struct {
	InstallDir    string
	ConfigDir     string
	ResourceDir   string
	WebDir        string
	DataDir       string
	RecordingsDir string
}

// Paths is the resolved filesystem layout for one standalone instance.
type Paths struct {
	Explicit        bool   `json:"explicit"`
	InstallDir      string `json:"install_dir"`
	ConfigDir       string `json:"config_dir"`
	ConfigFile      string `json:"config_file"`
	ResourceDir     string `json:"resource_dir"`
	WebDir          string `json:"web_dir"`
	DataDir         string `json:"data_dir"`
	DatabasePath    string `json:"database_path"`
	UploadDir       string `json:"upload_dir"`
	RecordingsDir   string `json:"recordings_dir"`
	LogsDir         string `json:"logs_dir"`
	SchedulerLogDir string `json:"scheduler_log_dir"`
}

// ResolvePaths validates and derives an explicit package layout. Config,
// resource, web, and data roots must remain below InstallDir. Recordings may
// live on another local drive, which is needed for field deployments.
func ResolvePaths(options PathOptions) (Paths, error) {
	installDir, err := cleanAbsolute(options.InstallDir)
	if err != nil {
		return Paths{}, fmt.Errorf("install directory: %w", err)
	}

	configDir, err := cleanAbsolute(options.ConfigDir)
	if err != nil {
		return Paths{}, fmt.Errorf("config directory: %w", err)
	}
	resourceDir, err := cleanAbsolute(options.ResourceDir)
	if err != nil {
		return Paths{}, fmt.Errorf("resource directory: %w", err)
	}
	webDir, err := cleanAbsolute(options.WebDir)
	if err != nil {
		return Paths{}, fmt.Errorf("web directory: %w", err)
	}
	dataDir, err := cleanAbsolute(options.DataDir)
	if err != nil {
		return Paths{}, fmt.Errorf("data directory: %w", err)
	}

	for name, candidate := range map[string]string{
		"config":   configDir,
		"resource": resourceDir,
		"web":      webDir,
		"data":     dataDir,
	} {
		if !isWithin(installDir, candidate) {
			return Paths{}, fmt.Errorf("%s directory: %w", name, ErrPathOutsideInstall)
		}
	}

	recordingsDir := options.RecordingsDir
	if strings.TrimSpace(recordingsDir) == "" {
		recordingsDir = filepath.Join(installDir, "recordings")
	}
	recordingsDir, err = cleanAbsolute(recordingsDir)
	if err != nil {
		return Paths{}, fmt.Errorf("recordings directory: %w", err)
	}

	return Paths{
		Explicit:        true,
		InstallDir:      installDir,
		ConfigDir:       configDir,
		ConfigFile:      filepath.Join(configDir, "config.yml"),
		ResourceDir:     resourceDir,
		WebDir:          webDir,
		DataDir:         dataDir,
		DatabasePath:    filepath.Join(dataDir, "uvp.db"),
		UploadDir:       filepath.Join(dataDir, "uploads"),
		RecordingsDir:   recordingsDir,
		LogsDir:         filepath.Join(installDir, "logs"),
		SchedulerLogDir: filepath.Join(installDir, "logs", "scheduler"),
	}, nil
}

// ResolveStartupPaths reads the supported path arguments and environment
// variables. An empty set means legacy mode and returns zero Paths. Once any
// standalone path input is present, every root, including InstallDir, is
// mandatory.
func ResolveStartupPaths(args []string, envLookup func(string) string) (Paths, error) {
	if envLookup == nil {
		envLookup = os.Getenv
	}

	options := PathOptions{
		InstallDir:    strings.TrimSpace(envLookup(EnvInstallDir)),
		ConfigDir:     strings.TrimSpace(envLookup(EnvConfigDir)),
		ResourceDir:   strings.TrimSpace(envLookup(EnvResourceDir)),
		WebDir:        strings.TrimSpace(envLookup(EnvWebDir)),
		DataDir:       strings.TrimSpace(envLookup(EnvDataDir)),
		RecordingsDir: strings.TrimSpace(envLookup(EnvRecordingsDir)),
	}

	explicit, err := applyPathArgs(&options, args)
	if err != nil {
		return Paths{}, err
	}
	if !explicit && allEmpty(options) {
		return Paths{}, nil
	}

	for name, value := range map[string]string{
		"install":  options.InstallDir,
		"config":   options.ConfigDir,
		"resource": options.ResourceDir,
		"web":      options.WebDir,
		"data":     options.DataDir,
	} {
		if strings.TrimSpace(value) == "" {
			return Paths{}, fmt.Errorf("%s directory: %w", name, ErrExplicitPathRequired)
		}
	}
	return ResolvePaths(options)
}

// Validate checks the filesystem before any database or business component is
// started. Resource and web directories are read-only package inputs; config,
// data, and recordings must be writable.
func (p Paths) Validate() error {
	if !p.Explicit {
		return nil
	}
	for name, dir := range map[string]string{
		"install":    p.InstallDir,
		"config":     p.ConfigDir,
		"resource":   p.ResourceDir,
		"web":        p.WebDir,
		"data":       p.DataDir,
		"recordings": p.RecordingsDir,
	} {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("%s directory: %w: %v", name, ErrPathMissing, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s directory: %w", name, ErrPathNotDirectory)
		}
		if err := validateLocalVolume(dir); err != nil {
			return fmt.Errorf("%s directory: %w", name, err)
		}
	}
	installReal, err := filepath.EvalSymlinks(p.InstallDir)
	if err != nil {
		return fmt.Errorf("install directory: %w: %v", ErrPathMissing, err)
	}
	for name, dir := range map[string]string{
		"config":   p.ConfigDir,
		"resource": p.ResourceDir,
		"web":      p.WebDir,
		"data":     p.DataDir,
	} {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return fmt.Errorf("%s directory: %w: %v", name, ErrPathMissing, err)
		}
		if !isWithin(installReal, resolved) {
			return fmt.Errorf("%s directory: %w", name, ErrPathOutsideInstall)
		}
	}
	if resolved, err := filepath.EvalSymlinks(p.RecordingsDir); err != nil {
		return fmt.Errorf("recordings directory: %w: %v", ErrPathMissing, err)
	} else if isUNCPath(resolved) {
		return fmt.Errorf("recordings directory: %w", ErrUNCPath)
	}
	if err := validateExistingTarget(p.ConfigDir, p.ConfigFile); err != nil {
		return fmt.Errorf("config file: %w", err)
	}
	if err := validateExistingTarget(p.DataDir, p.DatabasePath); err != nil {
		return fmt.Errorf("database path: %w", err)
	}
	if err := validateExistingTarget(p.DataDir, p.UploadDir); err != nil {
		return fmt.Errorf("upload directory: %w", err)
	}
	if err := validateExistingTarget(p.InstallDir, p.LogsDir); err != nil {
		return fmt.Errorf("logs directory: %w", err)
	}
	for name, dir := range map[string]string{
		"install":    p.InstallDir,
		"config":     p.ConfigDir,
		"data":       p.DataDir,
		"recordings": p.RecordingsDir,
	} {
		if err := validateWritableDirectory(dir); err != nil {
			return fmt.Errorf("%s directory: %w", name, err)
		}
	}
	return nil
}

func applyPathArgs(options *PathOptions, args []string) (bool, error) {
	values := map[string]*string{
		ArgInstallDir:          &options.InstallDir,
		ArgConfigDir:           &options.ConfigDir,
		ArgResourceDir:         &options.ResourceDir,
		ArgWebDir:              &options.WebDir,
		ArgDataDir:             &options.DataDir,
		ArgRecordingsDir:       &options.RecordingsDir,
		"--uvp-install-dir":    &options.InstallDir,
		"--uvp-config-dir":     &options.ConfigDir,
		"--uvp-resource-dir":   &options.ResourceDir,
		"--uvp-web-dir":        &options.WebDir,
		"--uvp-data-dir":       &options.DataDir,
		"--uvp-recordings-dir": &options.RecordingsDir,
	}

	explicit := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		key, value, hasValue := strings.Cut(arg, "=")
		destination, ok := values[key]
		if !ok {
			continue
		}
		explicit = true
		if !hasValue {
			if index+1 >= len(args) {
				return false, fmt.Errorf("%s: %w", key, ErrExplicitPathRequired)
			}
			index++
			value = args[index]
		}
		if strings.TrimSpace(value) == "" {
			return false, fmt.Errorf("%s: %w", key, ErrExplicitPathRequired)
		}
		*destination = value
	}
	return explicit, nil
}

func allEmpty(options PathOptions) bool {
	return strings.TrimSpace(options.InstallDir) == "" &&
		strings.TrimSpace(options.ConfigDir) == "" &&
		strings.TrimSpace(options.ResourceDir) == "" &&
		strings.TrimSpace(options.WebDir) == "" &&
		strings.TrimSpace(options.DataDir) == "" &&
		strings.TrimSpace(options.RecordingsDir) == ""
}

func cleanAbsolute(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrExplicitPathRequired
	}
	if isUNCPath(value) {
		return "", ErrUNCPath
	}
	if !isAbsolutePath(value) {
		return "", ErrPathNotAbsolute
	}
	return filepath.Clean(value), nil
}

func isAbsolutePath(value string) bool {
	if !filepath.IsAbs(value) {
		return false
	}
	// On Windows, a path beginning with a separator is rooted on the current
	// drive. It is still dependent on machine state, so only a drive-qualified
	// absolute path is accepted for launcher roots.
	if runtime.GOOS == "windows" && (strings.HasPrefix(value, `\`) || strings.HasPrefix(value, "/")) {
		return false
	}
	return true
}

func isUNCPath(value string) bool {
	normalized := strings.ReplaceAll(value, "/", `\`)
	return strings.HasPrefix(normalized, `\\`) || strings.HasPrefix(strings.ToLower(normalized), `\\?\unc\`)
}

func isWithin(base, candidate string) bool {
	relative, err := filepath.Rel(base, candidate)
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}
	if runtime.GOOS == "windows" {
		relative = strings.ToLower(relative)
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateWritableDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathMissing, err)
	}
	if !info.IsDir() {
		return ErrPathNotDirectory
	}
	// The permission check gives deterministic behavior in Unix test fixtures,
	// including when the test process itself runs as root. The temporary file
	// check covers Windows ACLs and actual filesystem write failures.
	if info.Mode().Perm()&0o222 == 0 {
		return ErrPathNotWritable
	}
	file, err := os.CreateTemp(dir, ".uvp-path-check-*")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPathNotWritable, err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("%w: %v", ErrPathNotWritable, err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("%w: %v", ErrPathNotWritable, err)
	}
	return nil
}

func validateExistingTarget(base, target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s", ErrPathSymlink, target)
	}
	if err := validateLocalVolume(target); err != nil {
		return err
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return err
	}
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		return err
	}
	if !isWithin(baseReal, targetReal) {
		return ErrPathOutsideInstall
	}
	return nil
}

// ValidateDatabaseFile rejects implicit locations and link escapes before the
// SQLite driver can create or modify a file.
func ValidateDatabaseFile(path string) error {
	clean, err := cleanAbsolute(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(clean)
	if err := validateLocalVolume(parent); err != nil {
		return err
	}
	if err := validateExistingTarget(parent, clean); err != nil {
		return err
	}
	if info, err := os.Stat(clean); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("standalone: database is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return validateWritableDirectory(parent)
}
