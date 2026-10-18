package upgrade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type registeredPreflight struct {
	registration Registration
	run          func(context.Context, string, []string, string, ...string) ([]byte, error)
}

// LoadRegisteredPreflight queries the installed source binary in a separate
// read-only process. It keeps working when the business service is stopped.
func LoadRegisteredPreflight(path string) (func(context.Context) (PreflightSnapshot, error), error) {
	registration, err := ReadRegistration(path)
	if err != nil {
		return nil, err
	}
	return NewRegisteredPreflight(registration), nil
}

// NewRegisteredPreflight builds a reader from an already-read registration so
// the helper does not reopen a mutable path after startup.
func NewRegisteredPreflight(registration Registration) func(context.Context) (PreflightSnapshot, error) {
	p := registeredPreflight{registration: registration, run: runPreflightCommand}
	return p.read
}

func (p registeredPreflight) read(ctx context.Context) (PreflightSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	run := p.run
	if run == nil {
		run = runPreflightCommand
	}
	layout := p.registration.Standalone
	if p.registration.Deployment == "compose" {
		layout = p.registration.Compose
	}
	if err := layout.validate(); err != nil {
		return PreflightSnapshot{}, err
	}
	var output []byte
	var err error
	switch p.registration.Deployment {
	case "standalone", "systemd":
		sourceRoot := layout.Current
		if p.registration.Deployment == "standalone" {
			if _, statErr := os.Lstat(sourceRoot); errors.Is(statErr, os.ErrNotExist) {
				// First-generation managed packages retain the original flat
				// source until their first successful release switch.
				sourceRoot = layout.Root
			} else if statErr != nil {
				return PreflightSnapshot{}, statErr
			}
		}
		if sourceRoot != layout.Root {
			resolved, resolveErr := filepath.EvalSymlinks(sourceRoot)
			if resolveErr != nil {
				return PreflightSnapshot{}, fmt.Errorf("resolve current preflight release: %w", resolveErr)
			}
			releases, resolveErr := filepath.EvalSymlinks(layout.Releases)
			if resolveErr != nil || !isWithin(releases, resolved) {
				return PreflightSnapshot{}, errors.New("current preflight executable is outside registered releases")
			}
		}
		dir, binary := layout.Root, filepath.Join(sourceRoot, "bin", "uvp-server")
		if p.registration.Deployment == "systemd" {
			dir = filepath.Join(layout.Current, "backend")
			binary = filepath.Join(dir, "uvp-gb28181")
			sourceRoot = dir
		}
		env := []string{"UVP_VERSION_PATH=" + filepath.Join(sourceRoot, "version.json")}
		output, err = run(ctx, dir, env, binary, "-upgrade-preflight")
	case "compose":
		if layout.ProjectDir == "" || layout.Project == "" || !filepath.IsAbs(p.registration.ComposeFile) || !isWithin(layout.ProjectDir, p.registration.ComposeFile) {
			return PreflightSnapshot{}, errors.New("compose preflight requires a registered project and compose file")
		}
		service := layout.MigrationService
		if service == "" {
			service = "backend"
		}
		output, err = run(ctx, layout.ProjectDir, nil, "docker", "compose", "--project-directory", layout.ProjectDir, "--project-name", layout.Project, "-f", p.registration.ComposeFile, "run", "--rm", "--no-deps", "--pull", "never", "--entrypoint", "/app/uvp-gb28181", service, "-upgrade-preflight")
	default:
		return PreflightSnapshot{}, errors.New("unsupported registered preflight deployment")
	}
	if err != nil {
		return PreflightSnapshot{}, fmt.Errorf("independent upgrade preflight: %w", err)
	}
	if len(output) > maxRuntimeIdentityPayload {
		return PreflightSnapshot{}, errors.New("preflight response exceeds size limit")
	}
	var snapshot PreflightSnapshot
	if err := decodeStrictJSON(output, &snapshot); err != nil {
		return PreflightSnapshot{}, fmt.Errorf("decode independent preflight: %w", err)
	}
	if snapshot.CurrentVersion == "" || snapshot.CurrentBuildID == "" || !validDigest(snapshot.ConfigDigest) || !validDigest(snapshot.MigrationDigest) || NormalizeDatabaseDialect(snapshot.DatabaseDialect) == "" {
		return PreflightSnapshot{}, errors.New("independent preflight returned incomplete installation evidence")
	}
	return snapshot, nil
}

func runPreflightCommand(ctx context.Context, dir string, overrides []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for _, override := range overrides {
		key := strings.SplitN(override, "=", 2)[0] + "="
		filtered := cmd.Env[:0]
		for _, value := range cmd.Env {
			if !strings.HasPrefix(value, key) {
				filtered = append(filtered, value)
			}
		}
		cmd.Env = append(filtered, override)
	}
	var output preflightOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type preflightOutput struct{ bytes.Buffer }

func (b *preflightOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxRuntimeIdentityPayload {
		return 0, errors.New("preflight output exceeds limit")
	}
	return b.Buffer.Write(data)
}
