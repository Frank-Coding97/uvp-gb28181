//go:build !windows

package upgrade

// 注册文件的信任边界在 Unix 上靠 POSIX 表达：必须是 0600 的普通文件、父目录不能
// 组可写、不能经由符号链接进入。Windows 换成了另一套判据（拒绝 reparse point、
// 安装根目录 ACL），因此这些断言只在 Unix 上有意义。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegistrationRejectsUnsafePathAndPermissions(t *testing.T) {
	root := secureRegistrationTestDir(t)
	path := filepath.Join(root, "registration.json")
	data := []byte(`{"deployment":"standalone","standalone":{"root":"/opt/uvp"}}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegistration("relative-registration.json"); err == nil {
		t.Fatal("relative registration path accepted")
	}
	if err := os.Chmod(path, 0620); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegistration(path); err == nil {
		t.Fatal("group-writable registration accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "registration-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegistration(link); err == nil {
		t.Fatal("symlinked registration accepted")
	}
	if _, err := ReadRegistration(root); err == nil {
		t.Fatal("registration directory accepted")
	}
}

func TestReadRegistrationRejectsBusinessWritableOrSymlinkedParent(t *testing.T) {
	root := secureRegistrationTestDir(t)
	data := []byte(`{"deployment":"standalone","standalone":{"root":"/opt/uvp"}}`)

	writable := filepath.Join(root, "business-writable")
	if err := os.Mkdir(writable, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writable, 0770); err != nil {
		t.Fatal(err)
	}
	writablePath := filepath.Join(writable, "registration.json")
	if err := os.WriteFile(writablePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegistration(writablePath); err == nil {
		t.Fatal("registration under business-writable parent accepted")
	}

	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(real, "registration.json")
	if err := os.WriteFile(realPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegistration(filepath.Join(linked, "registration.json")); err == nil {
		t.Fatal("registration below symlinked parent accepted")
	}
}
