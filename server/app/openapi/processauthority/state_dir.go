package processauthority

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultStateProduct = "UVP-GB28181"
	defaultStateLeaf    = "process-authority"
)

// PrepareStateDir resolves the process-authority directory. An explicitly
// configured path remains deployment-owned; only the platform default is
// created automatically for the zero-configuration install path.
func PrepareStateDir(configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		return configured, nil
	}
	if deploymentPath := strings.TrimSpace(os.Getenv("UVP_PROCESS_AUTHORITY_DIR")); deploymentPath != "" {
		return prepareManagedStateDir(deploymentPath)
	}
	base, err := defaultUserStateBase()
	if err != nil {
		return "", fmt.Errorf("resolve default process authority directory: %w", err)
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("default process authority directory is not absolute")
	}
	return prepareManagedStateDir(filepath.Join(base, defaultStateProduct, defaultStateLeaf))
}

func prepareManagedStateDir(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("managed process authority directory is not absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", fmt.Errorf("create managed process authority directory: %w", err)
	}
	return path, nil
}

// DefaultStateDir documents the platform-local persistent location used when
// configuration is omitted. Service users and interactive users therefore keep
// separate authority domains.
func DefaultStateDir() (string, error) {
	base, err := defaultUserStateBase()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("default process authority directory is not absolute")
	}
	return filepath.Join(base, defaultStateProduct, defaultStateLeaf), nil
}

func defaultUserStateBase() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
			return base, nil
		}
		return "", fmt.Errorf("LOCALAPPDATA is unavailable")
	case "linux":
		if base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); base != "" {
			return base, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state"), nil
	}
	return os.UserConfigDir()
}
