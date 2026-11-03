package upgrade

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type helperRecordingAdapter struct {
	fakeWorkflowAdapter
	result HelperSyncResult
	err    error
	calls  int
}

func (a *helperRecordingAdapter) SyncHelper(context.Context, TaskRecord) (HelperSyncResult, error) {
	a.calls++
	return a.result, a.err
}

type helperRecordingSink struct{ events []UpgradeEvent }

func (s *helperRecordingSink) Append(_ context.Context, event UpgradeEvent) error {
	s.events = append(s.events, event)
	return nil
}

func (s *helperRecordingSink) names() []string {
	names := make([]string, 0, len(s.events))
	for _, event := range s.events {
		names = append(names, event.Event)
	}
	return names
}

// 助手镜像必须落成旁挂文件：就地覆盖运行中的二进制，换掉的是 inode，
// 旧进程继续跑旧代码 —— 现场表现就是"升级成功了，但新能力没出现"。
func TestStageHelperImageWritesSidecar(t *testing.T) {
	release, install := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(release, HelperEntryDir), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(release, HelperEntryDir, HelperUnixImage)
	if err := os.WriteFile(source, []byte("new-helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(install, HelperEntryDir), 0o755); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(install, HelperEntryDir, HelperUnixImage)
	if err := os.WriteFile(installed, []byte("old-helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged, err := stageHelperImage(release, install, HelperUnixImage)
	if err != nil {
		t.Fatalf("stage helper: %v", err)
	}
	if !staged {
		t.Fatal("expected the candidate helper to be staged")
	}
	data, err := os.ReadFile(filepath.Join(install, HelperEntryDir, HelperUnixImage+HelperStagedSuffix))
	if err != nil {
		t.Fatalf("read staged helper: %v", err)
	}
	if string(data) != "new-helper" {
		t.Fatalf("staged helper content = %q", data)
	}
	// ⛔ 安装根里的正在使用的那份不能被就地改写，必须等控制脚本/重启换上。
	if current, err := os.ReadFile(installed); err != nil || string(current) != "old-helper" {
		t.Fatalf("installed helper was modified: %q (%v)", current, err)
	}
}

func TestStageHelperImageIsIdempotent(t *testing.T) {
	release, install := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(release, HelperEntryDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, HelperEntryDir, HelperUnixImage), []byte("same"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := stageHelperImage(release, install, HelperUnixImage); err != nil {
		t.Fatal(err)
	}
	stagedAgain, err := stageHelperImage(release, install, HelperUnixImage)
	if err != nil {
		t.Fatal(err)
	}
	if stagedAgain {
		t.Fatal("identical helper must not be re-staged on every retry")
	}
}

func TestStageHelperImageSkipsReleaseWithoutHelper(t *testing.T) {
	release, install := t.TempDir(), t.TempDir()
	staged, err := stageHelperImage(release, install, HelperUnixImage)
	if err != nil {
		t.Fatalf("a package without a helper is not an error: %v", err)
	}
	if staged {
		t.Fatal("nothing should be staged when the release ships no helper")
	}
}

// 助手以特权身份运行，包里若把它做成符号链接，等于让它指向任意路径。
func TestStageHelperImageRejectsSymlink(t *testing.T) {
	release, install := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(release, HelperEntryDir), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(release, "real-helper")
	if err := os.WriteFile(target, []byte("payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(release, HelperEntryDir, HelperUnixImage)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := stageHelperImage(release, install, HelperUnixImage); err == nil {
		t.Fatal("expected a symlinked helper to be rejected")
	}
}

func TestStageHelperImageRejectsEmptyFile(t *testing.T) {
	release, install := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(release, HelperEntryDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, HelperEntryDir, HelperUnixImage), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := stageHelperImage(release, install, HelperUnixImage); err == nil {
		t.Fatal("expected an empty helper to be rejected")
	}
}

// 路径里带分隔符的「镜像名」会把文件写到安装根外面。
func TestStageHelperImageRejectsPathInImageName(t *testing.T) {
	if _, err := stageHelperImage(t.TempDir(), t.TempDir(), "../escape"); err == nil {
		t.Fatal("expected a path-like image name to be rejected")
	}
}

func TestWorkflowSchedulesHelperRestartAfterSuccess(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &helperRecordingAdapter{result: HelperSyncResult{Staged: true, RestartScheduled: true}}
	sink := &helperRecordingSink{}
	if err := (&WorkflowExecutor{Store: store, Adapter: adapter, Events: sink}).Apply(context.Background(), record); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if adapter.calls != 1 {
		t.Fatalf("SyncHelper calls = %d, want 1", adapter.calls)
	}
	names := sink.names()
	if len(names) != 2 || names[0] != "helper_update_restart_scheduled" || names[1] != string(PhaseSucceeded) {
		t.Fatalf("events = %v, want helper event before the success event", names)
	}
}

func TestWorkflowKeepsSuccessWhenHelperSyncFails(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &helperRecordingAdapter{err: errors.New("no space left on device")}
	sink := &helperRecordingSink{}
	if err := (&WorkflowExecutor{Store: store, Adapter: adapter, Events: sink}).Apply(context.Background(), record); err != nil {
		t.Fatalf("a helper problem must not fail a live release: %v", err)
	}
	names := sink.names()
	if len(names) != 2 || names[0] != "helper_update_failed" || names[1] != string(PhaseSucceeded) {
		t.Fatalf("events = %v, want a failure event before the success event", names)
	}
	persisted, err := store.Get(record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Phase != PhaseSucceeded {
		t.Fatalf("task phase = %s, want succeeded", persisted.Phase)
	}
}

func TestWorkflowSkipsHelperSyncWhenReleaseHasNone(t *testing.T) {
	store, record := prepareWorkflowTask(t)
	adapter := &helperRecordingAdapter{}
	sink := &helperRecordingSink{}
	if err := (&WorkflowExecutor{Store: store, Adapter: adapter, Events: sink}).Apply(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if names := sink.names(); len(names) != 1 || names[0] != string(PhaseSucceeded) {
		t.Fatalf("events = %v, want only the success event", names)
	}
}

// 控制器脚本与 Go 侧对「助手装在哪、叫什么」必须逐字一致，否则旁挂文件永远
// 不会被换上（写到一个没人读的路径上，静默失效）。
func TestControlScriptAgreesOnHelperImagePath(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "standalone", "uvp-gb28181-ctl.sh"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			t.Skip("control script is not available from this checkout")
		}
		t.Fatal(err)
	}
	want := `UPDATER_BIN="$ROOT/` + HelperEntryDir + `/` + HelperUnixImage + `"`
	if !containsLine(string(script), want) {
		t.Fatalf("control script must resolve the helper as %s", want)
	}
	if !containsLine(string(script), `install_helper_image`) {
		t.Fatal("control script must align the helper image before starting it")
	}
	// 存量机器的旧助手不会自己同步（同步代码在新助手里），控制脚本必须能从
	// current 直接对齐，否则助手永远是升级前那一个。
	if !strings.Contains(string(script), `$ACTIVE_ROOT/updater/`) {
		t.Fatal("control script must be able to fall back to the active release helper")
	}
}

func containsLine(haystack, needle string) bool {
	for _, line := range splitLines(haystack) {
		if len(line) >= len(needle) && line[:len(needle)] == needle {
			return true
		}
	}
	return false
}

func splitLines(value string) []string {
	lines := make([]string, 0, 64)
	start := 0
	for index := 0; index < len(value); index++ {
		if value[index] == '\n' {
			lines = append(lines, value[start:index])
			start = index + 1
		}
	}
	lines = append(lines, value[start:])
	return lines
}
