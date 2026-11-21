//go:build !windows

package upgrade

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 重启脚本是在助手已经退出之后才跑的：语法错一个字符，现场就是「助手被停掉、
// 再也没起来」，而且那时已经没有人能替它报错。
func TestHelperRestartScriptIsValidShell(t *testing.T) {
	script := buildHelperRestartScript(
		"/opt/uvp/uvp-gb28181-ctl.sh",
		"/opt/uvp/updater/uvp-updater",
		"/opt/uvp/updater/uvp-updater.new",
		"/opt/uvp/logs/updater-sync.log",
	)
	command := exec.Command("sh", "-n")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated restart script is not valid shell: %v\n%s", err, output)
	}
}

func TestHelperRestartScriptQuotesAwkwardPaths(t *testing.T) {
	awkward := "/opt/uvp it's here/uvp-gb28181-ctl.sh"
	script := buildHelperRestartScript(awkward, "/opt/uvp/updater/uvp-updater", "/opt/uvp/uvp-updater.new", "/opt/uvp/log")
	command := exec.Command("sh", "-n")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("quoted path broke the script: %v\n%s", err, output)
	}
	if !strings.Contains(script, `'/opt/uvp it'\''s here/uvp-gb28181-ctl.sh'`) {
		t.Fatalf("control path is not safely quoted:\n%s", script)
	}
}

// systemd 形态必须交给 systemd 重启：自己 fork 一个会脱离单元管理，
// 下次 stop 时找不到它。
func TestHelperRestartScriptPrefersSystemdUnit(t *testing.T) {
	script := buildHelperRestartScript("/opt/uvp/ctl.sh", "/opt/uvp/updater/uvp-updater", "/opt/uvp/uvp-updater.new", "/opt/uvp/log")
	if !strings.Contains(script, "systemctl is-active --quiet '"+HelperSystemdUnit+"'") {
		t.Fatalf("script does not probe the helper unit:\n%s", script)
	}
	if !strings.Contains(script, "systemctl restart '"+HelperSystemdUnit+"'") {
		t.Fatalf("script does not restart through systemd:\n%s", script)
	}
	if !strings.Contains(script, "start-updater") {
		t.Fatalf("script lost the control-script fallback:\n%s", script)
	}
}

// 单元名两处必须逐字一致，否则重启落空、助手永远是旧的那个（静默失效）。
func TestControlScriptAgreesOnHelperUnitName(t *testing.T) {
	path := filepath.Join("..", "..", "..", "deploy", "standalone", "uvp-gb28181-ctl.sh")
	script, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("control script is not available from this checkout: %v", err)
	}
	want := `UPDATER_UNIT_NAME="` + HelperSystemdUnit + `"`
	if !containsLine(string(script), want) {
		t.Fatalf("control script must declare %s", want)
	}
}
