package upgrade

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestBusinessCommandRunnerUsesRegisteredIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix service identities")
	}
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	if os.Geteuid() == 0 {
		uid, gid = 65534, 65534
	}
	output, err := NewBusinessCommandRunner(uid, gid).Output(context.Background(), "/bin/sh", "-c", "id -u; id -g; id -G")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 3 || lines[0] != strconv.FormatUint(uint64(uid), 10) || lines[1] != strconv.FormatUint(uint64(gid), 10) {
		t.Fatalf("business command did not use registered identity: %q", output)
	}
	if os.Geteuid() == 0 && lines[2] != strconv.FormatUint(uint64(gid), 10) {
		t.Fatalf("business command retained privileged supplementary groups: %q", output)
	}
}

func TestBusinessCommandRunnerPreservesDirectoryAndFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix service identities")
	}
	runner := NewBusinessCommandRunner(uint32(os.Getuid()), uint32(os.Getgid()))
	path := t.TempDir()
	if err := runner.RunDir(context.Background(), path, "/bin/sh", "-c", "test \"$PWD\" = \"$1\"", "fixture", path); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(context.Background(), "/bin/sh", "-c", "exit 23"); err == nil {
		t.Fatal("business command failure was discarded")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 23 {
		t.Fatalf("wrong command failure: %v", err)
	}
}

func TestBusinessCommandRunnerCannotChangeIdentityWithoutPrivilege(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires an unprivileged Unix process")
	}
	if _, err := NewBusinessCommandRunner(0, 0).Output(context.Background(), "/bin/sh", "-c", "id -u"); err == nil {
		t.Fatal("unprivileged helper accepted root business identity")
	}
}
