//go:build linux || darwin

package main

// 这些用例断言的是 POSIX 权限模型与 systemd 部署，Windows 上不成立：
//   - 共享闸门靠 0750 目录位 + 业务组 gid 表达信任边界，Windows 改用安装目录 ACL；
//   - 升级助手在 Windows 上只有 standalone 一种形态，注册里的 deployment 标签恒为 standalone。
// 因此拆到这里，而不是在用例内部 skip —— 后者仍要求符号在各平台都存在。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func TestProvisionSharedGateUsesRegisteredBusinessGroup(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing the gate group requires root")
	}
	root := t.TempDir()
	const businessGID = 23456
	if _, err := provisionSharedGate(upgrade.RegisteredLayout{Root: root}, businessGID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "updater-gate"), filepath.Join(root, "updater-gate", "gate.json")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := fileGroupID(info); !ok || got != businessGID {
			t.Fatalf("%s group = %d (available=%v), want %d", path, got, ok, businessGID)
		}
	}
}

func TestProvisionSharedGateRejectsWritableExistingParentBeforeWritingGate(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "updater-gate")
	if err := os.Mkdir(parent, 0770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0770); err != nil {
		t.Fatal(err)
	}
	_, err := provisionSharedGate(upgrade.RegisteredLayout{Root: root})
	if err == nil || !strings.Contains(err.Error(), "mode must be 0750") {
		t.Fatalf("insecure gate parent error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(parent, "gate.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gate file was written into insecure parent: %v", err)
	}
}

func TestBuildServeRuntimeWiresRegisteredPreflightAndScheduler(t *testing.T) {
	root := t.TempDir()
	registrationPath := secureRegistrationPath(t)
	if err := writeRegistration(registrationPath, upgrade.Registration{
		Deployment:  "systemd",
		Standalone:  upgrade.RegisteredLayout{Root: root},
		SystemdUnit: "uvp-backend.service",
	}); err != nil {
		t.Fatal(err)
	}
	assembly, err := buildServeRuntime(serveOptions{
		SocketPath:       filepath.Join(root, "updater.sock"),
		RegistrationPath: registrationPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if assembly.workflow == nil || assembly.workflow.Preflight == nil {
		t.Fatal("registered workflow has no independent preflight")
	}
}
