package upgrade

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZLMUpgradeTar 造一个带 vendor/zlm/config.ini 的升级包（模拟发布包结构）。
func writeZLMUpgradeTar(t *testing.T, path, ini string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	entries := []struct{ name, body string }{
		{filepath.ToSlash(filepath.Join("release", "vendor", "zlm", "config.ini")), ini},
		{filepath.ToSlash(filepath.Join("release", "bin", "uvp-server")), "new"},
	}
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0600, Size: int64(len(entry.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

const legacyINI = "[rtc]\r\nport=51008\r\nexternIP=62.234.152.39\r\n"
const shippedINI = "[rtc]\r\nport=51008\r\nexternIP=\r\n"

// ⛔⛔ 回归用例（2026-11-20）：升级不能把现场设在 vendor/zlm/config.ini 里的
//   值重置掉 —— 客户抱怨的正是「每次在线升级 rtc.externIP 都被清空」。
func TestStandaloneActivatePreservesOperatorZLMConfig(t *testing.T) {
	root := t.TempDir()
	oldRelease := filepath.Join(root, "releases", "old")
	if err := os.MkdirAll(filepath.Join(oldRelease, "vendor", "zlm"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRelease, legacyZLMConfigPath), []byte(legacyINI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldRelease, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}

	artifact := filepath.Join(root, "upgrade.tar")
	writeZLMUpgradeTar(t, artifact, shippedINI)
	a := &StandaloneAdapter{
		Layout: RegisteredLayout{Root: root, Control: filepath.Join(root, "ctl")},
		Runner: &fakeCommandRunner{},
	}
	record := TaskRecord{TaskID: strings.Repeat("c", 32), StagingPath: artifact}
	if err := a.Activate(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(root, persistentZLMConfigPath))
	if err != nil {
		t.Fatalf("持久副本没生成: %v", err)
	}
	if string(got) != legacyINI {
		t.Fatalf("现场配置没被保住: %q", got)
	}

	// 新发布目录里那份仍然是**出厂模板**（脚本只在首次种子时读它）。
	target, err := a.CurrentTarget()
	if err != nil {
		t.Fatal(err)
	}
	shipped, err := os.ReadFile(filepath.Join(target, legacyZLMConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(shipped) != shippedINI {
		t.Fatalf("发布模板被污染: %q", shipped)
	}
}

func TestPreserveZLMConfigIsIdempotent(t *testing.T) {
	root := t.TempDir()
	oldRelease := filepath.Join(root, "releases", "old")
	if err := os.MkdirAll(filepath.Join(oldRelease, "vendor", "zlm"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRelease, legacyZLMConfigPath), []byte(legacyINI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preserveZLMConfig(root, oldRelease); err != nil {
		t.Fatal(err)
	}
	// 第二次升级：现场已经在持久副本里改过值，不能被旧发布目录再盖回去。
	edited := "[rtc]\r\nport=51008\r\nexternIP=198.51.100.9\r\n"
	target := filepath.Join(root, persistentZLMConfigPath)
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preserveZLMConfig(root, oldRelease); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Fatalf("二次迁移覆盖了持久副本: %q", got)
	}
}

// 老版本目录里没有这份文件（或 current 还没建）时什么都不做，交给启动脚本种子。
func TestPreserveZLMConfigNoopWithoutLegacyFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "releases", "old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := preserveZLMConfig(root, filepath.Join(root, "releases", "old")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, persistentZLMConfigPath)); !os.IsNotExist(err) {
		t.Fatalf("不该凭空造出配置文件: %v", err)
	}
	if err := preserveZLMConfig(root, ""); err != nil {
		t.Fatalf("空当前版本应跳过: %v", err)
	}
}
