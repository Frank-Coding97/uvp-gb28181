//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package upgrade

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"syscall"
)

// BusinessCommandRunner executes application commands with the identity
// registered by the administrator. The updater remains privileged, while the
// application binary and migration process do not inherit root.
type BusinessCommandRunner struct {
	uid uint32
	gid uint32
}

func NewBusinessCommandRunner(uid, gid uint32) BusinessCommandRunner {
	return BusinessCommandRunner{uid: uid, gid: gid}
}

func (r BusinessCommandRunner) command(ctx context.Context, dir, name string, args ...string) (*exec.Cmd, error) {
	if r.uid == ^uint32(0) || r.gid == ^uint32(0) {
		return nil, errors.New("business identity is outside the supported range")
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		if !filepath.IsAbs(dir) {
			return nil, errors.New("business command directory must be absolute")
		}
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: r.uid, Gid: r.gid, NoSetGroups: true}}
	return cmd, nil
}

func (r BusinessCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd, err := r.command(ctx, "", name, args...)
	if err != nil {
		return err
	}
	return runCommandWithDiagnostic(cmd)
}

func (r BusinessCommandRunner) RunDir(ctx context.Context, dir, name string, args ...string) error {
	cmd, err := r.command(ctx, dir, name, args...)
	if err != nil {
		return err
	}
	return runCommandWithDiagnostic(cmd)
}

func (r BusinessCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd, err := r.command(ctx, "", name, args...)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}
