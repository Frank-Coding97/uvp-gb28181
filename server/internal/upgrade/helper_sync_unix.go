//go:build !windows

package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// helperRestartDelay 让「任务已成功」的 RPC 响应先写完再动手：助手重启会短暂
// 断开 socket，紧接着的响应写失败会让成功看起来像失败。
const helperRestartDelay = 3 * time.Second

// SyncHelper replaces the installed helper with the mirror staged from the
// release that was just activated, then restarts it out of band.
//
// It runs only after the task is durably Succeeded, so nothing here may fail the
// upgrade. The replacement is done by a detached shell rather than in-process
// because stop-updater terminates this very process: the shell survives, waits
// for the pid to disappear, and starts the new image.
func (a *StandaloneAdapter) SyncHelper(ctx context.Context, record TaskRecord) (HelperSyncResult, error) {
	if err := a.Layout.validate(); err != nil {
		return HelperSyncResult{}, err
	}
	control := strings.TrimSpace(a.Layout.Control)
	if control == "" {
		return HelperSyncResult{}, errors.New("standalone control path is not registered")
	}
	staged := filepath.Join(a.Layout.Root, HelperEntryDir, HelperUnixImage+HelperStagedSuffix)
	if _, err := os.Lstat(staged); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return HelperSyncResult{Reason: "release did not ship a newer helper"}, nil
		}
		return HelperSyncResult{}, fmt.Errorf("inspect staged helper: %w", err)
	}
	if err := a.scheduleHelperRestart(ctx, control, staged); err != nil {
		return HelperSyncResult{Staged: true}, err
	}
	return HelperSyncResult{Staged: true, RestartScheduled: true}, nil
}

func (a *StandaloneAdapter) scheduleHelperRestart(ctx context.Context, control, staged string) error {
	logPath := filepath.Join(a.Layout.Root, "logs", "updater-sync.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create helper sync log directory: %w", err)
	}
	binary := filepath.Join(a.Layout.Root, HelperEntryDir, HelperUnixImage)
	script := buildHelperRestartScript(control, binary, staged, logPath)
	// 用 context.WithoutCancel：调用方的 ctx 会在任务收尾后取消，而这里恰恰要
	// 活过那个时刻。
	command := exec.CommandContext(context.WithoutCancel(ctx), "/bin/sh", "-c", script)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start helper restart: %w", err)
	}
	// 不等待：让子进程脱离本次请求的生命周期，失败原因由它自己写进日志。
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("detach helper restart: %w", err)
	}
	return nil
}

// buildHelperRestartScript keeps the variable set minimal on purpose: it runs
// after the helper is gone, so anything that could fail early leaves the
// installation with a live-but-old helper and a sidecar for the next attempt.
func buildHelperRestartScript(control, binary, staged, logPath string) string {
	quotedControl := shellSingleQuote(control)
	lines := []string{
		fmt.Sprintf("sleep %d", int(helperRestartDelay/time.Second)),
		fmt.Sprintf("exec >>%s 2>&1", shellSingleQuote(logPath)),
		`echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] helper update: begin"`,
		fmt.Sprintf("BIN=%s", shellSingleQuote(binary)),
		fmt.Sprintf("NEW=%s", shellSingleQuote(staged)),
		`if [ -f "$NEW" ]; then`,
		`  if [ -f "$BIN" ]; then`,
		`    cp -p "$BIN" "$BIN.bak-$(date -u +%Y%m%d%H%M%S)" 2>/dev/null || true`,
		`    keep=0; for old in $(ls -1t "$BIN".bak-* 2>/dev/null); do`,
		`      keep=$((keep+1)); [ "$keep" -gt 3 ] && rm -f "$old"`,
		`    done`,
		`  fi`,
		`  if ! mv -f "$NEW" "$BIN"; then echo "helper update: replace failed"; exit 1; fi`,
		`  chmod 0755 "$BIN" 2>/dev/null || true`,
		`  echo "helper update: image replaced"`,
		`fi`,
		// systemd 形态下助手由独立单元托管，必须让 systemd 去重启（它负责拉起，
		// 自己 fork 一个会脱离管理）；绿色包形态才走控制脚本的 stop/start。
		fmt.Sprintf(`if systemctl is-active --quiet %s 2>/dev/null; then`, shellSingleQuote(HelperSystemdUnit)),
		fmt.Sprintf(`  if ! systemctl restart %s; then echo 'helper update: systemd restart failed'; exit 1; fi`, shellSingleQuote(HelperSystemdUnit)),
		`else`,
		fmt.Sprintf(`  if ! %s stop-updater >/dev/null 2>&1; then echo 'helper update: stop-updater failed'; exit 1; fi`, quotedControl),
		`  sleep 1`,
		fmt.Sprintf(`  if ! %s start-updater; then echo 'helper update: start-updater failed'; exit 1; fi`, quotedControl),
		`fi`,
		`echo "helper update: done"`,
	}
	return strings.Join(lines, "\n") + "\n"
}

// shellSingleQuote is the POSIX-safe form: wrap in single quotes and close /
// reopen around every embedded quote.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
