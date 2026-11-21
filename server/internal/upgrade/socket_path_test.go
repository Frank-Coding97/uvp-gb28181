package upgrade

import (
	"context"
	"strings"
	"testing"
)

func TestValidateSocketPathAcceptsShortPaths(t *testing.T) {
	for _, path := range []string{
		"/run/uvp/updater.sock",
		`C:\uvp\run\updater.sock`,
		`C:\ProgramData\uvp-updater\u-0123456789ab.sock`,
	} {
		if err := ValidateSocketPath(path); err != nil {
			t.Fatalf("ValidateSocketPath(%q) = %v, want nil", path, err)
		}
	}
}

func TestValidateSocketPathBoundary(t *testing.T) {
	atLimit := strings.Repeat("a", MaxSocketPathBytes)
	if err := ValidateSocketPath(atLimit); err != nil {
		t.Fatalf("%d-byte path must be accepted: %v", MaxSocketPathBytes, err)
	}
	overLimit := strings.Repeat("a", MaxSocketPathBytes+1)
	if err := ValidateSocketPath(overLimit); err == nil {
		t.Fatalf("%d-byte path must be rejected", MaxSocketPathBytes+1)
	}
}

func TestValidateSocketPathCountsUTF8BytesNotRunes(t *testing.T) {
	// 40 CJK characters = 120 bytes but only 40 runes. Counting runes would
	// silently let an unbindable path through.
	path := strings.Repeat("中", 40) + ".sock"
	if len([]rune(path)) >= MaxSocketPathBytes {
		t.Fatalf("fixture drift: %d runes", len([]rune(path)))
	}
	if err := ValidateSocketPath(path); err == nil {
		t.Fatalf("%d-byte non-ASCII path must be rejected", len(path))
	}
}

// 后端拨号侧同样要提前拦住，不能把 "connect: invalid argument" 原样丢给
// 服务升级页（页面只会显示一个笼统的 503）。
func TestCallUnixRejectsOverlongSocketPath(t *testing.T) {
	request := ProtocolRequest{Protocol: UpdaterProtocolVersion, Action: ActionStatus}
	_, err := CallUnix(context.Background(), strings.Repeat("a", MaxSocketPathBytes+1), request)
	if err == nil || !strings.Contains(err.Error(), "107-byte") {
		t.Fatalf("CallUnix error = %v", err)
	}
}

func TestValidateSocketPathRejectsBlank(t *testing.T) {
	for _, path := range []string{"", "   ", "\t"} {
		if err := ValidateSocketPath(path); err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("ValidateSocketPath(%q) = %v", path, err)
		}
	}
}

func TestValidateSocketPathErrorIsActionable(t *testing.T) {
	path := strings.Repeat("a", 130)
	err := ValidateSocketPath(path)
	if err == nil {
		t.Fatal("overlong path must be rejected")
	}
	for _, want := range []string{
		"130 bytes", "107-byte", "108 bytes", "shorter directory", "duplicated release folder", path,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// 现场复现（2026-11-10，192.168.10.222 / Windows 1.1.21 全量包）：
// 压缩包套了两层同名目录，助手 bind: invalid argument、后端 connect: invalid
// argument，「服务升级」页一直 503；把内层目录改名 app 后立刻恢复。
func TestValidateSocketPathRealWorldWindowsCase(t *testing.T) {
	nested := `C:\Users\lnsoft\Downloads\uvp-gb28181-windows-amd64-1.1.21\uvp-gb28181-windows-amd64-1.1.21\run\updater.sock`
	if len(nested) != 108 {
		t.Fatalf("fixture drift: nested path is %d bytes, want 108", len(nested))
	}
	if err := ValidateSocketPath(nested); err == nil {
		t.Fatal("the 108-byte production path must be rejected")
	}
	short := `C:\Users\lnsoft\Downloads\uvp-gb28181-windows-amd64-1.1.21\app\run\updater.sock`
	if len(short) != 79 {
		t.Fatalf("fixture drift: renamed path is %d bytes, want 79", len(short))
	}
	if err := ValidateSocketPath(short); err != nil {
		t.Fatalf("renamed production path must be accepted: %v", err)
	}
}
