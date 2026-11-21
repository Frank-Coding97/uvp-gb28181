package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSettings 在临时包根下造一个 data/https/settings.json。
func writeSettings(t *testing.T, root string, enabled bool, port int) string {
	t.Helper()
	dir := filepath.Join(root, "data", "https")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	path := filepath.Join(dir, "settings.json")
	body, _ := json.Marshal(map[string]any{"enabled": enabled, "port": port})
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("写 settings.json 失败: %v", err)
	}
	return path
}

func readSettingsFile(t *testing.T, path string) (enabled bool, port int) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	var got struct {
		Enabled bool `json:"enabled"`
		Port    int  `json:"port"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return got.Enabled, got.Port
}

// ⛔ 本用例锁的是一条**真实会拦住客户升级**的路径：上一版落盘的 HTTPS 端口
// 51000 现在成了明文端口，不迁就是两个 listen 抢同一端口、nginx 起不来。
func TestHTTPSRepairPortMigratesConflictingPort(t *testing.T) {
	root := t.TempDir()
	path := writeSettings(t, root, true, 51000)

	out, err := captureStdoutErr(t, func() error {
		return runHTTPSRepairPort([]string{"--root", root, "--conflict", "51000", "--fallback", "51001"})
	})
	if err != nil {
		t.Fatalf("迁移失败: %v\n%s", err, out)
	}
	if enabled, port := readSettingsFile(t, path); !enabled || port != 51001 {
		t.Fatalf("落盘结果 = enabled:%v port:%d，期望 enabled:true port:51001", enabled, port)
	}
	// ⛔ 必须保留 enabled：迁移只该动端口，把"HTTPS 开着"顺手关掉会让客户
	//   升级后从 https 掉回 http，而页面上看不出发生过什么。
	if !strings.Contains(out, "migrated 51000 -> 51001") {
		t.Fatalf("输出应报出迁移结果，实际: %s", out)
	}
}

// 幂等：ctl.sh 每次启动都会跑这一句，第二次必须什么都不改。
func TestHTTPSRepairPortIsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, false, 51001)

	for i := 0; i < 2; i++ {
		out, err := captureStdoutErr(t, func() error {
			return runHTTPSRepairPort([]string{"--root", root, "--conflict", "51000", "--fallback", "51001"})
		})
		if err != nil {
			t.Fatalf("第 %d 次执行失败: %v\n%s", i+1, err, out)
		}
		if !strings.Contains(out, "unchanged 51001") {
			t.Fatalf("第 %d 次应报 unchanged 51001，实际: %s", i+1, out)
		}
	}
}

// 用户手工设过的端口一律不碰 —— 只有"与本包明文端口相同"才是必须修的组合。
func TestHTTPSRepairPortLeavesUnrelatedPortAlone(t *testing.T) {
	root := t.TempDir()
	path := writeSettings(t, root, true, 8443)

	out, err := captureStdoutErr(t, func() error {
		return runHTTPSRepairPort([]string{"--root", root, "--conflict", "51000", "--fallback", "51001"})
	})
	if err != nil {
		t.Fatalf("执行失败: %v\n%s", err, out)
	}
	if _, port := readSettingsFile(t, path); port != 8443 {
		t.Fatalf("不该改动 8443，实际变成 %d", port)
	}
	if !strings.Contains(out, "unchanged 8443") {
		t.Fatalf("输出应说明未改动，实际: %s", out)
	}
}

// 首次启动前没有这个文件：必须安静放过，且**不能顺手造一个** —
// 造出来就等于绕过 RenderStartup 的默认值落盘逻辑。
func TestHTTPSRepairPortIgnoresMissingFile(t *testing.T) {
	root := t.TempDir()

	out, err := captureStdoutErr(t, func() error {
		return runHTTPSRepairPort([]string{"--root", root, "--conflict", "51000", "--fallback", "51001"})
	})
	if err != nil {
		t.Fatalf("文件不存在不该报错: %v\n%s", err, out)
	}
	if !strings.Contains(out, "absent") {
		t.Fatalf("输出应报 absent，实际: %s", out)
	}
	if _, statErr := os.Stat(filepath.Join(root, "data", "https", "settings.json")); !os.IsNotExist(statErr) {
		t.Fatalf("不该创建 settings.json（实际 statErr=%v）", statErr)
	}
}

// 参数自检：迁到同一个值等于没迁，必须当错误报出来，否则调用方会以为修好了。
func TestHTTPSRepairPortRejectsSameSourceAndTarget(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, true, 51000)

	out, err := captureStdoutErr(t, func() error {
		return runHTTPSRepairPort([]string{"--root", root, "--conflict", "51000", "--fallback", "51000"})
	})
	if err == nil {
		t.Fatalf("conflict == fallback 时必须报错，实际通过: %s", out)
	}
}

// captureStdoutErr 是 captureStdout 的"带错误"版本：本包里的命令函数都返回 error，
// 测试既要断言输出、也要断言退出语义（例如 conflict==fallback 必须报错）。
func captureStdoutErr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("建管道失败: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	runErr := fn()

	os.Stdout = old
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out, runErr
}
